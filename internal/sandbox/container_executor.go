// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Supported container runtimes and sandbox modes.
const (
	RuntimeAuto   = "auto"
	RuntimePodman = "podman"
	RuntimeDocker = "docker"
	RuntimeHost   = "host"

	SandboxTypeOCI  = "oci"
	SandboxTypeHost = "host"

	DefaultSandboxImage = "alpine:3.20"
)

// CommandExecutor defines the standard execution contract for sandbox commands.
type CommandExecutor interface {
	ExecuteCommand(ctx context.Context, dir string, name string, args []string, env []string) (stdout string, stderr string, exitCode int, err error)
}

// StreamingCommandExecutor extends CommandExecutor with real-time streaming to io.Writer sinks.
type StreamingCommandExecutor interface {
	CommandExecutor
	ExecuteCommandStreaming(
		ctx context.Context,
		dir string,
		name string,
		args []string,
		env []string,
		stdoutWriter io.Writer,
		stderrWriter io.Writer,
	) (exitCode int, err error)
}

// ContainerSandboxConfig defines resource quotas, security constraints, and container engine settings.
type ContainerSandboxConfig struct {
	Image            string   `json:"image"`
	CPULimit         string   `json:"cpu_limit"`
	MemoryLimit      string   `json:"memory_limit"`
	PIDsLimit        int      `json:"pids_limit"`
	NetworkMode      string   `json:"network_mode"`
	DropCapabilities []string `json:"drop_capabilities"`
	NoNewPrivileges  bool     `json:"no_new_privileges"`
	ReadOnlyRootfs   bool     `json:"read_only_rootfs"`
	Runtime          string   `json:"runtime"`
}

// NewDefaultContainerSandboxConfig provides safe default settings for autonomous execution.
func NewDefaultContainerSandboxConfig() ContainerSandboxConfig {
	return ContainerSandboxConfig{
		Image:            DefaultSandboxImage,
		CPULimit:         "1.0",
		MemoryLimit:      "512m",
		PIDsLimit:        256,
		NetworkMode:      "none",
		DropCapabilities: []string{"ALL"},
		NoNewPrivileges:  true,
		ReadOnlyRootfs:   false,
		Runtime:          RuntimeAuto,
	}
}

// LoadContainerSandboxConfigFromEnv loads container sandbox configuration from environment variables.
func LoadContainerSandboxConfigFromEnv() ContainerSandboxConfig {
	cfg := NewDefaultContainerSandboxConfig()
	if img := os.Getenv("OV_SANDBOX_IMAGE"); img != "" {
		cfg.Image = img
	}
	if cpu := os.Getenv("OV_SANDBOX_CPU_LIMIT"); cpu != "" {
		cfg.CPULimit = cpu
	}
	if mem := os.Getenv("OV_SANDBOX_MEMORY_LIMIT"); mem != "" {
		cfg.MemoryLimit = mem
	}
	if pids := os.Getenv("OV_SANDBOX_PIDS_LIMIT"); pids != "" {
		if p, err := strconv.Atoi(pids); err == nil && p > 0 {
			cfg.PIDsLimit = p
		}
	}
	if net := os.Getenv("OV_SANDBOX_NETWORK"); net != "" {
		cfg.NetworkMode = net
	}
	if rt := os.Getenv("OV_SANDBOX_RUNTIME"); rt != "" {
		cfg.Runtime = strings.ToLower(strings.TrimSpace(rt))
	}
	return cfg
}

// LookPathFunc abstracts binary path lookup for testability.
type LookPathFunc func(file string) (string, error)

// CommandBuilderFunc abstracts command instantiation for testability.
type CommandBuilderFunc func(ctx context.Context, name string, args ...string) *exec.Cmd

// DefaultHostCommandExecutor executes commands directly on the host OS as fallback.
type DefaultHostCommandExecutor struct{}

