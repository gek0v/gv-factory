// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/sandbox"
)

func TestContainerSandboxConfig_DefaultsAndEnv(t *testing.T) {
	cfg := sandbox.NewDefaultContainerSandboxConfig()
	if cfg.Image != "alpine:3.20" {
		t.Errorf("expected default image alpine:3.20, got %s", cfg.Image)
	}
	if cfg.CPULimit != "1.0" {
		t.Errorf("expected default cpu 1.0, got %s", cfg.CPULimit)
	}
	if cfg.MemoryLimit != "512m" {
		t.Errorf("expected default memory 512m, got %s", cfg.MemoryLimit)
	}
	if cfg.PIDsLimit != 256 {
		t.Errorf("expected default pids 256, got %d", cfg.PIDsLimit)
	}
	if cfg.NetworkMode != "none" {
		t.Errorf("expected default network none, got %s", cfg.NetworkMode)
	}
	if len(cfg.DropCapabilities) != 1 || cfg.DropCapabilities[0] != "ALL" {
		t.Errorf("expected default cap drop [ALL], got %v", cfg.DropCapabilities)
	}
	if !cfg.NoNewPrivileges {
		t.Errorf("expected default NoNewPrivileges true")
	}
	if cfg.ReadOnlyRootfs {
		t.Errorf("expected default ReadOnlyRootfs false")
	}
	if cfg.Runtime != sandbox.RuntimeAuto {
		t.Errorf("expected default runtime auto, got %s", cfg.Runtime)
	}

	// Test environment variable loading
	t.Setenv("OV_SANDBOX_IMAGE", "custom:latest")
	t.Setenv("OV_SANDBOX_CPU_LIMIT", "3.0")
	t.Setenv("OV_SANDBOX_MEMORY_LIMIT", "2048m")
	t.Setenv("OV_SANDBOX_PIDS_LIMIT", "512")
	t.Setenv("OV_SANDBOX_NETWORK", "bridge")
	t.Setenv("OV_SANDBOX_RUNTIME", "docker")

	loaded := sandbox.LoadContainerSandboxConfigFromEnv()
	if loaded.Image != "custom:latest" {
		t.Errorf("expected image custom:latest, got %s", loaded.Image)
	}
	if loaded.CPULimit != "3.0" {
		t.Errorf("expected cpu 3.0, got %s", loaded.CPULimit)
	}
	if loaded.MemoryLimit != "2048m" {
		t.Errorf("expected memory 2048m, got %s", loaded.MemoryLimit)
	}
	if loaded.PIDsLimit != 512 {
		t.Errorf("expected pids 512, got %d", loaded.PIDsLimit)
	}
	if loaded.NetworkMode != "bridge" {
		t.Errorf("expected network bridge, got %s", loaded.NetworkMode)
	}
	if loaded.Runtime != sandbox.RuntimeDocker {
		t.Errorf("expected runtime docker, got %s", loaded.Runtime)
	}
}

