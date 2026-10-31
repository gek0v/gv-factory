// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package outbound

import (
	"context"
	"errors"
	"testing"
)

func TestMemorySubscriptionStore_CRUD(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySubscriptionStore()

	// 1. Create with empty URL fails
	_, err := store.Create(ctx, Subscription{URL: ""})
	if !errors.Is(err, ErrInvalidSubscription) {
		t.Fatalf("expected ErrInvalidSubscription, got %v", err)
	}

	// 2. Create valid subscription
	sub := Subscription{
		URL:        "https://example.com/webhook",
		Secret:     "secret-xyz",
		EventTypes: []string{"task.completed", "gate.*"},
		Active:     true,
		Headers:    map[string]string{"X-Custom": "header-val"},
	}
	created, err := store.Create(ctx, sub)
	if err != nil {
		t.Fatalf("failed creating subscription: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected generated subscription ID")
	}

	// 3. Prevent duplicate creation
	_, err = store.Create(ctx, created)
	if !errors.Is(err, ErrSubscriptionExists) {
		t.Fatalf("expected ErrSubscriptionExists, got %v", err)
	}

	// 4. Get subscription
	fetched, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed fetching subscription: %v", err)
	}
	if fetched.URL != "https://example.com/webhook" {
		t.Errorf("unexpected URL: %s", fetched.URL)
	}
	if fetched.Headers["X-Custom"] != "header-val" {
		t.Errorf("unexpected custom header: %v", fetched.Headers)
	}

	// 5. Update subscription
	fetched.Active = false
	updated, err := store.Update(ctx, fetched)
	if err != nil {
		t.Fatalf("failed updating subscription: %v", err)
	}
	if updated.Active != false {
		t.Errorf("expected subscription to be inactive")
	}

	// 6. Delete subscription
	if err := store.Delete(ctx, created.ID); err != nil {
		t.Fatalf("failed deleting subscription: %v", err)
	}

	// 7. Get deleted returns ErrSubscriptionNotFound
	_, err = store.Get(ctx, created.ID)
	if !errors.Is(err, ErrSubscriptionNotFound) {
		t.Fatalf("expected ErrSubscriptionNotFound after deletion, got %v", err)
	}
}

func TestMemorySubscriptionStore_GetActiveSubscriptionsForEvent(t *testing.T) {
	ctx := context.Background()
	store := NewMemorySubscriptionStore()

	// Sub 1: Active, subscribes to task.*
	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-task",
		URL:        "https://example.com/tasks",
		EventTypes: []string{"task.*"},
		Active:     true,
	})

	// Sub 2: Active, subscribes to pr.*
	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-pr",
		URL:        "https://example.com/prs",
		EventTypes: []string{"pr.*"},
		Active:     true,
	})

	// Sub 3: Inactive, subscribes to task.*
	_, _ = store.Create(ctx, Subscription{
		ID:         "sub-inactive-task",
		URL:        "https://example.com/tasks-inactive",
		EventTypes: []string{"task.*"},
		Active:     false,
	})

	matching, err := store.GetActiveSubscriptionsForEvent(ctx, "task.completed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matching) != 1 {
		t.Fatalf("expected 1 matching subscription, got %d", len(matching))
	}
	if matching[0].ID != "sub-task" {
		t.Errorf("expected sub-task, got %s", matching[0].ID)
	}
}
