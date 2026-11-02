// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package inbound

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gv-factory/gv-factory/internal/dag"
	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

// PipelineSubmitter abstracts the Orchestrator's submission method.
type PipelineSubmitter interface {
	SubmitRun(ctx context.Context, req dag.RunRequest) (*dag.FactoryRun, error)
}

// EventPublisher abstracts gateway event broadcasting.
type EventPublisher interface {
	PublishTaskEvent(ctx context.Context, ev gateway.Event) error
}

// Deduplicator checks and stores incoming delivery IDs to prevent replay.
type Deduplicator interface {
	IsDuplicate(ctx context.Context, deliveryID string) bool
	Close() error
}

// TemplateResolver retrieves factory templates by name.
type TemplateResolver interface {
	ResolveTemplate(ctx context.Context, templateName string) (*schema.FactoryTemplate, error)
}

// MemoryDeduplicator maintains delivery IDs in memory with TTL expiration.
type MemoryDeduplicator struct {
	mu        sync.RWMutex
	seen      map[string]time.Time
	ttl       time.Duration
	stopCh    chan struct{}
	closeOnce sync.Once
	closeWg   sync.WaitGroup
}

// NewMemoryDeduplicator initializes a MemoryDeduplicator with the given TTL.
func NewMemoryDeduplicator(ttl time.Duration) *MemoryDeduplicator {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	d := &MemoryDeduplicator{
		seen:   make(map[string]time.Time),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}

	d.closeWg.Add(1)
	go d.reaper()

	return d
}

// IsDuplicate returns true if the deliveryID was already seen within its TTL.
// An empty deliveryID is never treated as a duplicate.
func (d *MemoryDeduplicator) IsDuplicate(ctx context.Context, deliveryID string) bool {
	if deliveryID == "" {
		return false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if exp, exists := d.seen[deliveryID]; exists && now.Before(exp) {
		return true
	}

	d.seen[deliveryID] = now.Add(d.ttl)
	return false
}

// Close stops the background expiration reaper.
func (d *MemoryDeduplicator) Close() error {
	d.closeOnce.Do(func() {
		close(d.stopCh)
		d.closeWg.Wait()
	})
	return nil
}

func (d *MemoryDeduplicator) reaper() {
	defer d.closeWg.Done()
	interval := d.ttl / 2
	if interval < 10*time.Millisecond {
		interval = 10 * time.Millisecond
	} else if interval > 5*time.Minute {
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.cleanExpired()
		}
	}
}

func (d *MemoryDeduplicator) cleanExpired() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for k, exp := range d.seen {
		if now.After(exp) {
			delete(d.seen, k)
		}
	}
}

// CatalogTemplateResolver resolves templates from an in-memory catalog or defaults.
type CatalogTemplateResolver struct {
	mu        sync.RWMutex
	templates map[string]*schema.FactoryTemplate
}

// NewCatalogTemplateResolver constructs a resolver preloaded with standard factory templates.
func NewCatalogTemplateResolver() *CatalogTemplateResolver {
	r := &CatalogTemplateResolver{
		templates: make(map[string]*schema.FactoryTemplate),
	}

	// Register canonical default-tdd-factory
	r.RegisterTemplate(&schema.FactoryTemplate{
		APIVersion: "gv-factory.io/v1alpha1",
		Kind:       "FactoryTemplate",
		Metadata: schema.Metadata{
			Name:        "default-tdd-factory",
			Version:     "1.0.0",
			Description: "Standard autonomous TDD factory pipeline for issues and PRs",
		},
		Spec: schema.FactorySpec{
			Entrypoint: "ingestion",
			Steps: []schema.StepConfig{
				{
					ID:        "ingestion",
					Name:      "Context Ingestion",
					Plugin:    "extism://plugins/ingestor.wasm",
					DependsOn: []string{},
				},
				{
					ID:        "spec-writer",
					Name:      "SDD & TDD Spec Writer",
					Plugin:    "extism://plugins/spec-writer.wasm",
					DependsOn: []string{"ingestion"},
				},
				{
					ID:        "builder-tdd",
					Name:      "TDD Implementation",
					Plugin:    "extism://plugins/builder-tdd.wasm",
					DependsOn: []string{"spec-writer"},
				},
				{
					ID:        "reviewer",
					Name:      "Code Quality & Review",
					Plugin:    "extism://plugins/reviewer.wasm",
					DependsOn: []string{"builder-tdd"},
				},
			},
			Gates: schema.GatesConfig{
				MinCoveragePercent: 90.0,
				MaxSecurityAlerts:  0,
				RequireApproval:    false,
			},
		},
	})

	return r
}

