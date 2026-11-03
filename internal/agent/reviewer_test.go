// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/gv-factory/gv-factory/internal/brain"
	"github.com/gv-factory/gv-factory/internal/connectors/llm"
)

func TestSecurityReviewer_CleanDiff(t *testing.T) {
	reviewer := NewSecurityReviewer()
	ctx := context.Background()

	diff := `diff --git a/pkg/util/math.go b/pkg/util/math.go
--- a/pkg/util/math.go
+++ b/pkg/util/math.go
@@ -1,5 +1,6 @@
 package util
 
+func Add(a, b int) int {
+	return a + b
+}
`
	req := ReviewRequest{
		RunID:        "run-clean-01",
		Diff:         diff,
		FilesChanged: []string{"pkg/util/math.go"},
	}

	result, err := reviewer.Review(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Passed {
		t.Errorf("expected clean diff to pass, got passed=false (findings: %+v)", result.Findings)
	}
	if result.Score < 0.99 {
		t.Errorf("expected score ~1.0 for clean diff, got %f", result.Score)
	}
	if len(result.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(result.Findings))
	}
	if result.Severity != SeverityInfo {
		t.Errorf("expected severity INFO, got %s", result.Severity)
	}
}

func TestSecurityReviewer_DetectsSecrets(t *testing.T) {
	reviewer := NewSecurityReviewer()
	ctx := context.Background()

	tests := []struct {
		name     string
		diff     string
		expected string
		ruleCode string
	}{
		{
			name: "AWS Access Key",
			diff: `diff --git a/config.go b/config.go
+++ b/config.go
@@ -1,2 +1,3 @@
+const awsKey = "AKIAIOSFODNN7EXAMPLE"
`,
			expected: "AWS Access Key",
			ruleCode: "SEC-SECRET-001",
		},
		{
			name: "Private Key Block",
			diff: `diff --git a/cert.pem b/cert.pem
+++ b/cert.pem
@@ -1,1 +1,2 @@
+-----BEGIN RSA PRIVATE KEY-----
`,
			expected: "Private key",
			ruleCode: "SEC-SECRET-002",
		},
		{
			name: "Hardcoded API Key / Token",
			diff: `diff --git a/client.go b/client.go
+++ b/client.go
@@ -10,1 +10,2 @@
+var apiKey = "ghp_1234567890abcdefghijklmnopqrstuvwxyz"
`,
			expected: "secret or token",
			ruleCode: "SEC-SECRET-003",
		},
		{
			name: "Hardcoded Password",
			diff: `diff --git a/db.go b/db.go
+++ b/db.go
@@ -5,1 +5,2 @@
+password := "SuperSecret1234!"
`,
			expected: "password",
			ruleCode: "SEC-SECRET-004",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := ReviewRequest{
				RunID: "run-sec-" + tt.name,
				Diff:  tt.diff,
			}
			res, err := reviewer.Review(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Passed {
				t.Errorf("expected reviewer to fail on secret, but passed=true")
			}
			if res.Severity != SeverityCritical && res.Severity != SeverityError {
				t.Errorf("expected CRITICAL or ERROR severity, got %s", res.Severity)
			}
			if len(res.Findings) == 0 {
				t.Fatalf("expected at least 1 finding, got 0")
			}
			found := false
			for _, f := range res.Findings {
				if strings.Contains(strings.ToLower(f.Message), strings.ToLower(tt.expected)) ||
					strings.HasPrefix(f.RuleCode, "SEC-SECRET") {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("did not find expected finding '%s' in %+v", tt.expected, res.Findings)
			}
		})
	}
}

func TestSecurityReviewer_DetectsInjections(t *testing.T) {
	reviewer := NewSecurityReviewer()
	ctx := context.Background()

	tests := []struct {
		name     string
		diff     string
		ruleCode string
	}{
		{
			name: "SQL Injection via Sprintf",
			diff: `diff --git a/repo.go b/repo.go
+++ b/repo.go
@@ -20,2 +20,3 @@
+query := fmt.Sprintf("SELECT * FROM users WHERE id = '%s'", userID)
+db.Query(query)
`,
			ruleCode: "SEC-INJECT-SQL",
		},
		{
			name: "SQL Injection via Concatenation",
			diff: `diff --git a/repo.go b/repo.go
+++ b/repo.go
@@ -10,2 +10,3 @@
+rows, err := db.Query("SELECT id FROM accounts WHERE name = " + userInput)
`,
			ruleCode: "SEC-INJECT-SQL",
		},
		{
			name: "Command Injection via Shell",
			diff: `diff --git a/cmd.go b/cmd.go
+++ b/cmd.go
@@ -15,2 +15,3 @@
+exec.Command("sh", "-c", "echo " + userInput)
`,
			ruleCode: "SEC-INJECT-CMD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-inj", Diff: tt.diff})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Passed {
				t.Errorf("expected failure on injection, got passed=true")
			}
			found := false
			for _, f := range res.Findings {
				if f.RuleCode == tt.ruleCode {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected rule code %s in findings: %+v", tt.ruleCode, res.Findings)
			}
		})
	}
}