func TestContainerSandboxExecutor_RuntimeDetection(t *testing.T) {
	tests := []struct {
		name          string
		cfgRuntime     string
		mockLookPath  sandbox.LookPathFunc
		expectedEngine string
		expectedMode   string
	}{
		{
			name:       "auto selects podman when available",
			cfgRuntime: sandbox.RuntimeAuto,
			mockLookPath: func(file string) (string, error) {
				if file == "podman" {
					return "/usr/bin/podman", nil
				}
				return "", errors.New("not found")
			},
			expectedEngine: "podman",
			expectedMode:   sandbox.SandboxTypeOCI,
		},
		{
			name:       "auto selects docker when podman absent but docker present",
			cfgRuntime: sandbox.RuntimeAuto,
			mockLookPath: func(file string) (string, error) {
				if file == "docker" {
					return "/usr/bin/docker", nil
				}
				return "", errors.New("not found")
			},
			expectedEngine: "docker",
			expectedMode:   sandbox.SandboxTypeOCI,
		},
		{
			name:       "auto falls back to host when neither available",
			cfgRuntime: sandbox.RuntimeAuto,
			mockLookPath: func(file string) (string, error) {
				return "", errors.New("not found")
			},
			expectedEngine: "host",
			expectedMode:   sandbox.SandboxTypeHost,
		},
		{
			name:       "explicit host runtime never calls lookPath",
			cfgRuntime: sandbox.RuntimeHost,
			mockLookPath: func(file string) (string, error) {
				t.Fatalf("lookPath should not be called when runtime is host")
				return "", nil
			},
			expectedEngine: "host",
			expectedMode:   sandbox.SandboxTypeHost,
		},
		{
			name:       "explicit podman when present",
			cfgRuntime: sandbox.RuntimePodman,
			mockLookPath: func(file string) (string, error) {
				if file == "podman" {
					return "/bin/podman", nil
				}
				return "", errors.New("not found")
			},
			expectedEngine: "podman",
			expectedMode:   sandbox.SandboxTypeOCI,
		},
		{
			name:       "explicit docker but absent falls back to host",
			cfgRuntime: sandbox.RuntimeDocker,
			mockLookPath: func(file string) (string, error) {
				return "", errors.New("docker not found")
			},
			expectedEngine: "host",
			expectedMode:   sandbox.SandboxTypeHost,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sandbox.NewDefaultContainerSandboxConfig()
			cfg.Runtime = tc.cfgRuntime

			exec := sandbox.NewContainerSandboxExecutorWithDeps(cfg, tc.mockLookPath, nil, nil)
			meta := exec.GetRuntimeMetadata()

			if meta["runtime_engine"] != tc.expectedEngine {
				t.Errorf("expected engine %s, got %v", tc.expectedEngine, meta["runtime_engine"])
			}
			if meta["sandbox_runtime"] != tc.expectedMode {
				t.Errorf("expected mode %s, got %v", tc.expectedMode, meta["sandbox_runtime"])
			}
		})
	}
}

func TestContainerSandboxExecutor_BuildArgs(t *testing.T) {
	cfg := sandbox.ContainerSandboxConfig{
		Image:            "custom-runner:v1",
		CPULimit:         "2.0",
		MemoryLimit:      "1024m",
		PIDsLimit:        128,
		NetworkMode:      "none",
		DropCapabilities: []string{"ALL", "MKNOD"},
		NoNewPrivileges:  true,
		ReadOnlyRootfs:   true,
		Runtime:          sandbox.RuntimeDocker,
	}

	var capturedArgs []string
	mockCmdBuilder := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.CommandContext(ctx, "go", "version") // safe dummy
	}

	mockLookPath := func(file string) (string, error) {
		return "/usr/bin/docker", nil
	}

	executor := sandbox.NewContainerSandboxExecutorWithDeps(cfg, mockLookPath, mockCmdBuilder, nil)

	ctx := context.Background()
	_, _, _, err := executor.ExecuteCommand(ctx, "/path/to/worktree", "npm", []string{"test", "--coverage"}, []string{"FOO=bar", "BAZ=123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")

	expectedTokens := []string{
		"run",
		"--rm",
		"--cpus=2.0",
		"--memory=1024m",
		"--pids-limit=128",
		"--network=none",
		"--cap-drop=ALL",
		"--cap-drop=MKNOD",
		"--security-opt no-new-privileges:true",
		"--read-only",
		"--tmpfs /tmp:rw,noexec,nosuid,size=64m",
		"-v",
		":/workspace:rw",
		"-w /workspace",
		"-e FOO=bar",
		"-e BAZ=123",
		"custom-runner:v1",
		"npm test --coverage",
	}

	for _, token := range expectedTokens {
		if !strings.Contains(argsStr, token) {
			t.Errorf("expected args to contain %q, but got:\n%s", token, argsStr)
		}
	}

	// Test with empty dir and no name
	emptyArgs := executor.BuildContainerArgs("", "", nil, nil)
	emptyStr := strings.Join(emptyArgs, " ")
	if !strings.Contains(emptyStr, "-w /workspace") {
		t.Errorf("expected empty dir to use workspace, got %s", emptyStr)
	}
}

func TestContainerSandboxExecutor_OCIExecutionAndExitError(t *testing.T) {
	cfg := sandbox.ContainerSandboxConfig{
		Image:   "alpine:3.20",
		Runtime: sandbox.RuntimeDocker,
	}

	mockLookPath := func(file string) (string, error) {
		return "/usr/bin/docker", nil
	}

	// Mock command builder that returns a command failing with non-zero exit code
	mockCmdBuilder := func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "go", "non-existent-command-xyz")
	}

	executor := sandbox.NewContainerSandboxExecutorWithDeps(cfg, mockLookPath, mockCmdBuilder, nil)

	var stdoutBuf, stderrBuf bytes.Buffer
	code, err := executor.ExecuteCommandStreaming(context.Background(), "/tmp", "test", nil, nil, &stdoutBuf, &stderrBuf)
	if err == nil {
		t.Fatalf("expected error from non-existent command, got nil")
	}
	if code == 0 {
		t.Fatalf("expected non-zero exit code, got %d", code)
	}
}

