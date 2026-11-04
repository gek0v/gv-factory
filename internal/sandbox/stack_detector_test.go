// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

func TestDetectStack_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	info := DetectStack(tmpDir)
	if info.Language != "go" {
		t.Fatalf("expected default language 'go', got %s", info.Language)
	}
	if info.PackageManager != "go" {
		t.Fatalf("expected default package manager 'go', got %s", info.PackageManager)
	}

	// Empty string dir
	emptyInfo := DetectStack("")
	if emptyInfo.Language != "go" {
		t.Fatalf("expected 'go' for empty dir, got %s", emptyInfo.Language)
	}
}

func TestDetectStack_GoMod(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	info := DetectStack(tmpDir)
	if info.Language != "go" {
		t.Fatalf("expected language 'go', got %s", info.Language)
	}
	if info.SourceExtension != ".go" || info.TestExtension != "_test.go" {
		t.Fatalf("unexpected extensions: src=%s, test=%s", info.SourceExtension, info.TestExtension)
	}
	if len(info.TestCommand) == 0 || info.TestCommand[0] != "go" {
		t.Fatalf("unexpected test command: %v", info.TestCommand)
	}
}

func TestDetectStack_NodeVariants(t *testing.T) {
	t.Run("pnpm with tsconfig", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)
		_ = os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte(``), 0644)
		_ = os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0644)

		info := DetectStack(dir)
		if info.Language != "typescript" {
			t.Fatalf("expected 'typescript', got %s", info.Language)
		}
		if info.PackageManager != "pnpm" {
			t.Fatalf("expected 'pnpm', got %s", info.PackageManager)
		}
		if info.SourceExtension != ".ts" || info.TestExtension != ".test.ts" {
			t.Fatalf("unexpected extensions: src=%s, test=%s", info.SourceExtension, info.TestExtension)
		}
		if info.TestCommand[0] != "pnpm" {
			t.Fatalf("expected pnpm test command, got %v", info.TestCommand)
		}
	})

	t.Run("yarn with package.json devDependencies typescript", func(t *testing.T) {
		dir := t.TempDir()
		pkgContent := `{"name":"test", "devDependencies": {"typescript": "^5.0.0"}}`
		_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgContent), 0644)
		_ = os.WriteFile(filepath.Join(dir, "yarn.lock"), []byte(``), 0644)

		info := DetectStack(dir)
		if info.Language != "typescript" {
			t.Fatalf("expected 'typescript', got %s", info.Language)
		}
		if info.PackageManager != "yarn" {
			t.Fatalf("expected 'yarn', got %s", info.PackageManager)
		}
		if info.TestCommand[0] != "yarn" {
			t.Fatalf("expected yarn test command, got %v", info.TestCommand)
		}
	})

	t.Run("npm with dependencies typescript", func(t *testing.T) {
		dir := t.TempDir()
		pkgContent := `{"name":"test", "dependencies": {"typescript": "^5.0.0"}}`
		_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgContent), 0644)
		_ = os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(``), 0644)

		info := DetectStack(dir)
		if info.Language != "typescript" {
			t.Fatalf("expected 'typescript', got %s", info.Language)
		}
		if info.PackageManager != "npm" {
			t.Fatalf("expected 'npm', got %s", info.PackageManager)
		}
	})

	t.Run("plain javascript without typescript", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"plain-js"}`), 0644)

		info := DetectStack(dir)
		if info.Language != "javascript" {
			t.Fatalf("expected 'javascript', got %s", info.Language)
		}
		if info.SourceExtension != ".js" || info.TestExtension != ".test.js" {
			t.Fatalf("unexpected extensions: src=%s, test=%s", info.SourceExtension, info.TestExtension)
		}
	})
}

func TestDetectStack_PythonVariants(t *testing.T) {
	t.Run("pyproject.toml with poetry.lock", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(`[tool.poetry]`), 0644)
		_ = os.WriteFile(filepath.Join(dir, "poetry.lock"), []byte(``), 0644)

		info := DetectStack(dir)
		if info.Language != "python" {
			t.Fatalf("expected 'python', got %s", info.Language)
		}
		if info.PackageManager != "poetry" {
			t.Fatalf("expected 'poetry', got %s", info.PackageManager)
		}
		if info.SourceExtension != ".py" || info.TestExtension != "test_*.py" {
			t.Fatalf("unexpected extensions: src=%s, test=%s", info.SourceExtension, info.TestExtension)
		}
		if info.TestCommand[0] != "pytest" {
			t.Fatalf("expected pytest, got %v", info.TestCommand)
		}
	})

	t.Run("requirements.txt with Pipfile", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(`pytest==7.0.0`), 0644)
		_ = os.WriteFile(filepath.Join(dir, "Pipfile"), []byte(``), 0644)

		info := DetectStack(dir)
		if info.Language != "python" {
			t.Fatalf("expected 'python', got %s", info.Language)
		}
		if info.PackageManager != "pipenv" {
			t.Fatalf("expected 'pipenv', got %s", info.PackageManager)
		}
	})

	t.Run("setup.py default pip", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "setup.py"), []byte(`from setuptools import setup`), 0644)

		info := DetectStack(dir)
		if info.Language != "python" {
			t.Fatalf("expected 'python', got %s", info.Language)
		}
		if info.PackageManager != "pip" {
			t.Fatalf("expected 'pip', got %s", info.PackageManager)
		}
	})
}

func TestDetectStack_Rust(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]
name = "demo"
version = "0.1.0"`), 0644)

	info := DetectStack(dir)
	if info.Language != "rust" {
		t.Fatalf("expected 'rust', got %s", info.Language)
	}
	if info.PackageManager != "cargo" {
		t.Fatalf("expected 'cargo', got %s", info.PackageManager)
	}
	if info.SourceExtension != ".rs" || info.TestExtension != "_test.rs" {
		t.Fatalf("unexpected extensions: src=%s, test=%s", info.SourceExtension, info.TestExtension)
	}
	if info.TestCommand[0] != "cargo" {
		t.Fatalf("expected cargo test command, got %v", info.TestCommand)
	}
}

