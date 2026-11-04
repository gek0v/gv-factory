// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// StackInfo encapsulates detected language, tooling, commands, and telemetry parsers.
type StackInfo struct {
	Language         string   `json:"language"`
	PackageManager   string   `json:"package_manager"`
	TestCommand      []string `json:"test_command"`
	FormatCommand    []string `json:"format_command"`
	SourceExtension  string   `json:"source_extension"`
	TestExtension    string   `json:"test_extension"`
	CoveragePatterns []string `json:"coverage_patterns"`
}

// Pre-compiled regex patterns for standard coverage matchers.
var (
	goCoverageRegex = regexp.MustCompile(`(?i)(?:coverage:\s*|total:\s*\(statements\)\s*)(\d+(?:\.\d+)?)%`)

	// Node/TS: handles Jest/Vitest/c8/Istanbul table formats or summary lines
	nodeCoverageTableRegex = regexp.MustCompile(`(?i)All\s+files\s*\|\s*(\d+(?:\.\d+)?)`)
	nodeCoverageLineRegex  = regexp.MustCompile(`(?i)(?:Lines|Statements)\s*:\s*(\d+(?:\.\d+)?)%`)
	nodeGenericCovRegex    = regexp.MustCompile(`(?i)coverage:\s*(\d+(?:\.\d+)?)%`)

	// Python: handles pytest-cov summary line (e.g. "TOTAL    30    3    90%")
	pythonCoverageRegex = regexp.MustCompile(`(?i)TOTAL\s+.*?\s+(\d+(?:\.\d+)?)%`)

	// Rust: handles cargo-tarpaulin / llvm-cov (e.g. "88.50% coverage") and cargo test pass/fail ratio
	rustTarpaulinRegex = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)%\s+coverage`)
	rustTestResRegex   = regexp.MustCompile(`(?i)test result:\s*ok\.\s*(\d+)\s+passed;\s*(\d+)\s+failed`)
)

// DetectStack inspects the directory structure and manifest files in the given directory
// to determine the project's programming language, package manager, and test/format commands.
func DetectStack(dir string) StackInfo {
	if dir == "" {
		return DefaultGoStack()
	}

	// 1. Go: go.mod
	if fileExists(filepath.Join(dir, "go.mod")) {
		return DetectGoStack(dir)
	}

	// 2. TypeScript / Node: package.json
	if fileExists(filepath.Join(dir, "package.json")) {
		return DetectNodeStack(dir)
	}

	// 3. Rust: Cargo.toml
	if fileExists(filepath.Join(dir, "Cargo.toml")) {
		return DetectRustStack(dir)
	}

	// 4. Python: pyproject.toml, requirements.txt, setup.py
	if fileExists(filepath.Join(dir, "pyproject.toml")) ||
		fileExists(filepath.Join(dir, "requirements.txt")) ||
		fileExists(filepath.Join(dir, "setup.py")) {
		return DetectPythonStack(dir)
	}

	// Fallback to Go as default in gv-factory
	return DefaultGoStack()
}

// DefaultGoStack returns standard Go stack configuration.
func DefaultGoStack() StackInfo {
	return StackInfo{
		Language:         "go",
		PackageManager:   "go",
		TestCommand:      []string{"go", "test", "-v", "-cover", "./..."},
		FormatCommand:    []string{"go", "fmt", "./..."},
		SourceExtension:  ".go",
		TestExtension:    "_test.go",
		CoveragePatterns: []string{`(?i)(?:coverage:\s*|total:\s*\(statements\)\s*)(\d+(?:\.\d+)?)%`},
	}
}

// DetectGoStack configures a Go stack for the given directory.
func DetectGoStack(_ string) StackInfo {
	return DefaultGoStack()
}

// DetectNodeStack inspects package.json, lockfiles, and tsconfig.json to configure Node/TypeScript.
func DetectNodeStack(dir string) StackInfo {
	pkgManager := "npm"
	testCmd := []string{"npm", "test", "--", "--coverage"}
	formatCmd := []string{"npm", "run", "format"}

	if fileExists(filepath.Join(dir, "pnpm-lock.yaml")) {
		pkgManager = "pnpm"
		testCmd = []string{"pnpm", "test", "--", "--coverage"}
		formatCmd = []string{"pnpm", "format"}
	} else if fileExists(filepath.Join(dir, "yarn.lock")) {
		pkgManager = "yarn"
		testCmd = []string{"yarn", "test", "--coverage"}
		formatCmd = []string{"yarn", "format"}
	} else if fileExists(filepath.Join(dir, "package-lock.json")) {
		pkgManager = "npm"
		testCmd = []string{"npm", "test", "--", "--coverage"}
		formatCmd = []string{"npm", "run", "format"}
	}

	// Determine if TypeScript is used
	isTS := fileExists(filepath.Join(dir, "tsconfig.json"))
	if !isTS {
		// Inspect package.json dependencies for typescript
		if data, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
			var pkg map[string]interface{}
			if err := json.Unmarshal(data, &pkg); err == nil {
				if devDeps, ok := pkg["devDependencies"].(map[string]interface{}); ok {
					if _, hasTS := devDeps["typescript"]; hasTS {
						isTS = true
					}
				}
				if deps, ok := pkg["dependencies"].(map[string]interface{}); ok {
					if _, hasTS := deps["typescript"]; hasTS {
						isTS = true
					}
				}
			}
		}
	}

	lang := "javascript"
	srcExt := ".js"
	testExt := ".test.js"
	if isTS {
		lang = "typescript"
		srcExt = ".ts"
		testExt = ".test.ts"
	}

	return StackInfo{
		Language:        lang,
		PackageManager:  pkgManager,
		TestCommand:     testCmd,
		FormatCommand:   formatCmd,
		SourceExtension: srcExt,
		TestExtension:   testExt,
		CoveragePatterns: []string{
			`(?i)All\s+files\s*\|\s*(\d+(?:\.\d+)?)`,
			`(?i)(?:Lines|Statements)\s*:\s*(\d+(?:\.\d+)?)%`,
			`(?i)coverage:\s*(\d+(?:\.\d+)?)%`,
		},
	}
}

// DetectPythonStack inspects Python manifests and configures pytest and black/ruff.
func DetectPythonStack(dir string) StackInfo {
	pkgManager := "pip"
	if fileExists(filepath.Join(dir, "poetry.lock")) {
		pkgManager = "poetry"
	} else if fileExists(filepath.Join(dir, "Pipfile")) {
		pkgManager = "pipenv"
	}

	return StackInfo{
		Language:        "python",
		PackageManager:  pkgManager,
		TestCommand:     []string{"pytest", "--cov", "."},
		FormatCommand:   []string{"black", "."},
		SourceExtension: ".py",
		TestExtension:   "test_*.py",
		CoveragePatterns: []string{
			`(?i)TOTAL\s+.*?\s+(\d+(?:\.\d+)?)%`,
			`(?i)coverage:\s*(\d+(?:\.\d+)?)%`,
		},
	}
}

// DetectRustStack configures a Rust stack using Cargo.
func DetectRustStack(_ string) StackInfo {
	return StackInfo{
		Language:        "rust",
		PackageManager:  "cargo",
		TestCommand:     []string{"cargo", "test"},
		FormatCommand:   []string{"cargo", "fmt"},
		SourceExtension: ".rs",
		TestExtension:   "_test.rs",
		CoveragePatterns: []string{
			`(?i)(\d+(?:\.\d+)?)%\s+coverage`,
			`(?i)test result:\s*ok\.\s*(\d+)\s+passed;\s*(\d+)\s+failed`,
		},
	}
}

// ParseCoverage parses command output (stdout or stderr) and extracts a test coverage ratio
// in the range [0.0, 1.0]. Returns (ratio, true) if successfully matched, or (0.0, false) otherwise.
func (s StackInfo) ParseCoverage(output string) (float64, bool) {
	if strings.TrimSpace(output) == "" {
		return 0.0, false
	}

	switch s.Language {
	case "go":
		if match := goCoverageRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}

	case "typescript", "javascript", "node":
		if match := nodeCoverageTableRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}
		if match := nodeCoverageLineRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}
		if match := nodeGenericCovRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}

	case "python":
		if match := pythonCoverageRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}
		if match := nodeGenericCovRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}

	case "rust":
		// Check tarpaulin / llvm-cov first
		if match := rustTarpaulinRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}
		// Fallback to cargo test passed/failed ratio
		if match := rustTestResRegex.FindStringSubmatch(output); len(match) > 2 {
			passed, err1 := strconv.ParseFloat(match[1], 64)
			failed, err2 := strconv.ParseFloat(match[2], 64)
			if err1 == nil && err2 == nil {
				total := passed + failed
				if total > 0 {
					return passed / total, true
				}
				return 1.0, true
			}
		}
		if match := nodeGenericCovRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}

	default:
		// Attempt fallback with generic coverage pattern
		if match := nodeGenericCovRegex.FindStringSubmatch(output); len(match) > 1 {
			if val, err := strconv.ParseFloat(match[1], 64); err == nil {
				return val / 100.0, true
			}
		}
	}

	// Try any custom patterns registered in StackInfo
	for _, pat := range s.CoveragePatterns {
		if re, err := regexp.Compile(pat); err == nil {
			if match := re.FindStringSubmatch(output); len(match) > 1 {
				if val, err := strconv.ParseFloat(match[1], 64); err == nil {
					return val / 100.0, true
				}
			}
		}
	}

	return 0.0, false
}

// ParseCoverageFloat32 is a convenience wrapper returning float32 ratio for agent compatibility.
func (s StackInfo) ParseCoverageFloat32(output string) (float32, bool) {
	val, ok := s.ParseCoverage(output)
	return float32(val), ok
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
