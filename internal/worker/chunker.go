// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

// DefaultChunkSize is the maximum size (1 KB) of a single log chunk if no newlines occur.
const DefaultChunkSize = 1024

// StreamChunkerConfig holds configuration and dependencies for a log stream chunker.
type StreamChunkerConfig struct {
	TaskID     string
	StepID     string
	RunID      string
	WorkerID   string
	Stream     string // "stdout" | "stderr"
	Publisher  EventPublisher
	Secrets    []string
	ChunkSize  int
	SeqCounter *atomic.Int64
}

// StreamChunker buffers incoming execution log bytes, splits them by line or chunk threshold,
// masks sensitive secrets, and emits ordered TopicWorkerLogs events.
type StreamChunker struct {
	cfg        StreamChunkerConfig
	mu         sync.Mutex
	buf        bytes.Buffer
	fullOutput bytes.Buffer
	chunkSize  int
}

// NewStreamChunker creates an initialized StreamChunker.
func NewStreamChunker(cfg StreamChunkerConfig) *StreamChunker {
	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if cfg.SeqCounter == nil {
		cfg.SeqCounter = new(atomic.Int64)
	}

	return &StreamChunker{
		cfg:       cfg,
		chunkSize: chunkSize,
	}
}

// Write appends incoming bytes to the internal buffer and full output,
// scanning for newline delimiters or chunk size thresholds to emit events.
func (c *StreamChunker) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.fullOutput.Write(p)
	c.buf.Write(p)

	for c.buf.Len() > 0 {
		data := c.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')

		if idx != -1 {
			// Delimited line found
			lineChunk := make([]byte, idx+1)
			_, _ = c.buf.Read(lineChunk)
			c.emitChunkLocked(string(lineChunk))
		} else if c.buf.Len() >= c.chunkSize {
			// Buffer threshold reached
			thresholdChunk := make([]byte, c.chunkSize)
			_, _ = c.buf.Read(thresholdChunk)
			c.emitChunkLocked(string(thresholdChunk))
		} else {
			// Wait for more data or explicit flush
			break
		}
	}

	return len(p), nil
}

// Flush emits any residual bytes remaining in the buffer.
func (c *StreamChunker) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.buf.Len() > 0 {
		residual := c.buf.String()
		c.buf.Reset()
		c.emitChunkLocked(residual)
	}

	return nil
}

// FullOutput returns the entire accumulated output with secrets masked.
func (c *StreamChunker) FullOutput() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return sandbox.MaskSecrets(c.fullOutput.String(), c.cfg.Secrets)
}

func (c *StreamChunker) emitChunkLocked(rawChunk string) {
	if c.cfg.Publisher == nil {
		return
	}

	masked := sandbox.MaskSecrets(rawChunk, c.cfg.Secrets)
	seq := c.cfg.SeqCounter.Add(1)

	payload := gateway.WorkerLogChunkPayload{
		TaskID:    c.cfg.TaskID,
		StepID:    c.cfg.StepID,
		RunID:     c.cfg.RunID,
		WorkerID:  c.cfg.WorkerID,
		Stream:    c.cfg.Stream,
		Chunk:     masked,
		Sequence:  seq,
		Timestamp: time.Now().UnixMilli(),
	}

	ev := gateway.Event{
		Type:      gateway.TopicWorkerLogs,
		RunID:     c.cfg.RunID,
		TaskID:    c.cfg.TaskID,
		Payload:   payload.ToMap(),
		Timestamp: payload.Timestamp,
	}

	_ = c.cfg.Publisher.PublishTaskEvent(context.Background(), ev)
}