func TestSecurityReviewer_DetectsInsecurePermissionsAndPatterns(t *testing.T) {
	reviewer := NewSecurityReviewer()
	ctx := context.Background()

	diff := `diff --git a/file.go b/file.go
+++ b/file.go
@@ -10,3 +10,6 @@
+os.Chmod("/tmp/secret", 0777)
+tlsConfig := &tls.Config{InsecureSkipVerify: true}
+h := md5.New()
`
	res, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-perm", Diff: diff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Passed {
		t.Errorf("expected failure for insecure permissions and patterns, got passed=true")
	}

	var foundPerm, foundTLS, foundMD5 bool
	for _, f := range res.Findings {
		if f.RuleCode == "SEC-PERM-0777" {
			foundPerm = true
		}
		if f.RuleCode == "SEC-TLS-INSECURE" {
			foundTLS = true
		}
		if f.RuleCode == "SEC-CRYPTO-WEAK" {
			foundMD5 = true
		}
	}

	if !foundPerm {
		t.Errorf("expected SEC-PERM-0777 finding in %+v", res.Findings)
	}
	if !foundTLS {
		t.Errorf("expected SEC-TLS-INSECURE finding in %+v", res.Findings)
	}
	if !foundMD5 {
		t.Errorf("expected SEC-CRYPTO-WEAK finding in %+v", res.Findings)
	}
}

func TestSecurityReviewer_ContextCanceled(t *testing.T) {
	reviewer := NewSecurityReviewer()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-cancel", Diff: "+line"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestArchitectureReviewer_CleanDiff(t *testing.T) {
	rules := []brain.ArchitecturalRule{
		{
			RuleCode:    "ARCH-BOUNDARY-001",
			Title:       "Internal boundary",
			Description: "pkg/ must not import internal/",
			Severity:    SeverityError,
		},
	}
	reviewer := NewArchitectureReviewer(nil, rules...)
	ctx := context.Background()

	diff := `diff --git a/internal/agent/clean.go b/internal/agent/clean.go
+++ b/internal/agent/clean.go
@@ -0,0 +1,10 @@
+// SPDX-License-Identifier: Apache-2.0
+// SPDX-FileCopyrightText: 2026 gv-factory contributors
+
+package agent
+
+import (
+	"context"
+)
+
+func DoSomething(ctx context.Context) error { return nil }
`
	req := ReviewRequest{
		RunID:        "run-arch-clean",
		Diff:         diff,
		FilesChanged: []string{"internal/agent/clean.go"},
	}

	res, err := reviewer.Review(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !res.Passed {
		t.Errorf("expected architecture clean diff to pass, got findings: %+v", res.Findings)
	}
	if res.Score < 0.99 {
		t.Errorf("expected score ~1.0, got %f", res.Score)
	}
	if len(res.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(res.Findings))
	}
}

