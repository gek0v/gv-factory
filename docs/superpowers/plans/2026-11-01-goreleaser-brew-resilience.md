<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# Test-Driven Implementation Plan: Resilient Homebrew Tap Publishing (Issue #54)

> **Execution Directive:** Use the Red-Green-Refactor cycle. Follow each step sequentially, confirming test failure in Red before writing configuration in Green.

**Goal:** Configure resilient Homebrew tap publishing in `.goreleaser.yaml` using `skip_upload: auto` so missing tap repositories or insufficient token scopes do not abort the CI/CD release workflow.

**Tech Stack:** Go (1.22+), GoReleaser v2 (`goreleaser` v2.5.0), YAML (`gopkg.in/yaml.v3`).

**Spec Reference:** `docs/superpowers/specs/2026-11-01-goreleaser-brew-resilience-design.md`

---

## Global Constraints

- Header requirement:
  `// SPDX-License-Identifier: Apache-2.0`
  `// SPDX-FileCopyrightText: 2026 gv-factory contributors`
- Line endings must be `LF`.
- Indentation:
  - Tabs (`\t`) for Go code (`pkg/version/goreleaser_test.go`).
  - 2 spaces for YAML (`.goreleaser.yaml`).
- Commit message: `fix(dist): make Homebrew tap publishing resilient in .goreleaser.yaml with skip_upload: auto`

---

## Execution Checklist

### Phase 1: Red (Test First)

- [ ] **Step 1: Write the failing regression test in `pkg/version/goreleaser_test.go`**

  Add `TestGoReleaserBrewResilience` to `pkg/version/goreleaser_test.go` to assert that all entries in `brews` contain `skip_upload` with value `"auto"`:

  ```go
  func TestGoReleaserBrewResilience(t *testing.T) {
  	rootPath := filepath.Join("..", "..", ".goreleaser.yaml")
  	data, err := os.ReadFile(rootPath)
  	if err != nil {
  		t.Fatalf("failed to read .goreleaser.yaml at %q: %v", rootPath, err)
  	}

  	var config map[string]any
  	if err := yaml.Unmarshal(data, &config); err != nil {
  		t.Fatalf("failed to unmarshal .goreleaser.yaml: %v", err)
  	}

  	rawBrews, ok := config["brews"]
  	if !ok {
  		t.Fatalf("missing required 'brews' section at root")
  	}

  	brews, ok := rawBrews.([]any)
  	if !ok || len(brews) == 0 {
  		t.Fatalf("'brews' must be a non-empty slice")
  	}

  	for i, b := range brews {
  		brewMap, ok := b.(map[string]any)
  		if !ok {
  			t.Fatalf("brew[%d] must be a map", i)
  		}

  		name, _ := brewMap["name"].(string)

  		skipUploadVal, hasSkipUpload := brewMap["skip_upload"]
  		if !hasSkipUpload {
  			t.Errorf("brew[%d] (%s) is missing required 'skip_upload' setting for publication resilience", i, name)
  			continue
  		}

  		skipStr, ok := skipUploadVal.(string)
  		if !ok || skipStr != "auto" {
  			t.Errorf("brew[%d] (%s) 'skip_upload' must be 'auto', got %v (%T)", i, name, skipUploadVal, skipUploadVal)
  		}
  	}
  }
  ```

- [ ] **Step 2: Execute `go test` and confirm the test FAILS (Red)**

  Run:
  ```powershell
  go test -v ./pkg/version -run TestGoReleaserBrewResilience
  ```
  Expected output:
  ```text
  --- FAIL: TestGoReleaserBrewResilience (0.00s)
      goreleaser_test.go:...: brew[0] (ov-cli) is missing required 'skip_upload' setting for publication resilience
  FAIL
  ```

---

### Phase 2: Green (Minimal Implementation)

- [ ] **Step 3: Update `.goreleaser.yaml` with `skip_upload: auto`**

  In `.goreleaser.yaml`, locate the `brews` section and add `skip_upload: auto`:

  ```yaml
  brews:
    - name: ov-cli
      ids:
        - ov-cli
      repository:
        owner: gek0v
        name: homebrew-tap
        branch: main
      directory: Formula
      skip_upload: auto
      homepage: "https://github.com/gv-factory/gv-factory"
      description: "Official developer CLI and workflow runner for OV-Factory"
      license: "Apache-2.0"
  ```

- [ ] **Step 4: Execute `go test` and confirm the test PASSES (Green)**

  Run:
  ```powershell
  go test -v ./pkg/version -run TestGoReleaserBrewResilience
  ```
  Expected output:
  ```text
  === RUN   TestGoReleaserBrewResilience
  --- PASS: TestGoReleaserBrewResilience (0.00s)
  PASS
  ok  	github.com/gv-factory/gv-factory/pkg/version	...s
  ```

- [ ] **Step 5: Run `goreleaser check` to validate GoReleaser v2 schema**

  Run:
  ```powershell
  goreleaser check
  ```
  Expected output:
  ```text
  • checking                                         path=.goreleaser.yaml
  • 1 configuration file(s) validated
  • thanks for using goreleaser!
  ```

---

### Phase 3: Refactor & Optimization

- [ ] **Step 6: Integrate dynamic GoReleaser schema check into test suite**

  In `pkg/version/goreleaser_test.go`, add a conditional subtest or helper that invokes `goreleaser check -q` if `goreleaser` exists in the system `PATH`:

  ```go
  if goreleaserBin, err := exec.LookPath("goreleaser"); err == nil {
  	cmd := exec.Command(goreleaserBin, "check", "-q", "-f", rootPath)
  	if out, err := cmd.CombinedOutput(); err != nil {
  		t.Errorf("goreleaser check failed: %v, output: %s", err, string(out))
  	}
  }
  ```

- [ ] **Step 7: Run full unit test suite with race detector**

  Run:
  ```powershell
  go test -v -race ./pkg/version/...
  ```
  Confirm all tests pass without race conditions.

---

### Phase 4: Definition of Done Verification

- [ ] **Step 8: Verify acceptance criteria**
  - [ ] `.goreleaser.yaml` has `skip_upload: auto` on `brews`.
  - [ ] `goreleaser check` succeeds with exit code 0.
  - [ ] Automated regression tests pass.
  - [ ] All new and modified files have REUSE / SPDX headers.
  - [ ] Code formatting conforms to repository standards (gofmt, LF).