func TestParseCoverage(t *testing.T) {
	t.Run("Go coverage", func(t *testing.T) {
		stack := DefaultGoStack()
		out := "PASS\ncoverage: 88.5% of statements\nok  	github.com/foo/bar	0.045s"
		cov, ok := stack.ParseCoverage(out)
		if !ok || !almostEqual(cov, 0.885, 0.001) {
			t.Fatalf("expected 0.885, got %f (ok=%v)", cov, ok)
		}

		outTotal := "total: (statements) 92.4%"
		cov, ok = stack.ParseCoverage(outTotal)
		if !ok || !almostEqual(cov, 0.924, 0.001) {
			t.Fatalf("expected 0.924, got %f (ok=%v)", cov, ok)
		}
	})

	t.Run("TypeScript coverage", func(t *testing.T) {
		stack := StackInfo{Language: "typescript"}
		tableOut := `
-------------------|---------|----------|---------|---------|-------------------
File               | % Stmts | % Branch | % Funcs | % Lines | Uncovered Line #s 
-------------------|---------|----------|---------|---------|-------------------
All files          |   86.42 |       80 |     100 |   86.42 |                   
 index.ts          |   86.42 |       80 |     100 |   86.42 | 12-14             
-------------------|---------|----------|---------|---------|-------------------
`
		cov, ok := stack.ParseCoverage(tableOut)
		if !ok || !almostEqual(cov, 0.8642, 0.001) {
			t.Fatalf("expected 0.8642 from table, got %f (ok=%v)", cov, ok)
		}

		lineOut := "Statements : 91.5% ( 200/218 )\nLines : 90.0%"
		cov, ok = stack.ParseCoverage(lineOut)
		if !ok || !almostEqual(cov, 0.915, 0.001) {
			t.Fatalf("expected 0.915 from line, got %f (ok=%v)", cov, ok)
		}

		genericOut := "Overall coverage: 95.0%"
		cov, ok = stack.ParseCoverage(genericOut)
		if !ok || !almostEqual(cov, 0.95, 0.001) {
			t.Fatalf("expected 0.95 from generic, got %f (ok=%v)", cov, ok)
		}
	})

	t.Run("Python coverage", func(t *testing.T) {
		stack := StackInfo{Language: "python"}
		pytestOut := `
Name                 Stmts   Miss  Cover
----------------------------------------
app/main.py             20      2    90%
app/utils.py            10      1    90%
----------------------------------------
TOTAL                   30      3    90%
`
		cov, ok := stack.ParseCoverage(pytestOut)
		if !ok || !almostEqual(cov, 0.90, 0.001) {
			t.Fatalf("expected 0.90 from pytest-cov, got %f (ok=%v)", cov, ok)
		}
	})

	t.Run("Rust coverage", func(t *testing.T) {
		stack := StackInfo{Language: "rust"}
		tarpaulinOut := "88.50% coverage, 177/200 lines covered"
		cov, ok := stack.ParseCoverage(tarpaulinOut)
		if !ok || !almostEqual(cov, 0.885, 0.001) {
			t.Fatalf("expected 0.885 from tarpaulin, got %f (ok=%v)", cov, ok)
		}

		cargoTestOut := "test result: ok. 18 passed; 2 failed; 0 ignored; 0 measured; 0 filtered out"
		cov, ok = stack.ParseCoverage(cargoTestOut)
		if !ok || !almostEqual(cov, 0.90, 0.001) {
			t.Fatalf("expected 0.90 from cargo test pass ratio, got %f (ok=%v)", cov, ok)
		}

		cargoTestAllPass := "test result: ok. 0 passed; 0 failed; 0 ignored"
		cov, ok = stack.ParseCoverage(cargoTestAllPass)
		if !ok || !almostEqual(cov, 1.0, 0.001) {
			t.Fatalf("expected 1.0 for 0 passed / 0 failed, got %f (ok=%v)", cov, ok)
		}
	})

	t.Run("Empty and unknown", func(t *testing.T) {
		stack := StackInfo{Language: "unknown"}
		cov, ok := stack.ParseCoverage("")
		if ok || cov != 0.0 {
			t.Fatalf("expected false and 0.0 for empty string, got ok=%v, cov=%f", ok, cov)
		}

		cov, ok = stack.ParseCoverage("no coverage information here")
		if ok || cov != 0.0 {
			t.Fatalf("expected false and 0.0 for unmatching output, got ok=%v, cov=%f", ok, cov)
		}

		// ParseCoverageFloat32
		f32, ok32 := stack.ParseCoverageFloat32("coverage: 85.0%")
		if !ok32 || math.Abs(float64(f32)-0.85) > 0.001 {
			t.Fatalf("expected 0.85 from ParseCoverageFloat32, got %f (ok=%v)", f32, ok32)
		}
	})

	t.Run("Custom patterns", func(t *testing.T) {
		stack := StackInfo{
			Language:         "elixir",
			CoveragePatterns: []string{`Percentage:\s*(\d+(?:\.\d+)?)%`},
		}
		cov, ok := stack.ParseCoverage("Random log\nPercentage: 87.5%\nDone")
		if !ok || !almostEqual(cov, 0.875, 0.001) {
			t.Fatalf("expected 0.875 from custom pattern, got %f (ok=%v)", cov, ok)
		}
	})
}