func TestArchitectureReviewer_ViolatesPackageBoundary(t *testing.T) {
	reviewer := NewArchitectureReviewer(nil)
	ctx := context.Background()

	diff := `diff --git a/pkg/shared/util.go b/pkg/shared/util.go
+++ b/pkg/shared/util.go
@@ -1,5 +1,8 @@
 package shared
 
+import (
+	"github.com/gv-factory/gv-factory/internal/brain"
+)
`
	req := ReviewRequest{
		RunID:        "run-arch-bound",
		Diff:         diff,
		FilesChanged: []string{"pkg/shared/util.go"},
	}

	res, err := reviewer.Review(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Passed {
		t.Errorf("expected failure for pkg importing internal, got passed=true")
	}

	found := false
	for _, f := range res.Findings {
		if f.RuleCode == "ARCH-BOUNDARY-PKG-INTERNAL" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ARCH-BOUNDARY-PKG-INTERNAL finding, got %+v", res.Findings)
	}
}

func TestArchitectureReviewer_ViolatesSandboxCheckoutRule(t *testing.T) {
	reviewer := NewArchitectureReviewer(nil)
	ctx := context.Background()

	diff := `diff --git a/internal/sandbox/runner.go b/internal/sandbox/runner.go
+++ b/internal/sandbox/runner.go
@@ -10,3 +10,4 @@
 func run() {
+	exec.Command("git", "checkout", "main").Run()
 }
`
	req := ReviewRequest{
		RunID:        "run-arch-checkout",
		Diff:         diff,
		FilesChanged: []string{"internal/sandbox/runner.go"},
	}

	res, err := reviewer.Review(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Passed {
		t.Errorf("expected failure for git checkout in sandbox, got passed=true")
	}

	found := false
	for _, f := range res.Findings {
		if f.RuleCode == "ARCH-NO-GIT-CHECKOUT" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ARCH-NO-GIT-CHECKOUT finding, got %+v", res.Findings)
	}
}

func TestArchitectureReviewer_MissingSPDXHeaderInNewGoFile(t *testing.T) {
	reviewer := NewArchitectureReviewer(nil)
	ctx := context.Background()

	diff := `diff --git a/internal/agent/new_service.go b/internal/agent/new_service.go
new file mode 100644
--- /dev/null
+++ b/internal/agent/new_service.go
@@ -0,0 +1,5 @@
+package agent
+
+func NewService() {}
`
	req := ReviewRequest{
		RunID:        "run-spdx",
		Diff:         diff,
		FilesChanged: []string{"internal/agent/new_service.go"},
	}

	res, err := reviewer.Review(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, f := range res.Findings {
		if f.RuleCode == "ARCH-SPDX-HEADER" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ARCH-SPDX-HEADER finding for missing SPDX header in new file, got %+v", res.Findings)
	}
}

func TestArchitectureReviewer_WithBrainKnowledgeSource(t *testing.T) {
	mockB := &mockBrain{
		rules: []brain.ArchitecturalRule{
			{
				RuleCode:    "ARCH-ADR-004",
				Title:       "Direct main commit forbidden",
				Description: "Never push directly to main branch",
				Severity:    SeverityError,
			},
		},
	}

	reviewer := NewArchitectureReviewer(mockB)
	ctx := context.Background()

	diff := `diff --git a/git.go b/git.go
+++ b/git.go
@@ -5,2 +5,3 @@
+// commit directly to main
+git push origin main
`
	req := ReviewRequest{
		RunID: "run-adr",
		Diff:  diff,
	}

	res, err := reviewer.Review(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mockB.callsRules == 0 {
		t.Errorf("expected brain ListRules to be consulted")
	}

	found := false
	for _, f := range res.Findings {
		if f.RuleCode == "ARCH-ADR-004" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ARCH-ADR-004 finding from brain rules, got %+v", res.Findings)
	}
}

func TestArchitectureReviewer_BrainErrorResilience(t *testing.T) {
	mockB := &mockBrain{
		errRules: errors.New("database connection refused"),
	}

	reviewer := NewArchitectureReviewer(mockB)
	ctx := context.Background()

	diff := `diff --git a/pkg/util/math.go b/pkg/util/math.go
+++ b/pkg/util/math.go
@@ -1,2 +1,3 @@
+func Safe() {}
`
	res, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-err", Diff: diff})
	if err != nil {
		t.Fatalf("expected reviewer to be resilient to brain error, got: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil review result")
	}
}

func TestArchitectureReviewer_ContextCanceled(t *testing.T) {
	reviewer := NewArchitectureReviewer(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-cancel", Diff: "+line"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// mockReviewerLLMClient implements llm.LLMClient for testing semantic reviewers.
type mockReviewerLLMClient struct {
	mu           sync.Mutex
	calls        []*llm.ChatRequest
	response     *llm.ChatResponse
	err          error
	responseFunc func(req *llm.ChatRequest) (*llm.ChatResponse, error)
}

func (m *mockReviewerLLMClient) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	if m.responseFunc != nil {
		return m.responseFunc(req)
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}

func (m *mockReviewerLLMClient) StreamChatCompletion(ctx context.Context, req *llm.ChatRequest) (<-chan llm.ChatChunk, <-chan error) {
	chunkCh := make(chan llm.ChatChunk)
	errCh := make(chan error, 1)
	close(chunkCh)
	close(errCh)
	return chunkCh, errCh
}

func TestSecurityReviewer_WithLLM_CleanDiff(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: `{"findings":[], "summary":"No security issues or data races detected."}`,
			},
		},
	}

	reviewer := NewSecurityReviewerWithLLM(mockLLM, "test-model")
	ctx := context.Background()

	diff := `diff --git a/pkg/service.go b/pkg/service.go
+++ b/pkg/service.go
@@ -1,3 +1,5 @@
 package pkg
+func Safe() int { return 42 }
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-sec-llm-clean",
		Diff:         diff,
		FilesChanged: []string{"pkg/service.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected clean diff to pass, got: %+v", res)
	}
	if len(res.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(res.Findings))
	}
	if len(mockLLM.calls) != 1 {
		t.Errorf("expected 1 LLM call, got %d", len(mockLLM.calls))
	}
	if mockLLM.calls[0].Model != "test-model" {
		t.Errorf("expected model 'test-model', got %s", mockLLM.calls[0].Model)
	}
}

func TestSecurityReviewer_WithLLM_DetectsVulnerabilityAndDataRace(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role: llm.RoleAssistant,
				Content: `{
  "findings": [
    {
      "file": "internal/cache/cache.go",
      "line": 42,
      "rule_code": "SEC-RACE-001",
      "message": "Potential data race on shared map access across goroutines without sync.RWMutex",
      "severity": "CRITICAL"
    },
    {
      "file": "internal/cache/cache.go",
      "line": 50,
      "rule_code": "SEC-INJECT-CUSTOM",
      "message": "Unsanitized user input fed into raw template execution",
      "severity": "ERROR"
    }
  ],
  "summary": "Detected critical data race and template injection."
}`,
			},
		},
	}

	reviewer := NewSecurityReviewer(WithLLM(mockLLM))
	ctx := context.Background()

	diff := `diff --git a/internal/cache/cache.go b/internal/cache/cache.go
+++ b/internal/cache/cache.go
@@ -40,3 +40,12 @@
+var cacheMap = make(map[string]any)
+func Set(k string, v any) {
+    go func() { cacheMap[k] = v }()
+}
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-sec-llm-race",
		Diff:         diff,
		FilesChanged: []string{"internal/cache/cache.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("expected review with critical data race and injection to fail")
	}
	if res.Severity != SeverityCritical {
		t.Errorf("expected CRITICAL severity, got %s", res.Severity)
	}
	if len(res.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(res.Findings))
	}

	var foundRace, foundInject bool
	for _, f := range res.Findings {
		if f.RuleCode == "SEC-RACE-001" {
			foundRace = true
		}
		if f.RuleCode == "SEC-INJECT-CUSTOM" {
			foundInject = true
		}
	}
	if !foundRace || !foundInject {
		t.Errorf("expected findings for race and inject, got %+v", res.Findings)
	}
}

func TestSecurityReviewer_WithLLM_NetworkErrorFallback_WithSecret(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		err: errors.New("dial tcp 127.0.0.1:11434: connect: connection refused"),
	}

	reviewer := NewSecurityReviewer().WithLLM(mockLLM)
	ctx := context.Background()

	diff := `diff --git a/config.go b/config.go
+++ b/config.go
@@ -1,2 +1,3 @@
+const awsKey = "AKIAIOSFODNN7EXAMPLE"
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-sec-net-err",
		Diff:  diff,
	})
	if err != nil {
		t.Fatalf("expected reviewer to fall back gracefully on network error, got err: %v", err)
	}
	if res.Passed {
		t.Errorf("expected deterministic secret detection to fail review during fallback")
	}
	if res.Severity != SeverityCritical {
		t.Errorf("expected CRITICAL severity from deterministic secret detection, got %s", res.Severity)
	}

	foundSecret := false
	for _, f := range res.Findings {
		if f.RuleCode == "SEC-SECRET-001" {
			foundSecret = true
			break
		}
	}
	if !foundSecret {
		t.Errorf("expected SEC-SECRET-001 finding from deterministic fallback, got %+v", res.Findings)
	}
}

func TestSecurityReviewer_WithLLM_NetworkErrorFallback_Clean(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		err: errors.New("remote LLM provider timed out"),
	}

	reviewer := NewSecurityReviewerWithLLM(mockLLM)
	ctx := context.Background()

	diff := `diff --git a/math.go b/math.go
+++ b/math.go
@@ -1,2 +1,3 @@
+func Sub(a, b int) int { return a - b }
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-sec-net-clean",
		Diff:  diff,
	})
	if err != nil {
		t.Fatalf("unexpected error during offline fallback: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected clean diff to pass in offline fallback, got: %+v", res)
	}
	if len(res.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(res.Findings))
	}
}

