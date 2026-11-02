// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/agent"
	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

type mockPipelineSubmitter struct {
	mu           sync.Mutex
	submitted    []dag.RunRequest
	runs         []*dag.FactoryRun
	submitErr    error
	submitCalled int32
}

func (m *mockPipelineSubmitter) SubmitRun(ctx context.Context, req dag.RunRequest) (*dag.FactoryRun, error) {
	atomic.AddInt32(&m.submitCalled, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.submitErr != nil {
		return nil, m.submitErr
	}
	run := &dag.FactoryRun{
		ID:           req.RunID,
		TemplateName: req.Template.Metadata.Name,
		Branch:       req.Branch,
		BaseBranch:   req.BaseBranch,
		RepoPath:     req.RepoPath,
		Priority:     req.Priority,
		State:        dag.FactoryRunStateQueued,
		Labels:       req.Labels,
	}
	m.submitted = append(m.submitted, req)
	m.runs = append(m.runs, run)
	return run, nil
}

type mockEventPublisher struct {
	mu     sync.Mutex
	events []gateway.Event
}

func (m *mockEventPublisher) PublishTaskEvent(ctx context.Context, ev gateway.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, ev)
	return nil
}

func (m *mockEventPublisher) getEvents() []gateway.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]gateway.Event, len(m.events))
	copy(copied, m.events)
	return copied
}

func TestMemoryDeduplicator(t *testing.T) {
	t.Run("First arrival is not duplicate", func(t *testing.T) {
		d := NewMemoryDeduplicator(100 * time.Millisecond)
		defer d.Close()

		if d.IsDuplicate(context.Background(), "deliv-1") {
			t.Fatalf("expected deliv-1 not to be duplicate on first arrival")
		}
	})

	t.Run("Second arrival within TTL is duplicate", func(t *testing.T) {
		d := NewMemoryDeduplicator(100 * time.Millisecond)
		defer d.Close()

		if d.IsDuplicate(context.Background(), "deliv-2") {
			t.Fatalf("expected deliv-2 not to be duplicate first time")
		}
		if !d.IsDuplicate(context.Background(), "deliv-2") {
			t.Fatalf("expected deliv-2 to be detected as duplicate second time")
		}
	})

	t.Run("Expiration after TTL", func(t *testing.T) {
		d := NewMemoryDeduplicator(30 * time.Millisecond)
		defer d.Close()

		if d.IsDuplicate(context.Background(), "deliv-3") {
			t.Fatalf("first arrival should not be duplicate")
		}
		time.Sleep(50 * time.Millisecond)
		if d.IsDuplicate(context.Background(), "deliv-3") {
			t.Fatalf("deliv-3 should have expired after TTL")
		}
	})

	t.Run("Empty delivery ID is never duplicate", func(t *testing.T) {
		d := NewMemoryDeduplicator(100 * time.Millisecond)
		defer d.Close()

		if d.IsDuplicate(context.Background(), "") {
			t.Fatalf("empty delivery ID should not be duplicate")
		}
		if d.IsDuplicate(context.Background(), "") {
			t.Fatalf("empty delivery ID should not be duplicate on second arrival either")
		}
	})

	t.Run("Concurrent access safety", func(t *testing.T) {
		d := NewMemoryDeduplicator(200 * time.Millisecond)
		defer d.Close()

		var duplicateCount int32
		var notDuplicateCount int32
		var wg sync.WaitGroup

		for i := 0; i < 30; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if d.IsDuplicate(context.Background(), "concurrent-deliv") {
					atomic.AddInt32(&duplicateCount, 1)
				} else {
					atomic.AddInt32(&notDuplicateCount, 1)
				}
			}()
		}
		wg.Wait()

		if notDuplicateCount != 1 {
			t.Fatalf("expected exactly 1 not duplicate, got %d", notDuplicateCount)
		}
		if duplicateCount != 29 {
			t.Fatalf("expected 29 duplicates, got %d", duplicateCount)
		}
	})
}

