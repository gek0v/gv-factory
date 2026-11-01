// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
)

type mockChunkEventCollector struct {
	mu     sync.Mutex
	events []gateway.Event
}

func (m *mockChunkEventCollector) PublishTaskEvent(ctx context.Context, ev gateway.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

func (m *mockChunkEventCollector) getEvents() []gateway.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]gateway.Event, len(m.events))
	copy(copied, m.events)
	return copied
}

func TestStreamChunker_SplitByLines(t *testing.T) {
	collector := &mockChunkEventCollector{}
	seqCounter := new(atomic.Int64)

	chunker := NewStreamChunker(StreamChunkerConfig{
		TaskID:     "task-123",
		StepID:     "step-456",
		RunID:      "run-789",
		WorkerID:   "worker-01",
		Stream:     "stdout",
		Publisher:  collector,
		SeqCounter: seqCounter,
	})

	input := "line 1\nline 2\n"
	n, err := chunker.Write([]byte(input))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != len(input) {
		t.Fatalf("expected written %d bytes, got %d", len(input), n)
	}

	if err := chunker.Flush(); err != nil {
		t.Fatalf("unexpected flush error: %v", err)
	}

	events := collector.getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 chunk events, got %d", len(events))
	}

	for i, ev := range events {
		if ev.Type != gateway.TopicWorkerLogs {
			t.Errorf("event %d: expected type %s, got %s", i, gateway.TopicWorkerLogs, ev.Type)
		}
		if ev.RunID != "run-789" || ev.TaskID != "task-123" {
			t.Errorf("event %d: unexpected run/task ID: %+v", i, ev)
		}
		expectedSeq := int64(i + 1)
		if ev.Payload["sequence"] != expectedSeq {
			t.Errorf("event %d: expected sequence %d, got %v", i, expectedSeq, ev.Payload["sequence"])
		}
	}

	if events[0].Payload["chunk"] != "line 1\n" {
		t.Errorf("expected chunk 'line 1\\n', got %q", events[0].Payload["chunk"])
	}
	if events[1].Payload["chunk"] != "line 2\n" {
		t.Errorf("expected chunk 'line 2\\n', got %q", events[1].Payload["chunk"])
	}

	if chunker.FullOutput() != input {
		t.Errorf("expected full output %q, got %q", input, chunker.FullOutput())
	}
}

func TestStreamChunker_BufferThreshold1KB(t *testing.T) {
	collector := &mockChunkEventCollector{}
	seqCounter := new(atomic.Int64)

	chunker := NewStreamChunker(StreamChunkerConfig{
		TaskID:     "task-1",
		StepID:     "step-1",
		RunID:      "run-1",
		Stream:     "stderr",
		Publisher:  collector,
		ChunkSize:  1024,
		SeqCounter: seqCounter,
	})

	// 2500 bytes without any newline
	largePayload := strings.Repeat("A", 2500)
	n, err := chunker.Write([]byte(largePayload))
	if err != nil {
		t.Fatalf("write error: %v", err)
	}
	if n != len(largePayload) {
		t.Fatalf("expected %d bytes written, got %d", len(largePayload), n)
	}

	// Should have emitted 2 chunks of 1024 before flush
	eventsBeforeFlush := collector.getEvents()
	if len(eventsBeforeFlush) != 2 {
		t.Fatalf("expected 2 chunks before flush, got %d", len(eventsBeforeFlush))
	}
	if len(eventsBeforeFlush[0].Payload["chunk"].(string)) != 1024 {
		t.Errorf("expected 1024 bytes in chunk 1, got %d", len(eventsBeforeFlush[0].Payload["chunk"].(string)))
	}

	// Flush residual 452 bytes
	if err := chunker.Flush(); err != nil {
		t.Fatalf("flush error: %v", err)
	}

	eventsAfterFlush := collector.getEvents()
	if len(eventsAfterFlush) != 3 {
		t.Fatalf("expected 3 chunks in total, got %d", len(eventsAfterFlush))
	}
	if len(eventsAfterFlush[2].Payload["chunk"].(string)) != 452 {
		t.Errorf("expected 452 bytes in final chunk, got %d", len(eventsAfterFlush[2].Payload["chunk"].(string)))
	}
}

func TestStreamChunker_MaskSecrets(t *testing.T) {
	collector := &mockChunkEventCollector{}
	seqCounter := new(atomic.Int64)

	secrets := []string{"ghp_secretToken123", "super-secret-password"}
	chunker := NewStreamChunker(StreamChunkerConfig{
		TaskID:     "task-sec",
		StepID:     "step-sec",
		RunID:      "run-sec",
		Stream:     "stdout",
		Publisher:  collector,
		Secrets:    secrets,
		SeqCounter: seqCounter,
	})

	raw := "Connecting with ghp_secretToken123...\nAuthenticated with password: super-secret-password\n"
	_, _ = chunker.Write([]byte(raw))
	_ = chunker.Flush()

	events := collector.getEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	for _, ev := range events {
		chunk := ev.Payload["chunk"].(string)
		for _, s := range secrets {
			if strings.Contains(chunk, s) {
				t.Fatalf("chunk leaked secret %q: %s", s, chunk)
			}
		}
	}

	if strings.Contains(chunker.FullOutput(), "ghp_secretToken123") || strings.Contains(chunker.FullOutput(), "super-secret-password") {
		t.Errorf("FullOutput leaked secret: %s", chunker.FullOutput())
	}
}

func TestStreamChunker_ConcurrentStdoutStderr(t *testing.T) {
	collector := &mockChunkEventCollector{}
	seqCounter := new(atomic.Int64)

	stdoutChunker := NewStreamChunker(StreamChunkerConfig{
		TaskID:     "t-conc",
		StepID:     "s-conc",
		RunID:      "r-conc",
		Stream:     "stdout",
		Publisher:  collector,
		SeqCounter: seqCounter,
	})

	stderrChunker := NewStreamChunker(StreamChunkerConfig{
		TaskID:     "t-conc",
		StepID:     "s-conc",
		RunID:      "r-conc",
		Stream:     "stderr",
		Publisher:  collector,
		SeqCounter: seqCounter,
	})

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = stdoutChunker.Write([]byte("stdout line\n"))
			time.Sleep(1 * time.Millisecond)
		}
		_ = stdoutChunker.Flush()
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			_, _ = stderrChunker.Write([]byte("stderr line\n"))
			time.Sleep(1 * time.Millisecond)
		}
		_ = stderrChunker.Flush()
	}()

	wg.Wait()

	events := collector.getEvents()
	if len(events) != 40 {
		t.Fatalf("expected 40 events, got %d", len(events))
	}

	// Verify all sequence numbers from 1 to 40 exist and are distinct
	seenSeq := make(map[int64]bool)
	for _, ev := range events {
		seq := ev.Payload["sequence"].(int64)
		if seenSeq[seq] {
			t.Fatalf("duplicate sequence number: %d", seq)
		}
		seenSeq[seq] = true
	}
	if len(seenSeq) != 40 {
		t.Errorf("expected 40 distinct sequences, got %d", len(seenSeq))
	}
}
