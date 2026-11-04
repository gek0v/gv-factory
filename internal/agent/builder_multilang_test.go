// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/connectors/llm"
	"github.com/gv-factory/gv-factory/internal/sandbox"
)

func TestBuilder_AutoDetectStack_Go(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module myapp\n"), 0644)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir: dir,
	})

	stack := b.StackInfo()
	if stack.Language != "go" {
		t.Fatalf("expected language 'go', got %s", stack.Language)
	}
	if stack.SourceExtension != ".go" || stack.TestExtension != "_test.go" {
		t.Fatalf("unexpected extensions: src=%s, test=%s", stack.SourceExtension, stack.TestExtension)
	}
}

func TestBuilder_AutoDetectStack_TypeScript(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"tsapp"}`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte(``), 0644)
	_ = os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0644)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir: dir,
	})

	stack := b.StackInfo()
	if stack.Language != "typescript" {
		t.Fatalf("expected language 'typescript', got %s", stack.Language)
	}
	if stack.PackageManager != "pnpm" {
		t.Fatalf("expected 'pnpm', got %s", stack.PackageManager)
	}
	if stack.SourceExtension != ".ts" || stack.TestExtension != ".test.ts" {
		t.Fatalf("unexpected extensions: src=%s, test=%s", stack.SourceExtension, stack.TestExtension)
	}
	if len(b.TestCommand) == 0 || b.TestCommand[0] != "pnpm" {
		t.Fatalf("expected pnpm test command, got %v", b.TestCommand)
	}
}

func TestBuilder_AutoDetectStack_Python(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(`[tool.poetry]`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "poetry.lock"), []byte(``), 0644)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir: dir,
	})

	stack := b.StackInfo()
	if stack.Language != "python" {
		t.Fatalf("expected language 'python', got %s", stack.Language)
	}
	if stack.PackageManager != "poetry" {
		t.Fatalf("expected 'poetry', got %s", stack.PackageManager)
	}
	if stack.SourceExtension != ".py" || !strings.Contains(stack.TestExtension, ".py") {
		t.Fatalf("unexpected extensions: src=%s, test=%s", stack.SourceExtension, stack.TestExtension)
	}
	if len(b.TestCommand) == 0 || b.TestCommand[0] != "pytest" {
		t.Fatalf("expected pytest, got %v", b.TestCommand)
	}
}

func TestBuilder_AutoDetectStack_Rust(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]
name = "rustapp"
version = "0.1.0"`), 0644)

	b := NewBuilder(BuilderConfig{
		WorkspaceDir: dir,
	})

	stack := b.StackInfo()
	if stack.Language != "rust" {
		t.Fatalf("expected language 'rust', got %s", stack.Language)
	}
	if stack.PackageManager != "cargo" {
		t.Fatalf("expected 'cargo', got %s", stack.PackageManager)
	}
	if stack.SourceExtension != ".rs" || stack.TestExtension != "_test.rs" {
		t.Fatalf("unexpected extensions: src=%s, test=%s", stack.SourceExtension, stack.TestExtension)
	}
	if len(b.TestCommand) == 0 || b.TestCommand[0] != "cargo" {
		t.Fatalf("expected cargo test command, got %v", b.TestCommand)
	}
}

func TestBuilder_ExplicitStackConfiguration(t *testing.T) {
	customStack := sandbox.StackInfo{
		Language:        "kotlin",
		PackageManager:  "gradle",
		TestCommand:     []string{"./gradlew", "test"},
		FormatCommand:   []string{"./gradlew", "ktlintFormat"},
		SourceExtension: ".kt",
		TestExtension:   "Test.kt",
	}

	b := NewBuilder(BuilderConfig{
		WorkspaceDir: t.TempDir(),
		Stack:        customStack,
	})

	if b.StackInfo().Language != "kotlin" {
		t.Fatalf("expected 'kotlin', got %s", b.StackInfo().Language)
	}
	if len(b.TestCommand) != 2 || b.TestCommand[0] != "./gradlew" {
		t.Fatalf("expected gradlew test command, got %v", b.TestCommand)
	}
	if len(b.FormatCommand) != 2 || b.FormatCommand[1] != "ktlintFormat" {
		t.Fatalf("expected ktlintFormat command, got %v", b.FormatCommand)
	}
}