// RegisterTemplate adds or overwrites a template in the resolver.
func (r *CatalogTemplateResolver) RegisterTemplate(tpl *schema.FactoryTemplate) {
	if tpl == nil || tpl.Metadata.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[tpl.Metadata.Name] = tpl
}

// ResolveTemplate retrieves a template by name, falling back to "default-tdd-factory" if not found.
func (r *CatalogTemplateResolver) ResolveTemplate(ctx context.Context, templateName string) (*schema.FactoryTemplate, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if tpl, ok := r.templates[templateName]; ok {
		return cloneTemplate(tpl), nil
	}

	// Fallback to default-tdd-factory
	if def, ok := r.templates["default-tdd-factory"]; ok {
		return cloneTemplate(def), nil
	}

	return nil, fmt.Errorf("template %q not found and default template unavailable", templateName)
}

func cloneTemplate(src *schema.FactoryTemplate) *schema.FactoryTemplate {
	if src == nil {
		return nil
	}
	clone := *src
	clone.Spec.Steps = make([]schema.StepConfig, len(src.Spec.Steps))
	for i, s := range src.Spec.Steps {
		stepClone := s
		if s.Config != nil {
			stepClone.Config = make(map[string]any, len(s.Config))
			for k, v := range s.Config {
				stepClone.Config[k] = v
			}
		} else {
			stepClone.Config = make(map[string]any)
		}
		if s.DependsOn != nil {
			stepClone.DependsOn = append([]string(nil), s.DependsOn...)
		}
		clone.Spec.Steps[i] = stepClone
	}
	return &clone
}

// TriggerRule defines criteria matching inbound events to factory pipeline runs.
type TriggerRule struct {
	Name           string                             `json:"name"`
	Sources        []string                           `json:"sources,omitempty"`
	EventTypes     []string                           `json:"event_types,omitempty"`
	RequiredLabels []string                           `json:"required_labels,omitempty"`
	Repositories   []string                           `json:"repositories,omitempty"`
	TemplateName   string                             `json:"template_name,omitempty"`
	Priority       dag.Priority                       `json:"priority,omitempty"`
	CustomFilter   func(ev *schema.TriggerEvent) bool `json:"-"`
}

// TriggerManagerConfig holds the configuration for the PipelineTriggerManager.
type TriggerManagerConfig struct {
	DefaultTemplate  string        `json:"default_template"`
	Rules            []TriggerRule `json:"rules"`
	DeduplicationTTL time.Duration `json:"deduplication_ttl"`
	BaseBranch       string        `json:"base_branch"`
}

