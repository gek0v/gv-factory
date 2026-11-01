<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2026 gv-factory contributors
-->

# Software Design Description (SDD): Resilient Homebrew Tap Publishing in GoReleaser (Issue #54)

## 1. Executive Summary & Problem Analysis

### 1.1. Context & Motivation
In `gv-factory`, continuous delivery and binary distribution are orchestrated by the GitHub Actions workflow `Release & Supply Chain Security` (`.github/workflows/release.yml`), utilizing **GoReleaser v2** alongside **Sigstore Cosign** (keyless signing) and **Anchore Syft** (SPDX SBOM generation).

During the release run for tag `v1.0.0`, GoReleaser successfully compiled, packaged, signed, and published all 13 binary artifacts, archives, and SBOMs to GitHub Releases. However, at the final step of the release job, GoReleaser exited with fatal error code `1`:

```text
* homebrew formula: could not get default branch: GET https://api.github.com/repos/gek0v/homebrew-tap: 404 Not Found []
```

### 1.2. Root Cause Analysis
In `.goreleaser.yaml`, the `brews` configuration section specifies the formula publication for the developer CLI `ov-cli`:

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
    homepage: "https://github.com/gek0v/gv-factory"
    description: "Official developer CLI and workflow runner for OV-Factory"
    license: "Apache-2.0"
```

Because `skip_upload` was omitted, GoReleaser v2 applied the default value `skip_upload: false`. When executing the Homebrew pipe, GoReleaser unconditionally attempted to connect to the external tap repository `gek0v/homebrew-tap`. Because this repository either does not exist yet or the CI execution token (`GITHUB_TOKEN`) lacks cross-repository write access, the GitHub API responded with HTTP `404 Not Found`. This caused the entire release job to fail with exit code `1`, breaking pipeline telemetry and blocking subsequent release automation steps.

### 1.3. Scope & Objectives
1. **Resilience:** Add `skip_upload: auto` to the `brews` definition in `.goreleaser.yaml`. This ensures GoReleaser still generates the Ruby formula asset (`Formula/ov-cli.rb`) in the `dist/` directory, while omitting fatal upload attempts when conditions for pushing to an external tap are not met.
2. **Schema Compliance:** Ensure `.goreleaser.yaml` remains strictly compliant with the GoReleaser v2 JSON schema as validated by `goreleaser check`.
3. **Automated Regression Prevention:** Add unit tests to `pkg/version/goreleaser_test.go` enforcing that `skip_upload: auto` is permanently present, preventing future regressions.

---

## 2. Architecture & Data Contracts

### 2.1. Release Pipeline Flow

```text
[Tag Trigger: v*.*.*]
       │
       ▼
[GitHub Actions: release.yml]
       │
       ▼
[GoReleaser v2 Execution: release --clean]
       │
       ├─► 1. Builds (ov-server, ov-worker, ov-cli for linux/darwin/windows)
       ├─► 2. Archives (.tar.gz / .zip)
       ├─► 3. Checksums (SHA-256 checksums.txt)
       ├─► 4. SBOMs (Syft SPDX JSON)
       ├─► 5. Keyless Signatures (Cosign sign-blob)
       ├─► 6. GitHub Release Publishing (Binaries, Checksums, Signatures, SBOMs)
       │
       ▼
[Homebrew Formula Pipeline (brews)]
       │
       ├─► Generate Formula: dist/Formula/ov-cli.rb (Always generated locally)
       │
       ▼
[Evaluate skip_upload: auto]
       │
       ├─► If repository unavailable / prerelease / unauthenticated:
       │     ► Log: "skip_upload is set, skipping homebrew publish"
       │     ► Status: SUCCESS (Exit Code 0)
       │
       └─► If tap repository configured & credentials available:
             ► Push formula to gek0v/homebrew-tap:Formula/ov-cli.rb
```

### 2.2. GoReleaser Declarative Specification (`.goreleaser.yaml`)

The `brews` block in `.goreleaser.yaml` is updated as follows:

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

#### Field Specifications:
* `name`: Identifier for the Homebrew recipe (`ov-cli`).
* `ids`: Target binary builds included in the formula (`ov-cli`).
* `repository`: Target tap repository metadata (`owner: gek0v`, `name: homebrew-tap`, `branch: main`).
* `directory`: Path within the tap repository where formulas reside (`Formula`).
* `skip_upload`: Configured to `"auto"`. GoReleaser evaluates whether upload should proceed or be gracefully bypassed, generating artifacts in `dist/` without terminating the release run on external API errors.
* `homepage`: Canonical project URL (`https://github.com/gv-factory/gv-factory`).
* `description`: Formula summary string.
* `license`: Project license identifier (`Apache-2.0`).

### 2.3. Schema & Type Contract in Go

According to `github.com/goreleaser/goreleaser/v2/pkg/config`:

```go
type Homebrew struct {
	Name                  string               `yaml:"name,omitempty" json:"name,omitempty"`
	IDs                   []string             `yaml:"ids,omitempty" json:"ids,omitempty"`
	Repository            RepoRef              `yaml:"repository,omitempty" json:"repository,omitempty"`
	Directory             string               `yaml:"directory,omitempty" json:"directory,omitempty"`
	SkipUpload            string               `yaml:"skip_upload,omitempty" json:"skip_upload,omitempty" jsonschema:"oneof_type=string;boolean"`
	Homepage              string               `yaml:"homepage,omitempty" json:"homepage,omitempty"`
	Description           string               `yaml:"description,omitempty" json:"description,omitempty"`
	License               string               `yaml:"license,omitempty" json:"license,omitempty"`
	// ... additional optional metadata
}
```

The Go unmarshaler parses `skip_upload: auto` as a `string` with value `"auto"`.

---

## 3. Test Strategy & Acceptance Criteria

### 3.1. Test Strategy Overview
Testing follows the **Red-Green-Refactor** cycle implemented in `pkg/version/goreleaser_test.go`:

1. **Red Stage:** Extend `pkg/version/goreleaser_test.go` with a dedicated assertion `TestGoReleaserBrewResilience` requiring `skip_upload: "auto"`. Running this test against the unchanged `.goreleaser.yaml` produces a deterministic test failure.
2. **Green Stage:** Update `.goreleaser.yaml` to include `skip_upload: auto`. The unit test passes, and `goreleaser check` confirms 0 schema errors.
3. **Refactor Stage:** Add a subtest or dynamic execution hook that validates `.goreleaser.yaml` via `goreleaser check -q` if the binary is present in `PATH`, ensuring CI validation happens at test time as well.

### 3.2. Acceptance Criteria (DoD)
- [ ] `.goreleaser.yaml` defines `skip_upload: auto` on all entries under `brews`.
- [ ] `goreleaser check` completes with exit code `0` and no validation warnings.
- [ ] `go test -v -race ./pkg/version/...` passes with 100% success rate.
- [ ] All files maintain SPDX Apache-2.0 headers and strict LF line endings.
