// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type natsQueueMessage struct {
	msg *nats.Msg
}

func newNatsQueueMessage(msg *nats.Msg) *natsQueueMessage {
	return &natsQueueMessage{msg: msg}
}

func (m *natsQueueMessage) Data() []byte {
	return m.msg.Data
}

func (m *natsQueueMessage) Subject() string {
	return m.msg.Subject
}

func (m *natsQueueMessage) Ack() error {
	return m.msg.Ack()
}

func (m *natsQueueMessage) NakWithDelay(delay time.Duration) error {
	return m.msg.NakWithDelay(delay)
}

func (m *natsQueueMessage) Term() error {
	return m.msg.Term()
}

func (m *natsQueueMessage) InProgress() error {
	return m.msg.InProgress()
}

// QueueBackendConfig configures the queue provider.
type QueueBackendConfig struct {
	NATSURL        string
	DisableNATS    bool
	ConnectTimeout time.Duration
}

type natsSubscription struct {
	sub  *nats.Subscription
	ch   chan QueueMessage
	once sync.Once
}

func (s *natsSubscription) Messages() <-chan QueueMessage {
	return s.ch
}

func (s *natsSubscription) Unsubscribe() error {
	s.once.Do(func() {
		if s.sub != nil {
			_ = s.sub.Unsubscribe()
		}
		close(s.ch)
	})
	return nil
}

// NatsQueueBackend wraps a NATS JetStream connection.
type NatsQueueBackend struct {
	nc        *nats.Conn
	js        nats.JetStreamContext
	closeOnce sync.Once
}

// NewQueueBackend establishes a JetStream connection or falls back to an InMemoryQueue.
func NewQueueBackend(cfg QueueBackendConfig) (QueueBackend, bool, error) {
	if cfg.DisableNATS {
		return NewInMemoryQueue(), false, nil
	}

	url := strings.TrimSpace(cfg.NATSURL)
	if url == "" {
		url = nats.DefaultURL
	}

	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 250 * time.Millisecond
	}

	nc, err := nats.Connect(url, nats.Timeout(timeout), nats.MaxReconnects(2))
	if err != nil {
		// Fallback to in-memory mode
		return NewInMemoryQueue(), false, nil
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return NewInMemoryQueue(), false, nil
	}

	// Ensure OV_TASKS stream exists
	streamInfo, err := js.StreamInfo(StreamTasks)
	if err != nil || streamInfo == nil {
		_, _ = js.AddStream(&nats.StreamConfig{
			Name:       StreamTasks,
			Subjects:   []string{TopicWorkTasks, TopicWorkTasksFilter},
			Retention:  nats.WorkQueuePolicy,
			Storage:    nats.FileStorage,
			Duplicates: 2 * time.Minute,
		})
	}

	return &NatsQueueBackend{
		nc: nc,
		js: js,
	}, true, nil
}

// Publish sends a message to the specified JetStream subject.
func (b *NatsQueueBackend) Publish(subject string, data []byte) error {
	if b.js == nil {
		return errors.New("jetstream not available")
	}
	_, err := b.js.Publish(subject, data)
	return err
}

// Subscribe opens a durable queue group subscription on the JetStream subject.
func (b *NatsQueueBackend) Subscribe(ctx context.Context, subject string, queueGroup string) (QueueSubscription, error) {
	if b.js == nil {
		return nil, errors.New("jetstream not available")
	}

	msgCh := make(chan *nats.Msg, 100)
	sub, err := b.js.ChanQueueSubscribe(subject, queueGroup, msgCh, nats.ManualAck(), nats.Durable(queueGroup))
	if err != nil {
		return nil, fmt.Errorf("chan queue subscribe failed: %w", err)
	}

	qSub := &natsSubscription{
		sub: sub,
		ch:  make(chan QueueMessage, 100),
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case nm, ok := <-msgCh:
				if !ok {
					return
				}
				select {
				case qSub.ch <- newNatsQueueMessage(nm):
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return qSub, nil
}

// Close gracefully stops the NATS JetStream connection.
func (b *NatsQueueBackend) Close() error {
	b.closeOnce.Do(func() {
		if b.nc != nil {
			b.nc.Drain()
			b.nc.Close()
		}
	})
	return nil
}