// DefaultTriggerManagerConfig returns the standard production configuration for webhook triggering.
func DefaultTriggerManagerConfig() TriggerManagerConfig {
	return TriggerManagerConfig{
		DefaultTemplate:  "default-tdd-factory",
		DeduplicationTTL: 24 * time.Hour,
		BaseBranch:       "main",
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
}

// TriggerResult holds the outcome of evaluating a trigger event.
type TriggerResult struct {
	Handled     bool            `json:"handled"`
	Skipped     bool            `json:"skipped"`
	Reason      string          `json:"reason,omitempty"`
	RunID       string          `json:"run_id,omitempty"`
	RuleMatched string          `json:"rule_matched,omitempty"`
	Run         *dag.FactoryRun `json:"run,omitempty"`
}

// PipelineTriggerManager connects inbound trigger events with the DAG Orchestrator.
type PipelineTriggerManager struct {
	cfg       TriggerManagerConfig
	submitter PipelineSubmitter
	publisher EventPublisher
	dedup     Deduplicator
	resolver  TemplateResolver
}

// NewPipelineTriggerManager initializes a PipelineTriggerManager.
func NewPipelineTriggerManager(
	cfg TriggerManagerConfig,
	submitter PipelineSubmitter,
	publisher EventPublisher,
	dedup Deduplicator,
	resolver TemplateResolver,
) *PipelineTriggerManager {
	if cfg.DefaultTemplate == "" {
		cfg.DefaultTemplate = "default-tdd-factory"
	}
	if cfg.BaseBranch == "" {
		cfg.BaseBranch = "main"
	}
	if dedup == nil {
		ttl := cfg.DeduplicationTTL
		if ttl <= 0 {
			ttl = 24 * time.Hour
		}
		dedup = NewMemoryDeduplicator(ttl)
	}
	if resolver == nil {
		resolver = NewCatalogTemplateResolver()
	}

	return &PipelineTriggerManager{
		cfg:       cfg,
		submitter: submitter,
		publisher: publisher,
		dedup:     dedup,
		resolver:  resolver,
	}
}

// Close frees any resources held by the manager (e.g. deduplicator background worker).
func (m *PipelineTriggerManager) Close() error {
	if m.dedup != nil {
		return m.dedup.Close()
	}
	return nil
}

// Dispatch implements the Dispatcher interface so it can be wired directly to Ingestor.
func (m *PipelineTriggerManager) Dispatch(ctx context.Context, ev schema.TriggerEvent) error {
	_, err := m.HandleTrigger(ctx, ev)
	return err
}

// HandleTrigger processes a single inbound trigger event through deduplication, rule evaluation and pipeline launching.
func (m *PipelineTriggerManager) HandleTrigger(ctx context.Context, ev schema.TriggerEvent) (*TriggerResult, error) {
	// 1. Deduplication check
	if ev.ID != "" && m.dedup.IsDuplicate(ctx, ev.ID) {
		return &TriggerResult{
			Handled: false,
			Skipped: true,
			Reason:  "duplicate delivery ID",
		}, nil
	}

	// 2. Evaluate rules in priority order
	var matchedRule *TriggerRule
	for i := range m.cfg.Rules {
		r := &m.cfg.Rules[i]
		if m.matchesRule(r, &ev) {
			matchedRule = r
			break
		}
	}

	if matchedRule == nil {
		return &TriggerResult{
			Handled: false,
			Skipped: true,
			Reason:  fmt.Sprintf("no matching trigger rule for event type %q", ev.EventType),
		}, nil
	}

	// 3. Resolve template
	targetTemplateName := matchedRule.TemplateName
	if targetTemplateName == "" {
		targetTemplateName = m.cfg.DefaultTemplate
	}

	tpl, err := m.resolver.ResolveTemplate(ctx, targetTemplateName)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve template %q: %w", targetTemplateName, err)
	}

	// 4. Hydrate RunRequest
	runReq := m.hydrateRunRequest(*matchedRule, &ev, tpl)

	// 5. Submit to Orchestrator
	if m.submitter == nil {
		return nil, fmt.Errorf("pipeline submitter is nil")
	}

	run, err := m.submitter.SubmitRun(ctx, runReq)
	if err != nil {
		return &TriggerResult{
			Handled: false,
			Skipped: false,
			Reason:  err.Error(),
		}, err
	}

	// 6. Publish TopicPipelineTriggered event to gateway
	if m.publisher != nil {
		_ = m.publisher.PublishTaskEvent(ctx, gateway.Event{
			Type:      gateway.TopicPipelineTriggered,
			RunID:     runReq.RunID,
			Timestamp: time.Now().UnixMilli(),
			Payload: map[string]any{
				"run_id":          runReq.RunID,
				"rule":            matchedRule.Name,
				"source":          ev.Source,
				"event_type":      ev.EventType,
				"repository":      ev.Repository,
				"issue_number":    ev.IssueNumber,
				"pr_number":       ev.PRNumber,
				"sender":          ev.Sender,
				"template":        tpl.Metadata.Name,
				"branch":          runReq.Branch,
				"delivery_id":     ev.ID,
				"pipeline_status": "QUEUED",
			},
		})
	}

	return &TriggerResult{
		Handled:     true,
		Skipped:     false,
		RunID:       runReq.RunID,
		RuleMatched: matchedRule.Name,
		Run:         run,
	}, nil
}

func (m *PipelineTriggerManager) matchesRule(rule *TriggerRule, ev *schema.TriggerEvent) bool {
	// Source match
	if len(rule.Sources) > 0 {
		var sourceMatched bool
		for _, s := range rule.Sources {
			if strings.EqualFold(s, ev.Source) {
				sourceMatched = true
				break
			}
		}
		if !sourceMatched {
			return false
		}
	}

	// Event type match
	if len(rule.EventTypes) > 0 {
		var typeMatched bool
		for _, t := range rule.EventTypes {
			if strings.EqualFold(t, ev.EventType) {
				typeMatched = true
				break
			}
		}
		if !typeMatched {
			return false
		}
	}

	// Repositories match
	if len(rule.Repositories) > 0 {
		var repoMatched bool
		for _, r := range rule.Repositories {
			if strings.EqualFold(r, ev.Repository) {
				repoMatched = true
				break
			}
		}
		if !repoMatched {
			return false
		}
	}

	// Required labels match
	if len(rule.RequiredLabels) > 0 {
		labels := extractLabelsFromPayload(ev.Payload)
		for _, reqLabel := range rule.RequiredLabels {
			var hasLabel bool
			for _, l := range labels {
				if strings.EqualFold(l, reqLabel) {
					hasLabel = true
					break
				}
			}
			if !hasLabel {
				return false
			}
		}
	}

	// Custom filter if defined
	if rule.CustomFilter != nil {
		if !rule.CustomFilter(ev) {
			return false
		}
	}

	return true
}

