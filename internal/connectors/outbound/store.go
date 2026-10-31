// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SubscriptionStore defines operations for managing webhook subscriber configurations.
type SubscriptionStore interface {
	Create(ctx context.Context, sub Subscription) (Subscription, error)
	Get(ctx context.Context, id string) (Subscription, error)
	Update(ctx context.Context, sub Subscription) (Subscription, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]Subscription, error)
	GetActiveSubscriptionsForEvent(ctx context.Context, eventType string) ([]Subscription, error)
}

// MemorySubscriptionStore is a thread-safe in-memory implementation of SubscriptionStore.
type MemorySubscriptionStore struct {
	mu            sync.RWMutex
	subscriptions map[string]Subscription
}

// NewMemorySubscriptionStore creates an empty, initialized in-memory subscription store.
func NewMemorySubscriptionStore() *MemorySubscriptionStore {
	return &MemorySubscriptionStore{
		subscriptions: make(map[string]Subscription),
	}
}

func generateID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

// Create registers a new webhook subscription.
func (s *MemorySubscriptionStore) Create(ctx context.Context, sub Subscription) (Subscription, error) {
	if strings.TrimSpace(sub.URL) == "" {
		return Subscription{}, ErrInvalidSubscription
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if sub.ID == "" {
		sub.ID = generateID("sub")
	} else if _, exists := s.subscriptions[sub.ID]; exists {
		return Subscription{}, ErrSubscriptionExists
	}

	now := time.Now().UTC()
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = now
	}
	sub.UpdatedAt = now

	// Clone headers map to prevent external mutation
	if sub.Headers != nil {
		hCopy := make(map[string]string, len(sub.Headers))
		for k, v := range sub.Headers {
			hCopy[k] = v
		}
		sub.Headers = hCopy
	}

	// Clone events slice
	if sub.EventTypes != nil {
		eCopy := make([]string, len(sub.EventTypes))
		copy(eCopy, sub.EventTypes)
		sub.EventTypes = eCopy
	}

	s.subscriptions[sub.ID] = sub
	return sub, nil
}

// Get retrieves a subscription by its ID.
func (s *MemorySubscriptionStore) Get(ctx context.Context, id string) (Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sub, exists := s.subscriptions[id]
	if !exists {
		return Subscription{}, ErrSubscriptionNotFound
	}
	return sub, nil
}

// Update mutates an existing subscription.
func (s *MemorySubscriptionStore) Update(ctx context.Context, sub Subscription) (Subscription, error) {
	if strings.TrimSpace(sub.URL) == "" {
		return Subscription{}, ErrInvalidSubscription
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.subscriptions[sub.ID]
	if !exists {
		return Subscription{}, ErrSubscriptionNotFound
	}

	sub.CreatedAt = existing.CreatedAt
	sub.UpdatedAt = time.Now().UTC()

	if sub.Headers != nil {
		hCopy := make(map[string]string, len(sub.Headers))
		for k, v := range sub.Headers {
			hCopy[k] = v
		}
		sub.Headers = hCopy
	}

	if sub.EventTypes != nil {
		eCopy := make([]string, len(sub.EventTypes))
		copy(eCopy, sub.EventTypes)
		sub.EventTypes = eCopy
	}

	s.subscriptions[sub.ID] = sub
	return sub, nil
}

// Delete removes a subscription by its ID.
func (s *MemorySubscriptionStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.subscriptions[id]; !exists {
		return ErrSubscriptionNotFound
	}

	delete(s.subscriptions, id)
	return nil
}

// List returns all subscriptions in the store.
func (s *MemorySubscriptionStore) List(ctx context.Context) ([]Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]Subscription, 0, len(s.subscriptions))
	for _, sub := range s.subscriptions {
		list = append(list, sub)
	}
	return list, nil
}

// GetActiveSubscriptionsForEvent retrieves all active subscriptions that match eventType.
func (s *MemorySubscriptionStore) GetActiveSubscriptionsForEvent(ctx context.Context, eventType string) ([]Subscription, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matching []Subscription
	for _, sub := range s.subscriptions {
		if sub.Matches(eventType) {
			matching = append(matching, sub)
		}
	}
	return matching, nil
}