func TestCatalogTemplateResolver(t *testing.T) {
	resolver := NewCatalogTemplateResolver()

	t.Run("Resolve default-tdd-factory", func(t *testing.T) {
		tpl, err := resolver.ResolveTemplate(context.Background(), "default-tdd-factory")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tpl == nil || tpl.Metadata.Name != "default-tdd-factory" {
			t.Fatalf("expected default-tdd-factory, got %+v", tpl)
		}
	})

	t.Run("Fallback on unknown template", func(t *testing.T) {
		tpl, err := resolver.ResolveTemplate(context.Background(), "unknown-template-xyz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tpl == nil || tpl.Metadata.Name != "default-tdd-factory" {
			t.Fatalf("expected fallback to default-tdd-factory, got %+v", tpl)
		}
	})

	t.Run("Register custom template", func(t *testing.T) {
		custom := &schema.FactoryTemplate{
			APIVersion: "gv-factory.io/v1alpha1",
			Kind:       "FactoryTemplate",
			Metadata: schema.Metadata{
				Name: "custom-pipeline",
			},
			Spec: schema.FactorySpec{
				Entrypoint: "test-step",
				Steps: []schema.StepConfig{
					{
						ID:     "test-step",
						Name:   "Test Step",
						Plugin: "extism://plugins/test.wasm",
					},
				},
			},
		}
		resolver.RegisterTemplate(custom)

		resolved, err := resolver.ResolveTemplate(context.Background(), "custom-pipeline")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resolved.Metadata.Name != "custom-pipeline" {
			t.Fatalf("expected custom-pipeline, got %s", resolved.Metadata.Name)
		}
	})
}