// ExecuteCommandStreaming runs an OS command on the host and streams stdout/stderr.
func (e *DefaultHostCommandExecutor) ExecuteCommandStreaming(
	ctx context.Context,
	dir string,
	name string,
	args []string,
	env []string,
	stdoutWriter io.Writer,
	stderrWriter io.Writer,
) (int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return exitCode, err
}

// ExecuteCommand runs an OS command on the host and returns buffered output.
func (e *DefaultHostCommandExecutor) ExecuteCommand(
	ctx context.Context,
	dir string,
	name string,
	args []string,
	env []string,
) (string, string, int, error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode, err := e.ExecuteCommandStreaming(ctx, dir, name, args, env, &stdoutBuf, &stderrBuf)
	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}

// ContainerSandboxExecutor executes tasks inside an OCI container runner with cgroups v2 quotas and mount isolation.
type ContainerSandboxExecutor struct {
	mu           sync.RWMutex
	config       ContainerSandboxConfig
	lookPath     LookPathFunc
	cmdBuilder   CommandBuilderFunc
	fallbackExec StreamingCommandExecutor
	resolvedBin  string
	resolvedMode string // "podman", "docker" or "host"
}

// NewContainerSandboxExecutor creates a new ContainerSandboxExecutor with default system dependencies.
func NewContainerSandboxExecutor(cfg ContainerSandboxConfig) *ContainerSandboxExecutor {
	return NewContainerSandboxExecutorWithDeps(cfg, exec.LookPath, exec.CommandContext, &DefaultHostCommandExecutor{})
}

// NewContainerSandboxExecutorWithDeps creates a ContainerSandboxExecutor with injected dependencies for testing.
func NewContainerSandboxExecutorWithDeps(
	cfg ContainerSandboxConfig,
	lookPath LookPathFunc,
	cmdBuilder CommandBuilderFunc,
	fallbackExec StreamingCommandExecutor,
) *ContainerSandboxExecutor {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if cmdBuilder == nil {
		cmdBuilder = exec.CommandContext
	}
	if fallbackExec == nil {
		fallbackExec = &DefaultHostCommandExecutor{}
	}

	executor := &ContainerSandboxExecutor{
		config:       cfg,
		lookPath:     lookPath,
		cmdBuilder:   cmdBuilder,
		fallbackExec: fallbackExec,
	}
	executor.resolveRuntime()
	return executor
}

func (e *ContainerSandboxExecutor) resolveRuntime() {
	e.mu.Lock()
	defer e.mu.Unlock()

	req := strings.ToLower(strings.TrimSpace(e.config.Runtime))
	if req == "" || req == RuntimeAuto {
		if path, err := e.lookPath("podman"); err == nil && path != "" {
			e.resolvedBin = path
			e.resolvedMode = RuntimePodman
			return
		}
		if path, err := e.lookPath("docker"); err == nil && path != "" {
			e.resolvedBin = path
			e.resolvedMode = RuntimeDocker
			return
		}
		log.Printf("[sandbox] No container runtime (podman/docker) available in auto mode; falling back to host execution")
		e.resolvedBin = ""
		e.resolvedMode = RuntimeHost
		return
	}

	if req == RuntimeHost {
		e.resolvedBin = ""
		e.resolvedMode = RuntimeHost
		return
	}

	if path, err := e.lookPath(req); err == nil && path != "" {
		e.resolvedBin = path
		e.resolvedMode = req
		return
	}

	log.Printf("[sandbox] Specified container runtime %q not found; falling back to host execution", req)
	e.resolvedBin = ""
	e.resolvedMode = RuntimeHost
}