func extractLabelsFromPayload(payload map[string]any) []string {
	var labels []string
	if payload == nil {
		return labels
	}

	// 1. Check direct "label" field (e.g. from "labeled" event)
	if lbl, ok := payload["label"].(map[string]any); ok {
		if name, ok := lbl["name"].(string); ok && name != "" {
			labels = append(labels, name)
		}
	}

	// 2. Check "issue.labels" array
	if issue, ok := payload["issue"].(map[string]any); ok {
		if lblList, ok := issue["labels"].([]any); ok {
			for _, item := range lblList {
				if m, ok := item.(map[string]any); ok {
					if name, ok := m["name"].(string); ok && name != "" {
						labels = append(labels, name)
					}
				}
			}
		}
	}

	// 3. Check "pull_request.labels" array
	if pr, ok := payload["pull_request"].(map[string]any); ok {
		if lblList, ok := pr["labels"].([]any); ok {
			for _, item := range lblList {
				if m, ok := item.(map[string]any); ok {
					if name, ok := m["name"].(string); ok && name != "" {
						labels = append(labels, name)
					}
				}
			}
		}
	}

	return labels
}

func (m *PipelineTriggerManager) hydrateRunRequest(
	rule TriggerRule,
	ev *schema.TriggerEvent,
	tpl *schema.FactoryTemplate,
) dag.RunRequest {
	runID := generateRunID(ev)

	// Extract titles and bodies from payload
	var issueTitle, issueBody string
	if issue, ok := ev.Payload["issue"].(map[string]any); ok {
		if t, ok := issue["title"].(string); ok {
			issueTitle = t
		}
		if b, ok := issue["body"].(string); ok {
			issueBody = b
		}
	} else if pr, ok := ev.Payload["pull_request"].(map[string]any); ok {
		if t, ok := pr["title"].(string); ok {
			issueTitle = t
		}
		if b, ok := pr["body"].(string); ok {
			issueBody = b
		}
	}

	targetNumber := ev.IssueNumber
	if targetNumber == 0 {
		targetNumber = ev.PRNumber
	}

	branch := ev.Reference
	if branch == "" || strings.HasPrefix(branch, "refs/heads/") {
		if targetNumber > 0 {
			branch = fmt.Sprintf("ov-run/issue-%d", targetNumber)
		} else {
			branch = fmt.Sprintf("ov-run/%s", runID)
		}
	}

	baseBranch := m.cfg.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}

	labels := make(map[string]string)
	labels["source"] = ev.Source
	labels["event_type"] = ev.EventType
	if ev.Repository != "" {
		labels["repository"] = ev.Repository
	}
	if targetNumber > 0 {
		labels["issue_number"] = strconv.Itoa(targetNumber)
	}
	if ev.Sender != "" {
		labels["sender"] = ev.Sender
	}
	if ev.ID != "" {
		labels["delivery_id"] = ev.ID
	}
	if issueTitle != "" {
		labels["title"] = issueTitle
	}

	priority := rule.Priority
	if priority == 0 {
		priority = dag.PriorityNormal
	}

	// Propagate context into template steps
	if tpl != nil {
		for i := range tpl.Spec.Steps {
			st := &tpl.Spec.Steps[i]
			if st.Config == nil {
				st.Config = make(map[string]any)
			}
			st.Config["source"] = ev.Source
			st.Config["event_type"] = ev.EventType
			st.Config["repository"] = ev.Repository
			st.Config["sender"] = ev.Sender
			st.Config["branch"] = branch
			st.Config["base_branch"] = baseBranch
			st.Config["delivery_id"] = ev.ID
			if targetNumber > 0 {
				st.Config["issue_number"] = strconv.Itoa(targetNumber)
			}
			if issueTitle != "" {
				st.Config["issue_title"] = issueTitle
			}
			if issueBody != "" {
				st.Config["issue_body"] = issueBody
			}
		}
	}

	return dag.RunRequest{
		RunID:      runID,
		Template:   *tpl,
		RepoPath:   ".",
		Branch:     branch,
		BaseBranch: baseBranch,
		Priority:   priority,
		Labels:     labels,
	}
}

func generateRunID(ev *schema.TriggerEvent) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	randSuffix := hex.EncodeToString(b)

	targetNum := ev.IssueNumber
	if targetNum == 0 {
		targetNum = ev.PRNumber
	}

	if targetNum > 0 {
		return fmt.Sprintf("run-issue-%d-%s", targetNum, randSuffix)
	}
	return fmt.Sprintf("run-%s-%s", ev.Source, randSuffix)
}