func TestPipelineTriggerManager_Filtering(t *testing.T) {
	submitter := &mockPipelineSubmitter{}
	pub := &mockEventPublisher{}
	dedup := NewMemoryDeduplicator(time.Minute)
	defer dedup.Close()
	resolver := NewCatalogTemplateResolver()

	cfg := TriggerManagerConfig{
		DefaultTemplate: "default-tdd-factory",
		Rules: []TriggerRule{
			{
				Name:           "github-issue-ready-for-agent",
				Sources:        []string{schema.SourceGitHub},
				EventTypes:     []string{schema.EventIssueLabeled},
				RequiredLabels: []string{"ready-for-agent"},
				TemplateName:   "default-tdd-factory",
				Priority:       dag.PriorityHigh,
			},
			{
				Name:         "github-issue-opened",
				Sources:      []string{schema.SourceGitHub},
				EventTypes:   []string{schema.EventIssueOpened},
				TemplateName: "default-tdd-factory",
				Priority:     dag.PriorityNormal,
			},
			{
				Name:         "github-pr-opened",
				Sources:      []string{schema.SourceGitHub},
				EventTypes:   []string{schema.EventPROpened},
				TemplateName: "default-tdd-factory",
				Priority:     dag.PriorityNormal,
			},
		},
	}

	mgr := NewPipelineTriggerManager(cfg, submitter, pub, dedup, resolver)
	defer mgr.Close()

	t.Run("Issue labeled with ready-for-agent matches and triggers run", func(t *testing.T) {
		ev := schema.TriggerEvent{
			ID:          "deliv-match-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 121,
			Sender:      "gek0v",
			Payload: map[string]any{
				"action": "labeled",
				"label": map[string]any{
					"name": "ready-for-agent",
				},
				"issue": map[string]any{
					"number": 121,
					"title":  "feat: automated pipeline triggers",
					"body":   "Please trigger the pipeline",
					"labels": []any{
						map[string]any{"name": "ready-for-agent"},
						map[string]any{"name": "enhancement"},
					},
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("HandleTrigger failed: %v", err)
		}
		if !res.Handled {
			t.Fatalf("expected Handled == true, got %+v", res)
		}
		if res.RuleMatched != "github-issue-ready-for-agent" {
			t.Fatalf("expected rule github-issue-ready-for-agent, got %s", res.RuleMatched)
		}
		if atomic.LoadInt32(&submitter.submitCalled) != 1 {
			t.Fatalf("expected 1 submit call, got %d", submitter.submitCalled)
		}

		// Verify event TopicPipelineTriggered was published
		events := pub.getEvents()
		var found bool
		for _, e := range events {
			if e.Type == gateway.TopicPipelineTriggered {
				found = true
				if e.Payload["rule"] != "github-issue-ready-for-agent" {
					t.Errorf("unexpected rule in event payload: %v", e.Payload["rule"])
				}
				if e.Payload["issue_number"] != 121 {
					t.Errorf("unexpected issue_number in event: %v", e.Payload["issue_number"])
				}
			}
		}
		if !found {
			t.Fatalf("TopicPipelineTriggered was not published")
		}
	})

	t.Run("Issue labeled with documentation does not match", func(t *testing.T) {
		ev := schema.TriggerEvent{
			ID:          "deliv-skip-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 122,
			Sender:      "gek0v",
			Payload: map[string]any{
				"action": "labeled",
				"label": map[string]any{
					"name": "documentation",
				},
				"issue": map[string]any{
					"number": 122,
					"title":  "docs: update readme",
					"labels": []any{
						map[string]any{"name": "documentation"},
					},
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Handled {
			t.Fatalf("expected Handled == false for documentation label, got %+v", res)
		}
		if !res.Skipped {
			t.Fatalf("expected Skipped == true")
		}
	})

	t.Run("Replay of delivery ID is rejected as duplicate", func(t *testing.T) {
		ev := schema.TriggerEvent{
			ID:          "deliv-match-1", // Same ID as first test!
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 121,
			Sender:      "gek0v",
			Payload: map[string]any{
				"action": "labeled",
				"label": map[string]any{
					"name": "ready-for-agent",
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Handled {
			t.Fatalf("expected replayed event to be skipped, but Handled == true")
		}
		if !res.Skipped || res.Reason != "duplicate delivery ID" {
			t.Fatalf("expected duplicate delivery ID skip reason, got %+v", res)
		}
	})

	t.Run("Issue opened matches rule", func(t *testing.T) {
		ev := schema.TriggerEvent{
			ID:          "deliv-opened-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueOpened,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 123,
			Sender:      "developer",
			Payload: map[string]any{
				"action": "opened",
				"issue": map[string]any{
					"number": 123,
					"title":  "fix: race condition",
					"body":   "Found a race condition in worker",
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("HandleTrigger failed: %v", err)
		}
		if !res.Handled || res.RuleMatched != "github-issue-opened" {
			t.Fatalf("expected rule github-issue-opened, got %+v", res)
		}
	})

	t.Run("PR opened matches rule", func(t *testing.T) {
		ev := schema.TriggerEvent{
			ID:         "deliv-pr-opened-1",
			Source:     schema.SourceGitHub,
			EventType:  schema.EventPROpened,
			Repository: "gv-factory/gv-factory",
			PRNumber:   55,
			Reference:  "feat/new-ui",
			Sender:     "contributor",
			Payload: map[string]any{
				"action": "opened",
				"pull_request": map[string]any{
					"number": 55,
					"title":  "feat: new web UI",
					"body":   "PR description here",
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("HandleTrigger failed: %v", err)
		}
		if !res.Handled || res.RuleMatched != "github-pr-opened" {
			t.Fatalf("expected rule github-pr-opened, got %+v", res)
		}
	})

	t.Run("Ping or unsupported event is skipped", func(t *testing.T) {
		ev := schema.TriggerEvent{
			ID:        "deliv-ping-1",
			Source:    schema.SourceGitHub,
			EventType: "ping",
		}
		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Handled {
			t.Fatalf("expected ping to be skipped, got %+v", res)
		}
	})
}

func TestPipelineTriggerManager_ContextHydration(t *testing.T) {
	submitter := &mockPipelineSubmitter{}
	pub := &mockEventPublisher{}
	dedup := NewMemoryDeduplicator(time.Minute)
	defer dedup.Close()
	resolver := NewCatalogTemplateResolver()

	cfg := TriggerManagerConfig{
		DefaultTemplate: "default-tdd-factory",
		BaseBranch:      "main",
		Rules: []TriggerRule{
			{
				Name:         "catch-all",
				EventTypes:   []string{schema.EventIssueOpened},
				TemplateName: "default-tdd-factory",
			},
		},
	}

	mgr := NewPipelineTriggerManager(cfg, submitter, pub, dedup, resolver)
	defer mgr.Close()

	ev := schema.TriggerEvent{
		ID:          "deliv-ctx-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueOpened,
		Repository:  "gv-factory/sample-repo",
		IssueNumber: 42,
		Sender:      "alice",
		Payload: map[string]any{
			"issue": map[string]any{
				"number": 42,
				"title":  "Add user auth",
				"body":   "Need JWT-based login endpoint",
			},
		},
	}

	res, err := mgr.HandleTrigger(context.Background(), ev)
	if err != nil {
		t.Fatalf("HandleTrigger failed: %v", err)
	}
	if !res.Handled {
		t.Fatalf("expected Handled == true")
	}

	submitter.mu.Lock()
	defer submitter.mu.Unlock()

	if len(submitter.submitted) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(submitter.submitted))
	}

	req := submitter.submitted[0]

	// Verify Labels
	if req.Labels["source"] != "github" {
		t.Errorf("expected source github, got %s", req.Labels["source"])
	}
	if req.Labels["issue_number"] != "42" {
		t.Errorf("expected issue_number 42, got %s", req.Labels["issue_number"])
	}
	if req.Labels["repository"] != "gv-factory/sample-repo" {
		t.Errorf("expected repository gv-factory/sample-repo, got %s", req.Labels["repository"])
	}
	if req.Labels["sender"] != "alice" {
		t.Errorf("expected sender alice, got %s", req.Labels["sender"])
	}
	if req.Labels["delivery_id"] != "deliv-ctx-1" {
		t.Errorf("expected delivery_id deliv-ctx-1, got %s", req.Labels["delivery_id"])
	}

	// Verify Branch naming
	expectedBranchPrefix := "ov-run/issue-42"
	if req.Branch != expectedBranchPrefix && len(req.Branch) < len(expectedBranchPrefix) {
		t.Errorf("expected branch to contain %s, got %s", expectedBranchPrefix, req.Branch)
	}

	// Verify step configs hydrated
	if len(req.Template.Spec.Steps) == 0 {
		t.Fatalf("expected template to have steps")
	}
	step0 := req.Template.Spec.Steps[0]
	if step0.Config["issue_number"] != "42" {
		t.Errorf("step config missing issue_number: %+v", step0.Config)
	}
	if step0.Config["issue_title"] != "Add user auth" {
		t.Errorf("step config missing issue_title: %+v", step0.Config)
	}
	if step0.Config["issue_body"] != "Need JWT-based login endpoint" {
		t.Errorf("step config missing issue_body: %+v", step0.Config)
	}
}

func TestPipelineTriggerManager_DispatcherInterface(t *testing.T) {
	submitter := &mockPipelineSubmitter{}
	mgr := NewPipelineTriggerManager(DefaultTriggerManagerConfig(), submitter, nil, nil, nil)
	defer mgr.Close()

	// Verify it implements Dispatcher interface
	var _ Dispatcher = mgr

	ev := schema.TriggerEvent{
		ID:          "dispatch-test-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 88,
		Payload: map[string]any{
			"label": map[string]any{"name": "ready-for-agent"},
		},
	}

	if err := mgr.Dispatch(context.Background(), ev); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if atomic.LoadInt32(&submitter.submitCalled) != 1 {
		t.Fatalf("expected 1 submission from Dispatch call, got %d", submitter.submitCalled)
	}
}

func TestPipelineTriggerManager_SubmitErrorHandling(t *testing.T) {
	submitter := &mockPipelineSubmitter{
		submitErr: errors.New("branch conflict detected"),
	}
	mgr := NewPipelineTriggerManager(DefaultTriggerManagerConfig(), submitter, nil, nil, nil)
	defer mgr.Close()

	ev := schema.TriggerEvent{
		ID:          "dispatch-err-1",
		Source:      schema.SourceGitHub,
		EventType:   schema.EventIssueLabeled,
		Repository:  "owner/repo",
		IssueNumber: 88,
		Payload: map[string]any{
			"label": map[string]any{"name": "ready-for-agent"},
		},
	}

	res, err := mgr.HandleTrigger(context.Background(), ev)
	if err == nil {
		t.Fatalf("expected error from HandleTrigger, got nil")
	}
	if res.Handled {
		t.Fatalf("expected Handled == false on error")
	}

	t.Run("Submit error handling in Dispatch returns error", func(t *testing.T) {
		subErr := &mockPipelineSubmitter{
			submitErr: errors.New("orchestrator offline"),
		}
		m := NewPipelineTriggerManager(DefaultTriggerManagerConfig(), subErr, nil, nil, nil)
		defer m.Close()

		ev := schema.TriggerEvent{
			ID:          "dispatch-err-sub",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "owner/repo",
			IssueNumber: 88,
			Payload: map[string]any{
				"label": map[string]any{"name": "ready-for-agent"},
			},
		}

		err := m.Dispatch(context.Background(), ev)
		if err == nil {
			t.Fatalf("expected error from Dispatch when submitter errors, got nil")
		}
	})

	t.Run("PR labels array extraction matches rule", func(t *testing.T) {
		sub := &mockPipelineSubmitter{}
		cfg := DefaultTriggerManagerConfig()
		cfg.Rules = []TriggerRule{
			{
				Name:           "pr-labeled-ready",
				EventTypes:     []string{schema.EventPRUpdated},
				RequiredLabels: []string{"ready-for-agent"},
			},
		}
		m := NewPipelineTriggerManager(cfg, sub, nil, nil, nil)
		defer m.Close()

		ev := schema.TriggerEvent{
			ID:        "pr-label-extract-1",
			Source:    schema.SourceGitHub,
			EventType: schema.EventPRUpdated,
			PRNumber:  99,
			Payload: map[string]any{
				"pull_request": map[string]any{
					"number": 99,
					"title":  "fix: race condition in worker",
					"body":   "Resolves race condition",
					"labels": []any{
						map[string]any{"name": "ready-for-agent"},
					},
				},
			},
		}

		res, err := m.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("HandleTrigger failed: %v", err)
		}
		if !res.Handled {
			t.Fatalf("expected Handled == true for PR labels match")
		}
	})

	t.Run("CustomFilter and Repository filtering", func(t *testing.T) {
		sub := &mockPipelineSubmitter{}
		cfg := DefaultTriggerManagerConfig()
		cfg.Rules = []TriggerRule{
			{
				Name:         "filtered-repo",
				EventTypes:   []string{schema.EventIssueOpened},
				Repositories: []string{"gv-factory/gv-factory"},
				CustomFilter: func(ev *schema.TriggerEvent) bool {
					return ev.Sender == "special-bot"
				},
			},
		}
		m := NewPipelineTriggerManager(cfg, sub, nil, nil, nil)
		defer m.Close()

		// Case 1: Wrong repository
		ev1 := schema.TriggerEvent{
			ID:          "repo-mismatch",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueOpened,
			Repository:  "other/repo",
			IssueNumber: 1,
			Sender:      "special-bot",
		}
		res1, _ := m.HandleTrigger(context.Background(), ev1)
		if res1.Handled {
			t.Fatalf("expected repository mismatch to not be handled")
		}

		// Case 2: Correct repository, failed custom filter
		ev2 := schema.TriggerEvent{
			ID:          "custom-filter-failed",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueOpened,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 1,
			Sender:      "normal-user",
		}
		res2, _ := m.HandleTrigger(context.Background(), ev2)
		if res2.Handled {
			t.Fatalf("expected custom filter fail to not be handled")
		}

		// Case 3: Correct repository and passing custom filter
		ev3 := schema.TriggerEvent{
			ID:          "custom-filter-pass",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueOpened,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 1,
			Sender:      "special-bot",
		}
		res3, err := m.HandleTrigger(context.Background(), ev3)
		if err != nil || !res3.Handled {
			t.Fatalf("expected custom filter pass to be handled, got %+v (err: %v)", res3, err)
		}
	})

	t.Run("Nil submitter returns error", func(t *testing.T) {
		m := &PipelineTriggerManager{
			cfg:      DefaultTriggerManagerConfig(),
			dedup:    NewMemoryDeduplicator(time.Minute),
			resolver: NewCatalogTemplateResolver(),
		}
		defer m.Close()

		ev := schema.TriggerEvent{
			ID:          "nil-sub-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueOpened,
			IssueNumber: 10,
		}
		_, err := m.HandleTrigger(context.Background(), ev)
		if err == nil {
			t.Fatalf("expected error with nil submitter, got nil")
		}
	})

	t.Run("Fallback template resolution failure returns error", func(t *testing.T) {
		sub := &mockPipelineSubmitter{}
		emptyResolver := &CatalogTemplateResolver{
			templates: make(map[string]*schema.FactoryTemplate),
		}
		cfg := DefaultTriggerManagerConfig()
		cfg.DefaultTemplate = "non-existent"
		m := NewPipelineTriggerManager(cfg, sub, nil, nil, emptyResolver)
		defer m.Close()

		ev := schema.TriggerEvent{
			ID:          "res-fail-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueOpened,
			IssueNumber: 10,
		}
		_, err := m.HandleTrigger(context.Background(), ev)
		if err == nil {
			t.Fatalf("expected template resolution error, got nil")
		}
	})

	t.Run("Default fallback parameters in constructor and cloneTemplate edge cases", func(t *testing.T) {
		m := NewPipelineTriggerManager(
			TriggerManagerConfig{}, // empty default template & base branch
			&mockPipelineSubmitter{},
			nil,
			nil, // nil deduplicator
			nil, // nil resolver
		)
		defer m.Close()

		if m.cfg.DefaultTemplate != "default-tdd-factory" {
			t.Errorf("expected default-tdd-factory, got %s", m.cfg.DefaultTemplate)
		}
		if m.cfg.BaseBranch != "main" {
			t.Errorf("expected base branch main, got %s", m.cfg.BaseBranch)
		}

		// cloneTemplate nil test
		if cloneTemplate(nil) != nil {
			t.Errorf("expected nil clone for nil input")
		}

		// Register nil or empty template
		res := NewCatalogTemplateResolver()
		res.RegisterTemplate(nil)
		res.RegisterTemplate(&schema.FactoryTemplate{})

		// empty payload extractLabelsFromPayload
		lbls := extractLabelsFromPayload(nil)
		if len(lbls) != 0 {
			t.Errorf("expected 0 labels from nil payload")
		}
	})

	t.Run("generateRunID fallback for zero targetNum and empty reference", func(t *testing.T) {
		ev := schema.TriggerEvent{
			ID:        "zero-id",
			Source:    schema.SourceGitHub,
			EventType: schema.EventIssueOpened,
		}
		id := generateRunID(&ev)
		if !strings.HasPrefix(id, "run-github-") {
			t.Errorf("expected run-github- prefix, got %s", id)
		}

		m := NewPipelineTriggerManager(
			DefaultTriggerManagerConfig(),
			&mockPipelineSubmitter{},
			nil,
			nil,
			nil,
		)
		defer m.Close()

		// Test hydrateRunRequest when step.Config is already non-nil
		tpl := &schema.FactoryTemplate{
			Metadata: schema.Metadata{Name: "custom"},
			Spec: schema.FactorySpec{
				Steps: []schema.StepConfig{
					{
						ID:     "step-1",
						Config: map[string]any{"existing": "val"},
					},
				},
			},
		}
		req := m.hydrateRunRequest(TriggerRule{}, &ev, tpl)
		if req.Branch != "ov-run/"+req.RunID {
			t.Errorf("expected branch ov-run/%s, got %s", req.RunID, req.Branch)
		}
		if req.Template.Spec.Steps[0].Config["existing"] != "val" {
			t.Errorf("expected existing key preserved in step config")
		}
	})
}

type testPRCreatorMock struct {
	called     bool
	lastRepo   string
	lastHead   string
	lastBase   string
	lastTitle  string
	returnURL  string
	returnNum  int
	returnErr  error
}

func (m *testPRCreatorMock) CreatePullRequest(ctx context.Context, repo, head, base, title, body string) (*PullRequestResponse, error) {
	m.called = true
	m.lastRepo = repo
	m.lastHead = head
	m.lastBase = base
	m.lastTitle = title
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	url := m.returnURL
	if url == "" {
		url = fmt.Sprintf("https://github.com/%s/pull/%d", repo, m.returnNum)
	}
	return &PullRequestResponse{
		Number:  m.returnNum,
		HTMLURL: url,
		URL:     url,
		State:   "open",
		Title:   title,
		Body:    body,
	}, nil
}

func TestPipelineTriggerManager_AutonomousBridgeIntegration(t *testing.T) {
	t.Run("ready-for-agent label delegates to AutonomousBridge with PR creation and comment", func(t *testing.T) {
		runner := newMockFactoryRunner()
		runner.runOutcome = &agent.PipelineOutcome{
			TaskID:          "issue-42",
			Decision:        agent.DecisionApproved,
			TotalIterations: 1,
			Duration:        500 * time.Millisecond,
		}

		reporter := &mockIssueReporter{}
		prCreator := &testPRCreatorMock{
			returnNum: 101,
			returnURL: "https://github.com/gv-factory/gv-factory/pull/101",
		}

		bridgeCfg := DefaultBridgeConfig()
		bridgeCfg.PRCreator = prCreator
		bridge := NewAutonomousFactoryBridge(bridgeCfg, runner, nil, reporter)

		cfg := DefaultTriggerManagerConfig()
		cfg.AutonomousBridge = bridge

		mgr := NewPipelineTriggerManager(cfg, nil, nil, nil, nil)
		defer mgr.Close()

		if mgr.AutonomousBridge() != bridge {
			t.Error("expected AutonomousBridge getter to match configured bridge")
		}

		ev := schema.TriggerEvent{
			ID:          "deliv-auto-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 42,
			Sender:      "octocat",
			Payload: map[string]any{
				"action": "labeled",
				"label": map[string]any{
					"name": "ready-for-agent",
				},
				"issue": map[string]any{
					"number": 42,
					"title":  "Add user authentication",
					"body":   "Need oauth2 implementation",
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Handled {
			t.Fatalf("expected trigger to be handled, got %+v", res)
		}
		if res.RuleMatched != "github-issue-ready-for-agent" {
			t.Errorf("expected rule github-issue-ready-for-agent, got %s", res.RuleMatched)
		}
		if res.BridgeResult == nil || !res.BridgeResult.Handled {
			t.Errorf("expected non-nil handled BridgeResult, got %+v", res.BridgeResult)
		}

		// Verify PR was created
		if !prCreator.called {
			t.Error("expected PR creator to be invoked on approved issue outcome")
		}
		if prCreator.lastHead != "ov-run/issue-42" {
			t.Errorf("expected head ov-run/issue-42, got %s", prCreator.lastHead)
		}

		// Verify feedback comment was posted with PR link
		comments := reporter.getComments()
		if len(comments) != 1 {
			t.Fatalf("expected 1 comment posted, got %d", len(comments))
		}
		if !strings.Contains(comments[0].Comment, "https://github.com/gv-factory/gv-factory/pull/101") {
			t.Errorf("expected comment to contain PR link, got:\n%s", comments[0].Comment)
		}
	})

	t.Run("factory build issue comment triggers AutonomousRunner via trigger manager config", func(t *testing.T) {
		runner := newMockFactoryRunner()
		runner.runOutcome = &agent.PipelineOutcome{
			TaskID:          "issue-77",
			Decision:        agent.DecisionApproved,
			TotalIterations: 2,
			Duration:        800 * time.Millisecond,
		}

		reporter := &mockIssueReporter{}

		cfg := DefaultTriggerManagerConfig()
		cfg.AutonomousRunner = runner
		cfg.IssueReporter = reporter

		mgr := NewPipelineTriggerManager(cfg, nil, nil, nil, nil)
		defer mgr.Close()

		if mgr.AutonomousBridge() == nil {
			t.Fatal("expected AutonomousBridge to be constructed from AutonomousRunner")
		}

		ev := schema.TriggerEvent{
			ID:          "deliv-cmd-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueCommented,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 77,
			Sender:      "maintainer",
			Payload: map[string]any{
				"action": "created",
				"comment": map[string]any{
					"body": "@factory build Please implement this feature",
				},
				"issue": map[string]any{
					"number": 77,
					"title":  "Implement dark mode",
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Handled {
			t.Fatalf("expected trigger to be handled, got %+v", res)
		}
		if res.RuleMatched != "github-issue-comment-factory-build" {
			t.Errorf("expected rule github-issue-comment-factory-build, got %s", res.RuleMatched)
		}

		calls := runner.getCalls()
		if len(calls) != 1 {
			t.Fatalf("expected 1 pipeline run, got %d", len(calls))
		}
		if calls[0].TaskID != "issue-77" {
			t.Errorf("expected task ID issue-77, got %s", calls[0].TaskID)
		}
	})

	t.Run("GitLab object_attributes note with factory build comment", func(t *testing.T) {
		runner := newMockFactoryRunner()
		runner.runOutcome = &agent.PipelineOutcome{
			TaskID:   "issue-90",
			Decision: agent.DecisionApproved,
		}

		cfg := DefaultTriggerManagerConfig()
		cfg.AutonomousRunner = runner

		mgr := NewPipelineTriggerManager(cfg, nil, nil, nil, nil)
		defer mgr.Close()

		ev := schema.TriggerEvent{
			ID:          "deliv-gl-1",
			Source:      schema.SourceGitLab,
			EventType:   schema.EventIssueCommented,
			Repository:  "gitlab-org/project",
			IssueNumber: 90,
			Payload: map[string]any{
				"object_attributes": map[string]any{
					"note": "/factory build test",
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Handled {
			t.Fatalf("expected handled trigger, got %+v", res)
		}
	})

	t.Run("autonomous bridge error propagation", func(t *testing.T) {
		runner := newMockFactoryRunner()
		runner.runErr = fmt.Errorf("fatal pipeline crash")

		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), runner, nil, nil)
		cfg := DefaultTriggerManagerConfig()
		cfg.AutonomousBridge = bridge

		mgr := NewPipelineTriggerManager(cfg, nil, nil, nil, nil)
		defer mgr.Close()

		ev := schema.TriggerEvent{
			ID:          "deliv-err-1",
			Source:      schema.SourceGitHub,
			EventType:   schema.EventIssueLabeled,
			Repository:  "gv-factory/gv-factory",
			IssueNumber: 55,
			Payload: map[string]any{
				"action": "labeled",
				"label": map[string]any{
					"name": "ready-for-agent",
				},
			},
		}

		res, err := mgr.HandleTrigger(context.Background(), ev)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if res.Handled {
			t.Error("expected Handled == false on error")
		}
	})

	t.Run("SetAutonomousBridge and WithAutonomousBridge chaining", func(t *testing.T) {
		mgr := NewPipelineTriggerManager(DefaultTriggerManagerConfig(), nil, nil, nil, nil)
		defer mgr.Close()

		if mgr.AutonomousBridge() != nil {
			t.Error("expected initial bridge to be nil")
		}

		bridge := NewAutonomousFactoryBridge(DefaultBridgeConfig(), newMockFactoryRunner(), nil, nil)
		mgr.SetAutonomousBridge(bridge)
		if mgr.AutonomousBridge() != bridge {
			t.Error("expected SetAutonomousBridge to set the bridge")
		}

		bridge2 := NewAutonomousFactoryBridge(DefaultBridgeConfig(), newMockFactoryRunner(), nil, nil)
		chained := mgr.WithAutonomousBridge(bridge2)
		if chained != mgr || mgr.AutonomousBridge() != bridge2 {
			t.Error("expected WithAutonomousBridge to chain and update bridge")
		}
	})
}