func TestDefaultCodeSynthesizer_MultiLangFallback(t *testing.T) {
	task := AtomicTask{ID: "task-42", Title: "Calculate Sum"}
	tc := TestCase{Name: "TestCalculateSum", ExpectedOutcome: "PASS"}
	ctx := context.Background()

	t.Run("TypeScript fallback", func(t *testing.T) {
		synth := &defaultCodeSynthesizer{
			stack: sandbox.StackInfo{
				Language:        "typescript",
				SourceExtension: ".ts",
				TestExtension:   ".test.ts",
			},
		}

		gfTest, err := synth.GenerateTest(ctx, task, tc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(gfTest.Path, ".test.ts") {
			t.Errorf("expected .test.ts suffix, got %s", gfTest.Path)
		}
		if !strings.Contains(gfTest.Content, "describe") || !strings.Contains(gfTest.Content, "expect(false).toBe(true)") {
			t.Errorf("expected failing vitest assertion, got:\n%s", gfTest.Content)
		}

		gfImpl, err := synth.GenerateImplementation(ctx, task, tc, "failing", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(gfImpl.Path, ".ts") {
			t.Errorf("expected .ts suffix, got %s", gfImpl.Path)
		}
		if !strings.Contains(gfImpl.Content, "export function") {
			t.Errorf("expected typescript export function, got:\n%s", gfImpl.Content)
		}
	})

	t.Run("Python fallback", func(t *testing.T) {
		synth := &defaultCodeSynthesizer{
			stack: sandbox.StackInfo{
				Language:        "python",
				SourceExtension: ".py",
				TestExtension:   "test_*.py",
			},
		}

		gfTest, err := synth.GenerateTest(ctx, task, tc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(gfTest.Path, "test_") || !strings.HasSuffix(gfTest.Path, ".py") {
			t.Errorf("expected test_*.py name, got %s", gfTest.Path)
		}
		if !strings.Contains(gfTest.Content, "def test_") || !strings.Contains(gfTest.Content, "assert False") {
			t.Errorf("expected pytest assertion, got:\n%s", gfTest.Content)
		}

		gfImpl, err := synth.GenerateImplementation(ctx, task, tc, "failing", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(gfImpl.Path, ".py") {
			t.Errorf("expected .py suffix, got %s", gfImpl.Path)
		}
		if !strings.Contains(gfImpl.Content, "def ") || !strings.Contains(gfImpl.Content, "return True") {
			t.Errorf("expected python function, got:\n%s", gfImpl.Content)
		}
	})

	t.Run("Rust fallback", func(t *testing.T) {
		synth := &defaultCodeSynthesizer{
			stack: sandbox.StackInfo{
				Language:        "rust",
				SourceExtension: ".rs",
				TestExtension:   "_test.rs",
			},
		}

		gfTest, err := synth.GenerateTest(ctx, task, tc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(gfTest.Path, "_test.rs") {
			t.Errorf("expected _test.rs suffix, got %s", gfTest.Path)
		}
		if !strings.Contains(gfTest.Content, "#[test]") || !strings.Contains(gfTest.Content, "panic!") {
			t.Errorf("expected rust test assertion, got:\n%s", gfTest.Content)
		}

		gfImpl, err := synth.GenerateImplementation(ctx, task, tc, "failing", 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(gfImpl.Path, ".rs") {
			t.Errorf("expected .rs suffix, got %s", gfImpl.Path)
		}
		if !strings.Contains(gfImpl.Content, "pub fn") {
			t.Errorf("expected rust pub fn, got:\n%s", gfImpl.Content)
		}
	})
}

func TestDefaultCodeSynthesizer_MultiLangLLMPrompts(t *testing.T) {
	task := AtomicTask{ID: "task-synth", Title: "Multi-Lang Auth Service"}
	tc := TestCase{Name: "TestAuthLogin", ExpectedOutcome: "PASS"}
	ctx := context.Background()

	testStacks := []struct {
		stack           sandbox.StackInfo
		expectedPrompt  []string
		expectedSysMsg  string
	}{
		{
			stack: sandbox.StackInfo{
				Language:        "typescript",
				PackageManager:  "pnpm",
				TestCommand:     []string{"pnpm", "test"},
				SourceExtension: ".ts",
				TestExtension:   ".test.ts",
			},
			expectedPrompt: []string{"typescript", "*.test.ts", "pnpm test", "package name", "REAL test assertions", "SPDX"},
			expectedSysMsg: "specialized in typescript",
		},
		{
			stack: sandbox.StackInfo{
				Language:        "python",
				PackageManager:  "poetry",
				TestCommand:     []string{"pytest", "--cov", "."},
				SourceExtension: ".py",
				TestExtension:   "test_*.py",
			},
			expectedPrompt: []string{"python", "pytest --cov .", "REAL test assertions", "SPDX"},
			expectedSysMsg: "specialized in python",
		},
		{
			stack: sandbox.StackInfo{
				Language:        "rust",
				PackageManager:  "cargo",
				TestCommand:     []string{"cargo", "test"},
				SourceExtension: ".rs",
				TestExtension:   "_test.rs",
			},
			expectedPrompt: []string{"rust", "cargo test", "REAL test assertions", "SPDX"},
			expectedSysMsg: "specialized in rust",
		},
	}

	for _, ts := range testStacks {
		t.Run(ts.stack.Language, func(t *testing.T) {
			var capturedPrompt string
			var capturedSysMsg string
			mockClient := &builderMockLLMClient{
				responseFunc: func(req *llm.ChatRequest) (*llm.ChatResponse, error) {
					if len(req.Messages) > 0 {
						capturedSysMsg = req.Messages[0].Content
					}
					if len(req.Messages) > 1 {
						capturedPrompt = req.Messages[1].Content
					}
					return &llm.ChatResponse{
						Message: llm.ChatMessage{
							Role:    llm.RoleAssistant,
							Content: `{"path": "test_output.txt", "content": "dummy"}`,
						},
					}, nil
				},
			}

			synth := &defaultCodeSynthesizer{
				llmClient: mockClient,
				stack:     ts.stack,
			}

			_, err := synth.GenerateTest(ctx, task, tc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(capturedSysMsg, ts.expectedSysMsg) {
				t.Errorf("expected sys msg to contain %q, got: %s", ts.expectedSysMsg, capturedSysMsg)
			}
			for _, expectedStr := range ts.expectedPrompt {
				if !strings.Contains(capturedPrompt, expectedStr) {
					t.Errorf("expected prompt to contain %q, got:\n%s", expectedStr, capturedPrompt)
				}
			}
		})
	}
}

type multiLangMockExecutor struct {
	runs []string
}

func (m *multiLangMockExecutor) RunCommand(ctx context.Context, dir string, cmd string, args ...string) (CommandResult, error) {
	full := cmd + " " + strings.Join(args, " ")
	m.runs = append(m.runs, full)

	// Simulate Red (failure on first run), Green (success on second), Refactor (success on format & test)
	if len(m.runs) == 1 {
		return CommandResult{
			ExitCode: 1,
			Stderr:   "FAIL: expected true, got false",
			Duration: 10 * time.Millisecond,
		}, nil
	}
	return CommandResult{
		ExitCode: 0,
		Stdout:   "PASS: all tests passed\ncoverage: 92.0% of statements",
		Duration: 10 * time.Millisecond,
	}, nil
}

func TestBuilder_ExecuteTask_MultiLangTDDLoop(t *testing.T) {
	langs := []struct {
		name      string
		stack     sandbox.StackInfo
		testCheck func(string) bool
		srcCheck  func(string) bool
	}{
		{
			name: "TypeScript",
			stack: sandbox.StackInfo{
				Language:        "typescript",
				PackageManager:  "pnpm",
				TestCommand:     []string{"pnpm", "test"},
				FormatCommand:   []string{"pnpm", "format"},
				SourceExtension: ".ts",
				TestExtension:   ".test.ts",
			},
			testCheck: func(f string) bool { return strings.HasSuffix(f, ".test.ts") },
			srcCheck:  func(f string) bool { return strings.HasSuffix(f, ".ts") && !strings.HasSuffix(f, ".test.ts") },
		},
		{
			name: "Python",
			stack: sandbox.StackInfo{
				Language:        "python",
				PackageManager:  "pip",
				TestCommand:     []string{"pytest"},
				FormatCommand:   []string{"black", "."},
				SourceExtension: ".py",
				TestExtension:   "test_*.py",
			},
			testCheck: func(f string) bool { return strings.HasPrefix(filepath.Base(f), "test_") && strings.HasSuffix(f, ".py") },
			srcCheck:  func(f string) bool { return !strings.HasPrefix(filepath.Base(f), "test_") && strings.HasSuffix(f, ".py") },
		},
		{
			name: "Rust",
			stack: sandbox.StackInfo{
				Language:        "rust",
				PackageManager:  "cargo",
				TestCommand:     []string{"cargo", "test"},
				FormatCommand:   []string{"cargo", "fmt"},
				SourceExtension: ".rs",
				TestExtension:   "_test.rs",
			},
			testCheck: func(f string) bool { return strings.HasSuffix(f, "_test.rs") },
			srcCheck:  func(f string) bool { return strings.HasSuffix(f, ".rs") && !strings.HasSuffix(f, "_test.rs") },
		},
	}

	for _, entry := range langs {
		t.Run(entry.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			executor := &multiLangMockExecutor{}

			b := NewBuilder(BuilderConfig{
				WorkspaceDir:    tmpDir,
				CommandExecutor: executor,
				Stack:           entry.stack,
				MaxIterations:   2,
			})

			res, err := b.ExecuteTask(context.Background(), AtomicTask{
				ID:          "calc-task",
				Title:       "Calculator Feature",
				Description: "Basic math operations",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !res.Success {
				t.Fatalf("expected build success, got failure: %s", res.ErrorMessage)
			}
			if len(res.Phases) != 3 {
				t.Fatalf("expected 3 phases (Red, Green, Refactor), got %d", len(res.Phases))
			}
			if res.Phases[0].Phase != PhaseRed || res.Phases[0].ExitCode != 1 {
				t.Errorf("expected Red phase exit code 1, got %v", res.Phases[0])
			}
			if res.Phases[1].Phase != PhaseGreen || res.Phases[1].ExitCode != 0 {
				t.Errorf("expected Green phase exit code 0, got %v", res.Phases[1])
			}
			if res.Phases[2].Phase != PhaseRefactor || res.Phases[2].ExitCode != 0 {
				t.Errorf("expected Refactor phase exit code 0, got %v", res.Phases[2])
			}

			// Verify generated files match expected language extensions
			var hasTestFile, hasSrcFile bool
			for _, file := range res.FilesModified {
				if entry.testCheck(file) {
					hasTestFile = true
				}
				if entry.srcCheck(file) {
					hasSrcFile = true
				}
			}
			if !hasTestFile {
				t.Errorf("expected test file matching check, files were: %v", res.FilesModified)
			}
			if !hasSrcFile {
				t.Errorf("expected source file matching check, files were: %v", res.FilesModified)
			}
		})
	}
}
