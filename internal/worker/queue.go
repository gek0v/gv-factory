// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package worker

import (
	"context"
	"errors"
	"sync"
	"time"
)

// QueueMessage abstracts a message received from JetStream or In-Memory queues.
type QueueMessage interface {
	Data() []byte
	Subject() string
	Ack() error
	NakWithDelay(delay time.Duration) error
	Term() error
	InProgress() error
}

// QueueSubscription defines an active message subscription.
type QueueSubscription interface {
	Messages() <-chan QueueMessage
	Unsubscribe() error
}

// QueueBackend defines the queue provider abstraction.
type QueueBackend interface {
	Publish(subject string, data []byte) error
	Subscribe(ctx context.Context, subject string, queueGroup string) (QueueSubscription, error)
	Close() error
}

// InMemoryQueueMessage implements QueueMessage for tests and standalone mode.
type InMemoryQueueMessage struct {
	mu        sync.RWMutex
	subject   string
	data      []byte
	acked     bool
	termed    bool
	nakCount  int
	lastDelay time.Duration
}

// NewInMemoryQueueMessage creates a new in-memory message.
func NewInMemoryQueueMessage(subject string, data []byte) *InMemoryQueueMessage {
	return &InMemoryQueueMessage{
		subject: subject,
		data:    data,
	}
}

func (m *InMemoryQueueMessage) Data() []byte {
	return m.data
}

func (m *InMemoryQueueMessage) Subject() string {
	return m.subject
}

func (m *InMemoryQueueMessage) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return nil
}

func (m *InMemoryQueueMessage) NakWithDelay(delay time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nakCount++
	m.lastDelay = delay
	return nil
}

func (m *InMemoryQueueMessage) Term() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.termed = true
	return nil
}

func (m *InMemoryQueueMessage) InProgress() error {
	return nil
}

func (m *InMemoryQueueMessage) IsAcked() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.acked
}

func (m *InMemoryQueueMessage) IsTermed() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.termed
}

func (m *InMemoryQueueMessage) NakCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nakCount
}

func (m *InMemoryQueueMessage) LastDelay() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastDelay
}

type inMemorySub struct {
	ch      chan QueueMessage
	q       *InMemoryQueue
	subject string
	group   string
	once    sync.Once
}

func (s *inMemorySub) Messages() <-chan QueueMessage {
	return s.ch
}

func (s *inMemorySub) Unsubscribe() error {
	s.once.Do(func() {
		s.q.removeSub(s)
		close(s.ch)
	})
	return nil
}

// InMemoryQueue provides an in-memory queue backend for testing and standalone mode.
type InMemoryQueue struct {
	mu     sync.RWMutex
	closed bool
	// groups: subject -> groupName -> list of subs
	groups map[string]map[string][]*inMemorySub
	// rrIndex: subject -> groupName -> round-robin pointer
	rrIndex map[string]map[string]int
}

// NewInMemoryQueue initializes a thread-safe in-memory queue.
func NewInMemoryQueue() *InMemoryQueue {
	return &InMemoryQueue{
		groups:  make(map[string]map[string][]*inMemorySub),
		rrIndex: make(map[string]map[string]int),
	}
}

// Publish broadcasts a message to subscribers matching the subject.
func (q *InMemoryQueue) Publish(subject string, data []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return errors.New("queue is closed")
	}

	groupMap, ok := q.groups[subject]
	if !ok || len(groupMap) == 0 {
		return nil
	}

	for groupName, subs := range groupMap {
		if len(subs) == 0 {
			continue
		}
		if groupName == "" {
			// Broadcast to all subscribers without group
			for _, s := range subs {
				msg := NewInMemoryQueueMessage(subject, data)
				select {
				case s.ch <- msg:
				default:
				}
			}
		} else {
			// Round-robin among subscribers in the queue group
			idx := q.rrIndex[subject][groupName] % len(subs)
			q.rrIndex[subject][groupName] = (idx + 1) % len(subs)
			targetSub := subs[idx]
			msg := NewInMemoryQueueMessage(subject, data)
			select {
			case targetSub.ch <- msg:
			default:
			}
		}
	}

	return nil
}

// Subscribe registers a queue subscription for the given subject and optional queueGroup.
func (q *InMemoryQueue) Subscribe(ctx context.Context, subject string, queueGroup string) (QueueSubscription, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return nil, errors.New("queue is closed")
	}

	if q.groups[subject] == nil {
		q.groups[subject] = make(map[string][]*inMemorySub)
		q.rrIndex[subject] = make(map[string]int)
	}

	sub := &inMemorySub{
		ch:      make(chan QueueMessage, 100),
		q:       q,
		subject: subject,
		group:   queueGroup,
	}

	if ctx != nil {
		go func() {
			<-ctx.Done()
			_ = sub.Unsubscribe()
		}()
	}

	q.groups[subject][queueGroup] = append(q.groups[subject][queueGroup], sub)
	return sub, nil
}

func (q *InMemoryQueue) removeSub(target *inMemorySub) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.closed {
		return
	}

	if q.groups[target.subject] != nil {
		subs := q.groups[target.subject][target.group]
		for i, s := range subs {
			if s == target {
				q.groups[target.subject][target.group] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
	}
}

// Close shuts down the in-memory queue and all active subscriptions.
func (q *InMemoryQueue) Close() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true

	var allSubs []*inMemorySub
	for _, groupMap := range q.groups {
		for _, subs := range groupMap {
			allSubs = append(allSubs, subs...)
		}
	}
	q.groups = make(map[string]map[string][]*inMemorySub)
	q.mu.Unlock()

	for _, s := range allSubs {
		_ = s.Unsubscribe()
	}
	return nil
}