func TestSecurityReviewer_WithLLM_InvalidJSONFallback(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: "Here is my review: everything looks okay!",
			},
		},
	}

	reviewer := NewSecurityReviewerWithLLM(mockLLM)
	ctx := context.Background()

	diff := `diff --git a/math.go b/math.go
+++ b/math.go
@@ -1,2 +1,3 @@
+func Safe() {}
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-sec-invalid-json",
		Diff:  diff,
	})
	if err != nil {
		t.Fatalf("unexpected error on invalid JSON fallback: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected clean diff to pass despite invalid LLM JSON, got: %+v", res)
	}
}

func TestSecurityReviewer_WithLLM_MarkdownCodeFences(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role: llm.RoleAssistant,
				Content: "```json\n" + `{
  "findings": [
    {
      "file": "api.go",
      "line": 12,
      "rule_code": "SEC-SECRET-003",
      "message": "Generic secret in markdown block",
      "severity": "CRITICAL"
    }
  ],
  "summary": "Markdown fenced findings"
}` + "\n```",
			},
		},
	}

	reviewer := NewSecurityReviewerWithLLM(mockLLM)
	ctx := context.Background()

	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-sec-fences",
		Diff:  "+line",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("expected failure from markdown block finding")
	}
	if len(res.Findings) != 1 || res.Findings[0].RuleCode != "SEC-SECRET-003" {
		t.Errorf("expected SEC-SECRET-003 finding from markdown fenced JSON, got %+v", res.Findings)
	}
}