func TestDefaultHostCommandExecutor(t *testing.T) {
	hostExec := &sandbox.DefaultHostCommandExecutor{}
	ctx := context.Background()
	out, errOut, code, err := hostExec.ExecuteCommand(ctx, "", "go", []string{"version"}, []string{"TEST_ENV=1"})
	if err != nil || code != 0 {
		t.Fatalf("unexpected error: %v, code=%d", err, code)
	}
	if !strings.Contains(out, "go version") {
		t.Errorf("expected go version output, got: %s (stderr: %s)", out, errOut)
	}

	// Exit code path
	_, _, codeFail, errFail := hostExec.ExecuteCommand(ctx, "", "go", []string{"non-existent-subcommand-123"}, nil)
	if errFail == nil || codeFail == 0 {
		t.Errorf("expected command failure, got err: %v, code: %d", errFail, codeFail)
	}
}

func TestContainerSandboxExecutor_ExecuteCommandStreaming(t *testing.T) {
	cfg := sandbox.NewDefaultContainerSandboxConfig()
	cfg.Runtime = sandbox.RuntimeHost // use host fallback to run real local echo / test command

	execInstance := sandbox.NewContainerSandboxExecutor(cfg)

	var stdoutBuf, stderrBuf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Execute go version or echo as a cross-platform command
	exitCode, err := execInstance.ExecuteCommandStreaming(ctx, "", "go", []string{"version"}, nil, &stdoutBuf, &stderrBuf)
	if err != nil {
		t.Fatalf("ExecuteCommandStreaming failed: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(stdoutBuf.String(), "go version") {
		t.Errorf("expected stdout to contain 'go version', got: %s", stdoutBuf.String())
	}

	// Test ExecuteCommand wrapper
	stdout, stderr, code, err := execInstance.ExecuteCommand(ctx, "", "go", []string{"version"}, nil)
	if err != nil || code != 0 {
		t.Fatalf("ExecuteCommand failed: %v, code: %d", err, code)
	}
	if !strings.Contains(stdout, "go version") {
		t.Errorf("expected stdout to contain 'go version', got: %s, stderr: %s", stdout, stderr)
	}
}

func TestContainerSandboxExecutor_ContextCancellation(t *testing.T) {
	cfg := sandbox.NewDefaultContainerSandboxConfig()
	cfg.Runtime = sandbox.RuntimeHost

	execInstance := sandbox.NewContainerSandboxExecutor(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	var stdoutBuf, stderrBuf bytes.Buffer
	_, err := execInstance.ExecuteCommandStreaming(ctx, "", "go", []string{"version"}, nil, &stdoutBuf, &stderrBuf)
	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil")
	}
}

func TestContainerSandboxExecutor_Concurrency(t *testing.T) {
	cfg := sandbox.NewDefaultContainerSandboxConfig()
	cfg.Runtime = sandbox.RuntimeHost

	execInstance := sandbox.NewContainerSandboxExecutor(cfg)

	var wg sync.WaitGroup
	errCh := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var stdout, stderr bytes.Buffer
			code, err := execInstance.ExecuteCommandStreaming(context.Background(), "", "go", []string{"version"}, nil, &stdout, &stderr)
			if err != nil || code != 0 {
				errCh <- fmt.Errorf("worker %d failed: err=%v, code=%d", idx, err, code)
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

func TestContainerSandboxExecutor_NewWithNilDeps(t *testing.T) {
	cfg := sandbox.NewDefaultContainerSandboxConfig()
	cfg.Runtime = sandbox.RuntimeHost
	executor := sandbox.NewContainerSandboxExecutorWithDeps(cfg, nil, nil, nil)
	meta := executor.GetRuntimeMetadata()
	if meta["runtime_engine"] != "host" {
		t.Fatalf("expected host engine, got %v", meta["runtime_engine"])
	}
}