// GetRuntimeMetadata returns metadata about the active execution environment for TaskResult enrichment.
func (e *ContainerSandboxExecutor) GetRuntimeMetadata() map[string]any {
	e.mu.RLock()
	defer e.mu.RUnlock()

	mode := SandboxTypeOCI
	if e.resolvedMode == RuntimeHost {
		mode = SandboxTypeHost
	}

	meta := map[string]any{
		"sandbox_runtime": mode,
		"runtime_engine":  e.resolvedMode,
	}

	if mode == SandboxTypeOCI {
		meta["image"] = e.config.Image
		meta["resource_limits"] = map[string]any{
			"cpu_limit":    e.config.CPULimit,
			"memory_limit": e.config.MemoryLimit,
			"pids_limit":   e.config.PIDsLimit,
			"network_mode": e.config.NetworkMode,
		}
	}

	return meta
}

// BuildContainerArgs constructs the OCI command-line flags and parameters for podman or docker run.
func (e *ContainerSandboxExecutor) BuildContainerArgs(dir string, name string, args []string, env []string) []string {
	var cmdArgs []string

	cmdArgs = append(cmdArgs, "run", "--rm")

	// cgroups v2 resource limits
	if e.config.CPULimit != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--cpus=%s", e.config.CPULimit))
	}
	if e.config.MemoryLimit != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--memory=%s", e.config.MemoryLimit))
	}
	if e.config.PIDsLimit > 0 {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--pids-limit=%d", e.config.PIDsLimit))
	}

	// Network isolation
	if e.config.NetworkMode != "" {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--network=%s", e.config.NetworkMode))
	}

	// Security privileges and capabilities
	for _, cap := range e.config.DropCapabilities {
		cmdArgs = append(cmdArgs, fmt.Sprintf("--cap-drop=%s", cap))
	}
	if e.config.NoNewPrivileges {
		cmdArgs = append(cmdArgs, "--security-opt", "no-new-privileges:true")
	}

	// Filesystem and mount isolation
	if e.config.ReadOnlyRootfs {
		cmdArgs = append(cmdArgs, "--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m")
	}

	// Worktree mount and working directory
	mountDir := dir
	if mountDir == "" {
		mountDir = "."
	}
	// Normalize path for OCI volume mounting
	absPath, err := filepath.Abs(mountDir)
	if err == nil {
		mountDir = absPath
	}
	normalizedMount := filepath.ToSlash(mountDir)
	cmdArgs = append(cmdArgs, "-v", fmt.Sprintf("%s:/workspace:rw", normalizedMount))
	cmdArgs = append(cmdArgs, "-w", "/workspace")

	// Environment variables
	for _, envVar := range env {
		cmdArgs = append(cmdArgs, "-e", envVar)
	}

	// Container image
	img := e.config.Image
	if img == "" {
		img = DefaultSandboxImage
	}
	cmdArgs = append(cmdArgs, img)

	// Command and parameters
	if name != "" {
		cmdArgs = append(cmdArgs, name)
	}
	cmdArgs = append(cmdArgs, args...)

	return cmdArgs
}

// ExecuteCommandStreaming executes a command inside the container (or host fallback) with real-time output streaming.
func (e *ContainerSandboxExecutor) ExecuteCommandStreaming(
	ctx context.Context,
	dir string,
	name string,
	args []string,
	env []string,
	stdoutWriter io.Writer,
	stderrWriter io.Writer,
) (int, error) {
	e.mu.RLock()
	mode := e.resolvedMode
	bin := e.resolvedBin
	e.mu.RUnlock()

	if mode == RuntimeHost || bin == "" {
		return e.fallbackExec.ExecuteCommandStreaming(ctx, dir, name, args, env, stdoutWriter, stderrWriter)
	}

	containerArgs := e.BuildContainerArgs(dir, name, args, env)
	cmd := e.cmdBuilder(ctx, bin, containerArgs...)
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	return exitCode, err
}

// ExecuteCommand runs a command and buffers standard output and standard error.
func (e *ContainerSandboxExecutor) ExecuteCommand(
	ctx context.Context,
	dir string,
	name string,
	args []string,
	env []string,
) (string, string, int, error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	exitCode, err := e.ExecuteCommandStreaming(ctx, dir, name, args, env, &stdoutBuf, &stderrBuf)
	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}