func TestSecurityReviewer_WithLLM_EmptyContent(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: "   \n\t  ",
			},
		},
	}

	reviewer := NewSecurityReviewerWithLLM(mockLLM)
	ctx := context.Background()

	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-sec-empty",
		Diff:  "+line",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected clean diff to pass on empty response, got: %+v", res)
	}
}

func TestSecurityReviewer_ContextCanceledDuringLLM(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	mockLLM := &mockReviewerLLMClient{
		responseFunc: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
			cancel()
			return nil, context.Canceled
		},
	}

	reviewer := NewSecurityReviewerWithLLM(mockLLM)
	_, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-sec-cancel",
		Diff:  "+line",
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestArchitectureReviewer_WithLLM_CleanDiff(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: `{"findings":[], "summary":"Architectural design and boundaries respected."}`,
			},
		},
	}

	reviewer := NewArchitectureReviewerWithLLM(nil, mockLLM)
	ctx := context.Background()

	diff := `diff --git a/internal/agent/clean.go b/internal/agent/clean.go
+++ b/internal/agent/clean.go
@@ -0,0 +1,5 @@
+// SPDX-License-Identifier: Apache-2.0
+// SPDX-FileCopyrightText: 2026 gv-factory contributors
+
+package agent
+func Clean() {}
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-arch-llm-clean",
		Diff:         diff,
		FilesChanged: []string{"internal/agent/clean.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected architectural review to pass, got: %+v", res)
	}
	if len(res.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(res.Findings))
	}
}

func TestArchitectureReviewer_WithLLM_DetectsViolations(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role: llm.RoleAssistant,
				Content: `{
  "findings": [
    {
      "file": "pkg/client/client.go",
      "line": 15,
      "rule_code": "ARCH-CONCURRENCY-LEAK",
      "message": "Architectural hazard: background worker goroutine spawned without cancellation context or lifecycle stop hook",
      "severity": "ERROR"
    }
  ],
  "summary": "Architectural data race/leak hazard detected"
}`,
			},
		},
	}

	reviewer := NewArchitectureReviewer(nil).WithLLM(mockLLM, "custom-arch-model")
	ctx := context.Background()

	diff := `diff --git a/pkg/client/client.go b/pkg/client/client.go
