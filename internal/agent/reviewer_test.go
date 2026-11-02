// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gv-factory/gv-factory/internal/brain"
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