+++ b/pkg/client/client.go
@@ -10,3 +10,6 @@
+func StartWorker() {
+    go func() { for { tick() } }()
+}
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-arch-llm-viol",
		Diff:         diff,
		FilesChanged: []string{"pkg/client/client.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("expected architectural review with leak finding to fail")
	}
	if len(res.Findings) != 1 || res.Findings[0].RuleCode != "ARCH-CONCURRENCY-LEAK" {
		t.Errorf("expected ARCH-CONCURRENCY-LEAK finding, got %+v", res.Findings)
	}
}

func TestArchitectureReviewer_WithLLM_NetworkErrorFallback(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		err: errors.New("network unreachable"),
	}

	reviewer := NewArchitectureReviewerWithLLM(nil, mockLLM)
	ctx := context.Background()

	diff := `diff --git a/pkg/shared/util.go b/pkg/shared/util.go
+++ b/pkg/shared/util.go
@@ -1,5 +1,8 @@
 package shared
 
+import (
+	"github.com/gv-factory/gv-factory/internal/brain"
+)
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-arch-fallback",
		Diff:         diff,
		FilesChanged: []string{"pkg/shared/util.go"},
	})
	if err != nil {
		t.Fatalf("expected fallback on network error, got err: %v", err)
	}
	if res.Passed {
		t.Errorf("expected deterministic package boundary finding during fallback")
	}

	foundBound := false
	for _, f := range res.Findings {
		if f.RuleCode == "ARCH-BOUNDARY-PKG-INTERNAL" {
			foundBound = true
			break
		}
	}
	if !foundBound {
		t.Errorf("expected ARCH-BOUNDARY-PKG-INTERNAL finding, got %+v", res.Findings)
	}
}

func TestArchitectureReviewer_WithLLM_InvalidJSONFallback(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: "Invalid JSON response from model",
			},
		},
	}

	reviewer := NewArchitectureReviewerWithLLM(nil, mockLLM)
	ctx := context.Background()

	diff := `diff --git a/internal/agent/clean.go b/internal/agent/clean.go
+++ b/internal/agent/clean.go
@@ -0,0 +1,5 @@
+// SPDX-License-Identifier: Apache-2.0
+// SPDX-FileCopyrightText: 2026 gv-factory contributors
+
+package agent
+func Clean() {}
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-arch-inv-json",
		Diff:         diff,
		FilesChanged: []string{"internal/agent/clean.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error on invalid JSON fallback: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected clean diff to pass despite invalid JSON, got %+v", res)
	}
}

func TestArchitectureReviewer_WithLLM_MarkdownCodeFences(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role: llm.RoleAssistant,
				Content: "```json\n" + `{
  "findings": [
    {
      "file": "main.go",
      "line": 8,
      "rule_code": "ARCH-RULE-VIOLATION",
      "message": "Direct commit to main detected by LLM",
      "severity": "ERROR"
    }
  ],
  "summary": "Fenced architectural violation"
}` + "\n```",
			},
		},
	}

	reviewer := NewArchitectureReviewerWithLLM(nil, mockLLM)
	ctx := context.Background()

	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-arch-fences",
		Diff:  "+line",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("expected failure from fenced architectural violation")
	}
	if len(res.Findings) != 1 || res.Findings[0].RuleCode != "ARCH-RULE-VIOLATION" {
		t.Errorf("expected ARCH-RULE-VIOLATION finding, got %+v", res.Findings)
	}
}

func TestSemanticReviewer_CleanDiff_WithLLM(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: `{"findings":[], "summary":"All security, concurrency, and architectural audits passed."}`,
			},
		},
	}

	reviewer := NewSemanticReviewer(mockLLM, WithReviewerModel("claude-test"))
	ctx := context.Background()

	diff := `diff --git a/pkg/math/calc.go b/pkg/math/calc.go
+++ b/pkg/math/calc.go
@@ -0,0 +1,5 @@
+package math
+func Mult(a, b int) int { return a * b }
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-sem-clean",
		Diff:         diff,
		FilesChanged: []string{"pkg/math/calc.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected semantic review to pass cleanly, got: %+v", res)
	}
	if res.ReviewerName != "SemanticReviewer" {
		t.Errorf("expected ReviewerName 'SemanticReviewer', got %s", res.ReviewerName)
	}
}

func TestSemanticReviewer_DetectsSecurityDataRaceAndArchitecture_WithLLM(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role: llm.RoleAssistant,
				Content: `{
  "findings": [
    {
      "file": "server.go",
      "line": 20,
      "rule_code": "SEC-RACE-DATA",
      "message": "Unsynchronized concurrent slice append induces data race",
      "severity": "CRITICAL"
    },
    {
      "file": "server.go",
      "line": 35,
      "rule_code": "ARCH-PKG-VIOLATION",
      "message": "Direct bypass of domain architecture boundary",
      "severity": "ERROR"
    }
  ],
  "summary": "Detected critical data race and architectural boundary violation"
}`,
			},
		},
	}

	reviewer := NewSemanticReviewer(nil).WithLLM(mockLLM)
	ctx := context.Background()

	diff := `diff --git a/server.go b/server.go
+++ b/server.go
@@ -10,3 +10,10 @@
+var items []string
+func AppendItem(s string) {
+    go func() { items = append(items, s) }()
+}
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-sem-findings",
		Diff:         diff,
		FilesChanged: []string{"server.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("expected semantic review to fail with findings")
	}
	if len(res.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(res.Findings))
	}
	if res.Severity != SeverityCritical {
		t.Errorf("expected CRITICAL severity, got %s", res.Severity)
	}
}

func TestSemanticReviewer_NilLLM_OfflineFallback(t *testing.T) {
	// With nil LLM, both deterministic security and architecture checks must run
	reviewer := NewSemanticReviewer(nil)
	ctx := context.Background()

	diff := `diff --git a/pkg/service.go b/pkg/service.go
+++ b/pkg/service.go
@@ -1,5 +1,8 @@
 package pkg
+import "github.com/gv-factory/gv-factory/internal/brain"
+const awsKey = "AKIAIOSFODNN7EXAMPLE"
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID:        "run-sem-offline",
		Diff:         diff,
		FilesChanged: []string{"pkg/service.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error on offline fallback: %v", err)
	}
	if res.Passed {
		t.Errorf("expected offline fallback to detect both secret and package boundary violations")
	}

	var foundSecret, foundBoundary bool
	for _, f := range res.Findings {
		if f.RuleCode == "SEC-SECRET-001" {
			foundSecret = true
		}
		if f.RuleCode == "ARCH-BOUNDARY-PKG-INTERNAL" {
			foundBoundary = true
		}
	}
	if !foundSecret || !foundBoundary {
		t.Errorf("expected both SEC-SECRET-001 and ARCH-BOUNDARY-PKG-INTERNAL in offline fallback, got %+v", res.Findings)
	}
}

func TestSemanticReviewer_NetworkErrorFallback(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		err: errors.New("503 Service Unavailable"),
	}

	reviewer := NewSemanticReviewer(mockLLM)
	ctx := context.Background()

	diff := `diff --git a/file.go b/file.go
+++ b/file.go
@@ -1,2 +1,3 @@
+os.Chmod("/tmp/secret", 0777)
`
	res, err := reviewer.Review(ctx, ReviewRequest{
		RunID: "run-sem-net-fallback",
		Diff:  diff,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("expected deterministic SEC-PERM-0777 to fail review during fallback")
	}

	foundPerm := false
	for _, f := range res.Findings {
		if f.RuleCode == "SEC-PERM-0777" {
			foundPerm = true
			break
		}
	}
	if !foundPerm {
		t.Errorf("expected SEC-PERM-0777 finding from deterministic fallback, got %+v", res.Findings)
	}
}

func TestSemanticReviewer_InvalidJSONFallback(t *testing.T) {
	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: "Malformed output without JSON braces",
			},
		},
	}

	reviewer := NewSemanticReviewer(mockLLM)
	ctx := context.Background()

	diff := `diff --git a/safe.go b/safe.go
+++ b/safe.go
@@ -1,2 +1,3 @@
+func Safe() {}
`
	res, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-sem-bad-json", Diff: diff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("expected clean diff to pass despite bad JSON output")
	}
}

func TestSemanticReviewer_ContextCanceled(t *testing.T) {
	reviewer := NewSemanticReviewer(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-sem-cancel", Diff: "+line"})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestSemanticReviewer_WithBrainAndRules(t *testing.T) {
	mockB := &mockBrain{
		rules: []brain.ArchitecturalRule{
			{
				RuleCode:    "RULE-ADR-10",
				Title:       "Do not commit to main",
				Description: "main push forbidden",
				Severity:    SeverityError,
			},
		},
	}

	mockLLM := &mockReviewerLLMClient{
		response: &llm.ChatResponse{
			Message: llm.ChatMessage{
				Role:    llm.RoleAssistant,
				Content: `{"findings":[], "summary":"Clean"}`,
			},
		},
	}

	reviewer := NewSemanticReviewer(mockLLM,
		WithReviewerBrain(mockB),
		WithReviewerRules(brain.ArchitecturalRule{
			RuleCode:    "RULE-EXPLICIT-1",
			Title:       "Explicit rule",
			Description: "Must follow conventions",
			Severity:    SeverityWarn,
		}),
	)
	ctx := context.Background()

	diff := `diff --git a/git.go b/git.go
+++ b/git.go
@@ -1,2 +1,3 @@
+git push origin main
`
	res, err := reviewer.Review(ctx, ReviewRequest{RunID: "run-sem-rules", Diff: diff})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("expected main push rule violation to fail review")
	}
}

func TestReviewer_HelperFunctions(t *testing.T) {
	// 1. normalizeSeverity
	if normalizeSeverity("critical") != SeverityCritical {
		t.Errorf("expected CRITICAL, got %s", normalizeSeverity("critical"))
	}
	if normalizeSeverity("error") != SeverityError {
		t.Errorf("expected ERROR, got %s", normalizeSeverity("error"))
	}
	if normalizeSeverity("warning") != SeverityWarn {
		t.Errorf("expected WARN, got %s", normalizeSeverity("warning"))
	}
	if normalizeSeverity("informational") != SeverityInfo {
		t.Errorf("expected INFO, got %s", normalizeSeverity("informational"))
	}
	if normalizeSeverity("unknown_value") != SeverityWarn {
		t.Errorf("expected WARN fallback for unknown, got %s", normalizeSeverity("unknown_value"))
	}

	// 2. extractReviewerJSON
	rawJSON := `{"findings":[]}`
	if extractReviewerJSON(rawJSON) != rawJSON {
		t.Errorf("expected raw json preserved, got %s", extractReviewerJSON(rawJSON))
	}
	fenced := "```json\n" + rawJSON + "\n```"
	if extractReviewerJSON(fenced) != rawJSON {
		t.Errorf("expected fenced json extracted, got %s", extractReviewerJSON(fenced))
	}
	embedded := "Leading text " + rawJSON + " trailing text"
	if extractReviewerJSON(embedded) != rawJSON {
		t.Errorf("expected embedded json extracted, got %s", extractReviewerJSON(embedded))
	}

	// 3. mergeFindings deduplication
	f1 := Finding{File: "a.go", Line: 10, RuleCode: "R1", Severity: "warn"}
	f2 := Finding{File: "a.go", Line: 10, RuleCode: "R1", Severity: "warn"}
	f3 := Finding{File: "b.go", Line: 20, RuleCode: "R2", Severity: "error"}
	merged := mergeFindings([]Finding{f1}, []Finding{f2, f3})
	if len(merged) != 2 {
		t.Errorf("expected 2 merged findings after deduplication, got %d", len(merged))
	}

	// 4. evaluateFindings scoring
	score, sev, passed := evaluateFindings(nil)
	if !passed || score != 1.0 || sev != SeverityInfo {
		t.Errorf("expected empty findings to pass with score 1.0, got score=%.2f, passed=%v", score, passed)
	}

	warnFindings := []Finding{
		{Severity: SeverityWarn},
		{Severity: SeverityWarn},
		{Severity: SeverityWarn},
		{Severity: SeverityWarn}, // 4 warns: 1.0 - 0.40 = 0.60 < 0.70 -> passed = false
	}
	score, _, passed = evaluateFindings(warnFindings)
	if passed || score >= 0.70 {
		t.Errorf("expected 4 warnings to fail score threshold, got score=%.2f passed=%v", score, passed)
	}

	infoFindings := []Finding{
		{Severity: SeverityInfo},
	}
	score, sev, passed = evaluateFindings(infoFindings)
	if !passed || sev != SeverityInfo || score < 0.95 {
		t.Errorf("expected info findings to pass cleanly, got score=%.2f sev=%s passed=%v", score, sev, passed)
	}

	// 5. WithReviewerModel
	cfg := reviewerConfig{}
	WithReviewerModel("custom-model")(&cfg)
	if cfg.model != "custom-model" {
		t.Errorf("expected model 'custom-model', got %s", cfg.model)
	}
}

