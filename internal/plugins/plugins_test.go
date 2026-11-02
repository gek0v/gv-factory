// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
	"github.com/gv-factory/gv-factory/internal/brain"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
)

func TestPluginManifestValidation(t *testing.T) {
	tests := []struct {
		name      string
		manifest  PluginManifest
		expectErr bool
		errCode   ErrorCode
	}{
		{
			name: "valid manifest",
			manifest: PluginManifest{
				Name:        "test-plugin",
				Version:     "1.0.0",
				Entrypoint:  "run",
				Permissions: []string{"workspace:read", "log:write"},
			},
			expectErr: false,
		},
		{
			name: "missing name",
			manifest: PluginManifest{
				Name:       "",
				Version:    "1.0.0",
				Entrypoint: "run",
			},
			expectErr: true,
			errCode:   ErrCodeInvalidManifest,
		},
		{
			name: "missing version",
			manifest: PluginManifest{
				Name:       "test-plugin",
				Version:    "   ",
				Entrypoint: "run",
			},
			expectErr: true,
			errCode:   ErrCodeInvalidManifest,
		},
		{
			name: "missing entrypoint",
			manifest: PluginManifest{
				Name:       "test-plugin",
				Version:    "1.0.0",
				Entrypoint: "",
			},
			expectErr: true,
			errCode:   ErrCodeInvalidManifest,
		},
		{
			name: "empty permission item",
			manifest: PluginManifest{
				Name:        "test-plugin",
				Version:     "1.0.0",
				Entrypoint:  "run",
				Permissions: []string{"workspace:read", ""},
			},
			expectErr: true,
			errCode:   ErrCodeInvalidManifest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.manifest.Validate()
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, ErrInvalidManifest) {
					t.Errorf("expected error to wrap ErrInvalidManifest, got %v", err)
				}
				var pErr *PluginError
				if errors.As(err, &pErr) {
					if pErr.Code != tc.errCode {
						t.Errorf("expected error code %q, got %q", tc.errCode, pErr.Code)
					}
				} else {
					t.Errorf("expected *PluginError type, got %T", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestHostInitAndDefaults(t *testing.T) {
	h := NewHost()
	if h == nil {
		t.Fatal("expected non-nil host")
	}
	defer h.Close()

	if h.config.DefaultTimeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", h.config.DefaultTimeout)
	}
	if h.config.DefaultMaxPages != 1024 {
		t.Errorf("expected default max pages 1024, got %v", h.config.DefaultMaxPages)
	}
	if !h.config.EnableWasi {
		t.Errorf("expected default EnableWasi to be true")
	}

	customHost := NewHost(HostConfig{
		DefaultTimeout:  5 * time.Second,
		DefaultMaxPages: 512,
		EnableWasi:      false,
	})
	defer customHost.Close()

	if customHost.config.DefaultTimeout != 5*time.Second {
		t.Errorf("expected custom timeout 5s, got %v", customHost.config.DefaultTimeout)
	}
	if customHost.config.DefaultMaxPages != 512 {
		t.Errorf("expected custom max pages 512, got %v", customHost.config.DefaultMaxPages)
	}
	if customHost.config.EnableWasi {
		t.Errorf("expected custom EnableWasi to be false")
	}
}

func TestHostLoadPlugin(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "hello.wasm")
	manifest := PluginManifest{
		Name:       "hello-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	// 1. Load from path
	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin from path: %v", err)
	}
	if inst == nil {
		t.Fatal("expected non-nil plugin instance")
	}
	if inst.Name() != "hello-plugin" {
		t.Errorf("expected plugin name 'hello-plugin', got %q", inst.Name())
	}

	// Retrieve plugin from host registry
	found, ok := h.GetPlugin("hello-plugin")
	if !ok || found != inst {
		t.Errorf("expected to find loaded plugin in host registry")
	}

	plugins := h.ListPlugins()
	if len(plugins) != 1 || plugins[0].Name != "hello-plugin" {
		t.Errorf("expected 1 plugin listed, got %v", plugins)
	}

	// 2. Load from bytes
	wasmData, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("failed to read test wasm file: %v", err)
	}
	bytesManifest := PluginManifest{
		Name:       "hello-bytes-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}
	bytesInst, err := h.LoadPluginFromBytes(ctx, bytesManifest, wasmData)
	if err != nil {
		t.Fatalf("failed to load plugin from bytes: %v", err)
	}
	defer bytesInst.Close()

	// 3. Load invalid source (empty)
	_, err = h.LoadPlugin(ctx, PluginManifest{
		Name:       "empty-src",
		Version:    "1.0.0",
		Entrypoint: "run",
	}, PluginSource{})
	if err == nil || !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("expected ErrInvalidSource, got %v", err)
	}

	// 4. Load non-existent file
	_, err = h.LoadPluginFromPath(ctx, PluginManifest{
		Name:       "missing-src",
		Version:    "1.0.0",
		Entrypoint: "run",
	}, "testdata/does-not-exist.wasm")
	if err == nil || !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("expected ErrPluginNotFound, got %v", err)
	}

	// 5. Load with invalid manifest
	_, err = h.LoadPluginFromPath(ctx, PluginManifest{
		Name: "",
	}, wasmPath)
	if err == nil || !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("expected ErrInvalidManifest, got %v", err)
	}
}

func TestInstanceCallAndEntrypoint(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "hello.wasm")
	manifest := PluginManifest{
		Name:       "hello-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	// Check function exists
	if !inst.FunctionExists("run_test") {
		t.Errorf("expected run_test function to exist")
	}
	if inst.FunctionExists("non_existent_func") {
		t.Errorf("did not expect non_existent_func to exist")
	}

	// Call explicitly by function name
	output, err := inst.Call(ctx, "run_test", []byte{})
	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
	if string(output) != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", string(output))
	}

	// Call with empty function name (should fallback to manifest entrypoint)
	output, err = inst.Call(ctx, "", []byte{})
	if err != nil {
		t.Fatalf("unexpected call error with default entrypoint: %v", err)
	}
	if string(output) != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", string(output))
	}

	// Call non-existent function
	_, err = inst.Call(ctx, "unknown_function", []byte{})
	if err == nil || !errors.Is(err, ErrFunctionNotFound) {
		t.Fatalf("expected ErrFunctionNotFound, got %v", err)
	}
	var pErr *PluginError
	if errors.As(err, &pErr) {
		if pErr.Code != ErrCodeFunctionNotFound {
			t.Errorf("expected error code %q, got %q", ErrCodeFunctionNotFound, pErr.Code)
		}
	}
}

func TestHostCall(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "hello.wasm")
	manifest := PluginManifest{
		Name:       "hello-registered",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	_, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	// Call via registered plugin name
	out, err := h.Call(ctx, "hello-registered", "run_test", []byte{})
	if err != nil {
		t.Fatalf("failed to call by plugin name: %v", err)
	}
	if string(out) != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", string(out))
	}

	// Call via direct file path (auto-load)
	out, err = h.Call(ctx, wasmPath, "run_test", []byte{})
	if err != nil {
		t.Fatalf("failed to call by path: %v", err)
	}
	if string(out) != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", string(out))
	}

	// Call missing plugin
	_, err = h.Call(ctx, "non-existent-plugin", "run", []byte{})
	if err == nil || !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("expected ErrPluginNotFound, got %v", err)
	}
}

func TestCallPDK(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "hello.wasm")
	manifest := PluginManifest{
		Name:       "pdk-test-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	pdkCtx := pdk.Context{
		RunID:   "run-123",
		TaskID:  "task-abc",
		Configs: map[string]string{"key": "val"},
	}

	// Test CallPDK on instance
	res, err := inst.CallPDK(ctx, "run_test", pdkCtx)
	if err != nil {
		t.Fatalf("failed to execute CallPDK on instance: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success true, got %v", res.Success)
	}
	if res.Output != "Hello, world!" {
		t.Errorf("expected output 'Hello, world!', got %q", res.Output)
	}

	// Test CallPDK on host
	hostRes, err := h.CallPDK(ctx, "pdk-test-plugin", "run_test", pdkCtx)
	if err != nil {
		t.Fatalf("failed to execute CallPDK on host: %v", err)
	}
	if !hostRes.Success || hostRes.Output != "Hello, world!" {
		t.Errorf("unexpected host CallPDK result: %+v", hostRes)
	}
}

func TestExecutionTimeout(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "sleep.wasm")
	manifest := PluginManifest{
		Name:       "sleep-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
		TimeoutMS:  100, // 100 milliseconds
		Config: map[string]string{
			"duration": "2", // sleep for 2 seconds
		},
	}

	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load sleep plugin: %v", err)
	}
	defer inst.Close()

	start := time.Now()
	_, err = inst.Call(ctx, "run_test", []byte{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected execution timeout error, got nil")
	}

	if !errors.Is(err, ErrExecutionTimeout) {
		t.Errorf("expected error to wrap ErrExecutionTimeout, got: %v", err)
	}

	var pErr *PluginError
	if errors.As(err, &pErr) {
		if pErr.Code != ErrCodeExecutionTimeout {
			t.Errorf("expected error code %q, got %q", ErrCodeExecutionTimeout, pErr.Code)
		}
	} else {
		t.Errorf("expected *PluginError, got %T", err)
	}

	// Check that timeout was enforced without waiting 2 full seconds
	if elapsed > 1500*time.Millisecond {
		t.Errorf("execution took too long (%v), timeout not enforced promptly", elapsed)
	}
}

func TestAllocAndMemoryLimits(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "alloc.wasm")

	// 1. Exceeding / violating memory page limit (< 16 pages required by extism runtime)
	constrainedManifest := PluginManifest{
		Name:           "alloc-constrained",
		Version:        "1.0.0",
		Entrypoint:     "run_test",
		MemoryMaxPages: 10,
	}

	_, err := h.LoadPluginFromPath(ctx, constrainedManifest, wasmPath)
	if err == nil {
		t.Fatal("expected memory fault error when memory pages are below requirement, got nil")
	}
	if !errors.Is(err, ErrMemoryFault) {
		t.Errorf("expected error to wrap ErrMemoryFault, got: %v", err)
	}
	var pErr *PluginError
	if errors.As(err, &pErr) {
		if pErr.Code != ErrCodeMemoryFault {
			t.Errorf("expected error code %q, got %q", ErrCodeMemoryFault, pErr.Code)
		}
	} else {
		t.Errorf("expected *PluginError, got %T", err)
	}

	// 2. Normal execution with adequate memory limit (e.g. 64 pages)
	validManifest := PluginManifest{
		Name:           "alloc-valid",
		Version:        "1.0.0",
		Entrypoint:     "run_test",
		MemoryMaxPages: 64,
	}

	inst, err := h.LoadPluginFromPath(ctx, validManifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load alloc plugin with valid memory limit: %v", err)
	}
	defer inst.Close()

	_, err = inst.Call(ctx, "run_test", []byte{})
	if err != nil {
		t.Fatalf("unexpected call error: %v", err)
	}
}

func TestFaultTrapping(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "fail.wasm")
	manifest := PluginManifest{
		Name:       "fail-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load fail plugin: %v", err)
	}
	defer inst.Close()

	// Calling fail.wasm must not panic or crash the host daemon
	_, err = inst.Call(ctx, "run_test", []byte{})
	if err == nil {
		t.Fatal("expected failure error from fail.wasm, got nil")
	}

	var pErr *PluginError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected *PluginError, got %T: %v", err, err)
	}
	if pErr.Code != ErrCodeRuntime {
		t.Errorf("expected error code %q, got %q", ErrCodeRuntime, pErr.Code)
	}

	// Verify host is still intact and can execute another plugin without issues
	helloPath := filepath.Join("testdata", "hello.wasm")
	helloManifest := PluginManifest{
		Name:       "hello-after-fail",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}
	helloInst, err := h.LoadPluginFromPath(ctx, helloManifest, helloPath)
	if err != nil {
		t.Fatalf("host damaged after fault: %v", err)
	}
	defer helloInst.Close()

	out, err := helloInst.Call(ctx, "run_test", []byte{})
	if err != nil || string(out) != "Hello, world!" {
		t.Fatalf("failed to execute hello after fault: %v, output: %s", err, string(out))
	}
}

func TestResourceCleanupAndClose(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "hello.wasm")
	manifest := PluginManifest{
		Name:       "cleanup-test-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}

	// 1. Close instance
	if err := inst.Close(); err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}

	// Idempotent close
	if err := inst.Close(); err != nil {
		t.Errorf("unexpected error on second Close: %v", err)
	}

	// Calling after close
	_, err = inst.Call(ctx, "run_test", []byte{})
	if err == nil || !errors.Is(err, ErrPluginClosed) {
		t.Fatalf("expected ErrPluginClosed after close, got %v", err)
	}

	// Plugin should be removed from host
	_, ok := h.GetPlugin("cleanup-test-plugin")
	if ok {
		t.Errorf("expected plugin to be removed from host after close")
	}

	// 2. UnloadPlugin
	manifest2 := PluginManifest{
		Name:       "unload-test-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}
	_, err = h.LoadPluginFromPath(ctx, manifest2, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin 2: %v", err)
	}

	if err := h.UnloadPlugin(ctx, "unload-test-plugin"); err != nil {
		t.Fatalf("failed to unload plugin: %v", err)
	}
	if err := h.UnloadPlugin(ctx, "unload-test-plugin"); err == nil || !errors.Is(err, ErrPluginNotFound) {
		t.Errorf("expected ErrPluginNotFound on second unload, got %v", err)
	}

	// 3. Host Close closes all instances
	manifest3 := PluginManifest{
		Name:       "host-close-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}
	inst3, err := h.LoadPluginFromPath(ctx, manifest3, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin 3: %v", err)
	}

	if err := h.Close(); err != nil {
		t.Errorf("unexpected error closing host: %v", err)
	}

	// Host is closed, subsequent LoadPlugin should fail
	_, err = h.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err == nil || !errors.Is(err, ErrPluginClosed) {
		t.Errorf("expected ErrPluginClosed when loading on closed host, got %v", err)
	}

	// inst3 should now be closed
	_, err = inst3.Call(ctx, "run_test", []byte{})
	if err == nil || !errors.Is(err, ErrPluginClosed) {
		t.Errorf("expected ErrPluginClosed on inst3, got %v", err)
	}
}

func TestConcurrency(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "hello.wasm")

	// Concurrent executions across distinct plugin instances
	const numInstances = 5
	const callsPerInstance = 10
	instances := make([]*Instance, numInstances)

	for i := 0; i < numInstances; i++ {
		manifest := PluginManifest{
			Name:       filepath.Base(wasmPath) + string(rune('A'+i)),
			Version:    "1.0.0",
			Entrypoint: "run_test",
		}
		inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath)
		if err != nil {
			t.Fatalf("failed to load plugin %d: %v", i, err)
		}
		instances[i] = inst
	}

	var wg sync.WaitGroup
	errCh := make(chan error, numInstances*callsPerInstance)

	for i := 0; i < numInstances; i++ {
		inst := instances[i]
		for j := 0; j < callsPerInstance; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, err := inst.Call(ctx, "run_test", []byte{})
				if err != nil {
					errCh <- err
					return
				}
				if string(out) != "Hello, world!" {
					errCh <- errors.New("unexpected output: " + string(out))
				}
			}()
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrency error: %v", err)
	}
}

func TestPluginOptionsAndManifest(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	wasmPath := filepath.Join("testdata", "hello.wasm")
	manifest := PluginManifest{
		Name:        "opts-plugin",
		Version:     "2.1.0",
		Entrypoint:  "run_test",
		Permissions: []string{"workspace:read"},
	}

	tempDir := t.TempDir()

	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath,
		WithTimeout(15*time.Second),
		WithMaxPages(2048),
		WithConfig(map[string]string{"env": "test"}),
		WithAllowedHosts([]string{"example.com"}),
		WithAllowedPaths(map[string]string{tempDir: "/mnt"}),
	)
	if err != nil {
		t.Fatalf("failed to load plugin with options: %v", err)
	}
	defer inst.Close()

	gotManifest := inst.Manifest()
	if gotManifest.Name != "opts-plugin" || gotManifest.Version != "2.1.0" {
		t.Errorf("manifest mismatch: %+v", gotManifest)
	}
	if inst.timeout != 15*time.Second {
		t.Errorf("expected timeout 15s, got %v", inst.timeout)
	}
	if inst.maxPages != 2048 {
		t.Errorf("expected maxPages 2048, got %d", inst.maxPages)
	}
}

func TestPluginErrorFormattingAndClassification(t *testing.T) {
	innerErr := errors.New("underlying failure")

	errFull := &PluginError{
		Code:     ErrCodeTrap,
		Plugin:   "worker-plugin",
		Function: "process",
		Message:  "fatal memory trap",
		Err:      innerErr,
	}
	if errFull.Error() != `[TRAP] plugin "worker-plugin" function "process": fatal memory trap` {
		t.Errorf("unexpected full error string: %s", errFull.Error())
	}
	if errors.Unwrap(errFull) != innerErr {
		t.Errorf("expected Unwrap to return innerErr")
	}
	if !errors.Is(errFull, innerErr) {
		t.Errorf("expected Unwrap to match innerErr")
	}
	if !errors.Is(errFull, ErrTrap) {
		t.Errorf("expected Is to match ErrTrap")
	}

	errPluginOnly := &PluginError{
		Code:    ErrCodePluginNotFound,
		Plugin:  "worker-plugin",
		Message: "missing file",
	}
	if errPluginOnly.Error() != `[PLUGIN_NOT_FOUND] plugin "worker-plugin": missing file` {
		t.Errorf("unexpected plugin-only error string: %s", errPluginOnly.Error())
	}
	if !errors.Is(errPluginOnly, ErrPluginNotFound) {
		t.Errorf("expected Is to match ErrPluginNotFound")
	}

	errCodeOnly := &PluginError{
		Code:    ErrCodeClosed,
		Message: "system shutting down",
	}
	if errCodeOnly.Error() != `[PLUGIN_CLOSED] system shutting down` {
		t.Errorf("unexpected code-only error string: %s", errCodeOnly.Error())
	}
	if !errors.Is(errCodeOnly, ErrPluginClosed) {
		t.Errorf("expected Is to match ErrPluginClosed")
	}

	if errors.Is(errCodeOnly, nil) {
		t.Errorf("expected Is(nil) to return false")
	}

	// Test other error Is cases
	cases := []struct {
		err    *PluginError
		target error
	}{
		{&PluginError{Code: ErrCodeInvalidManifest}, ErrInvalidManifest},
		{&PluginError{Code: ErrCodeFunctionNotFound}, ErrFunctionNotFound},
		{&PluginError{Code: ErrCodeExecutionTimeout}, ErrExecutionTimeout},
		{&PluginError{Code: ErrCodeMemoryFault}, ErrMemoryFault},
		{&PluginError{Code: ErrCodeInvalidSource}, ErrInvalidSource},
		{&PluginError{Code: ErrCodeRuntime}, ErrPluginRuntime},
	}
	for _, c := range cases {
		if !errors.Is(c.err, c.target) {
			t.Errorf("expected %v to match %v", c.err, c.target)
		}
	}

	// Test classifyError
	ctx := context.Background()
	if classifyError("p", "f", 0, nil, ctx) != nil {
		t.Errorf("expected nil for nil error")
	}

	trapErr := classifyError("p", "f", 1, errors.New("wasm trap: unreachable executed"), ctx)
	if !errors.Is(trapErr, ErrTrap) {
		t.Errorf("expected trap classification, got %v", trapErr)
	}

	divZeroErr := classifyError("p", "f", 1, errors.New("integer divide by zero"), ctx)
	if !errors.Is(divZeroErr, ErrTrap) {
		t.Errorf("expected trap classification for div by zero, got %v", divZeroErr)
	}

	memErr := classifyError("p", "f", 1, errors.New("memory out of bounds"), ctx)
	if !errors.Is(memErr, ErrMemoryFault) {
		t.Errorf("expected memory fault classification, got %v", memErr)
	}
}

func TestHostEmptyPluginRefAndHostFunctions(t *testing.T) {
	ctx := context.Background()
	h := NewHost()
	defer h.Close()

	// Empty plugin ref
	_, err := h.Call(ctx, "", "func", []byte{})
	if err == nil || !errors.Is(err, ErrPluginNotFound) {
		t.Errorf("expected ErrPluginNotFound for empty pluginRef, got %v", err)
	}

	dummyHostFn := extism.NewHostFunctionWithStack(
		"dummy_test_fn",
		func(ctx context.Context, p *extism.CurrentPlugin, stack []uint64) {},
		[]extism.ValueType{},
		[]extism.ValueType{},
	)
	h.RegisterHostFunction(dummyHostFn)

	wasmPath := filepath.Join("testdata", "hello.wasm")
	manifest := PluginManifest{
		Name:       "dummy-hostfn-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	inst, err := h.LoadPluginFromPath(ctx, manifest, wasmPath, WithHostFunctions(dummyHostFn))
	if err != nil {
		t.Fatalf("failed to load plugin with host functions: %v", err)
	}
	defer inst.Close()
}

func TestWasmPluginEndToEndExecution(t *testing.T) {
	ctx := context.Background()

	// 1. Setup mock brain service with sample architectural rules
	mockBrain := newMockBrainService()
	mockBrain.addRule(brain.ArchitecturalRule{
		RuleCode:    "ARCH-001",
		Title:       "Microkernel Portability",
		Description: "Run agents inside isolated WASM guest environments",
		Severity:    "ERROR",
	})
	mockBrain.addRule(brain.ArchitecturalRule{
		RuleCode:    "SEC-001",
		Title:       "Secret Masking",
		Description: "Scrub all tokens in logging",
		Severity:    "CRITICAL",
	})

	// 2. Setup mock workspace with a task file
	worktreeDir := t.TempDir()
	taskFilePath := filepath.Join(worktreeDir, "task.txt")
	if err := os.WriteFile(taskFilePath, []byte("Content of isolated task file for verification"), 0644); err != nil {
		t.Fatalf("failed to create task file in worktree: %v", err)
	}

	// 3. Setup captured logs
	var capturedLogs []LogEvent
	var logMu sync.Mutex
	logSink := func(ev LogEvent) {
		logMu.Lock()
		defer logMu.Unlock()
		capturedLogs = append(capturedLogs, ev)
	}

	// 4. Configure Host and Registry
	registry := NewHostFunctionRegistry(
		WithBrainService(mockBrain),
		WithSecrets("secret-token-xyz"),
		WithLogSink(logSink),
	)
	registry.RegisterWorktree("run-e2e-verification", worktreeDir)

	host := NewHost()
	defer host.Close()
	registry.RegisterWithHost(host)

	// 5. Load the compiled e2e_plugin.wasm fixture
	wasmPath := filepath.Join("testdata", "e2e_plugin.wasm")
	manifest := PluginManifest{
		Name:       "e2e-verifier",
		Version:    "1.0.0",
		Entrypoint: "execute",
	}

	inst, err := host.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load e2e_plugin.wasm: %v", err)
	}
	defer inst.Close()

	// 6. Execute plugin passing pdk.Context
	pdkCtx := pdk.Context{
		RunID:  "run-e2e-verification",
		TaskID: "task-verification-42",
		Configs: map[string]string{
			"target_file": "task.txt",
		},
	}

	res, err := inst.CallPDK(ctx, "execute", pdkCtx)
	if err != nil {
		t.Fatalf("unexpected CallPDK execution failure: %v", err)
	}

	// 7. Verify pdk.Result output
	if !res.Success {
		t.Errorf("expected res.Success to be true, got false")
	}
	if !strings.Contains(res.Output, "Execution completed successfully for task task-verification-42") {
		t.Errorf("unexpected res.Output: %s", res.Output)
	}

	// Verify data map contents populated by host functions
	if res.Data == nil {
		t.Fatalf("expected res.Data to be populated, got nil")
	}

	rulesCount, ok := res.Data["rules_count"].(float64)
	if !ok || int(rulesCount) != 2 {
		t.Errorf("expected rules_count 2, got: %v (type %T)", res.Data["rules_count"], res.Data["rules_count"])
	}

	fileReadSuccess, ok := res.Data["file_read_success"].(bool)
	if !ok || !fileReadSuccess {
		t.Errorf("expected file_read_success to be true, got: %v", res.Data["file_read_success"])
	}

	fileContent, ok := res.Data["file_content"].(string)
	if !ok || fileContent != "Content of isolated task file for verification" {
		t.Errorf("expected file_content 'Content of isolated task file for verification', got: %q", fileContent)
	}

	// Verify log_event was called and captured
	logMu.Lock()
	defer logMu.Unlock()
	if len(capturedLogs) != 1 {
		t.Errorf("expected 1 log event captured, got %d", len(capturedLogs))
	} else {
		msg := capturedLogs[0].Message
		if !strings.Contains(msg, "task-verification-42") || !strings.Contains(msg, "run-e2e-verification") {
			t.Errorf("unexpected captured log message: %s", msg)
		}
	}
}

func TestWasmPluginFaultContainment(t *testing.T) {
	ctx := context.Background()
	registry := NewHostFunctionRegistry()
	host := NewHost()
	defer host.Close()
	registry.RegisterWithHost(host)

	wasmPath := filepath.Join("testdata", "e2e_plugin.wasm")
	manifest := PluginManifest{
		Name:       "fault-plugin",
		Version:    "1.0.0",
		Entrypoint: "execute",
	}

	inst, err := host.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load plugin: %v", err)
	}
	defer inst.Close()

	// Calling the trap_test function triggers guest panic
	_, err = inst.Call(ctx, "trap_test", []byte{})
	if err == nil {
		t.Fatal("expected error from guest panic, got nil")
	}

	var pErr *PluginError
	if !errors.As(err, &pErr) {
		t.Fatalf("expected *PluginError, got %T: %v", err, err)
	}
	if pErr.Code != ErrCodeRuntime && pErr.Code != ErrCodeTrap {
		t.Errorf("expected error code RUNTIME_ERROR or TRAP, got %q", pErr.Code)
	}

	// Verify host daemon remains completely alive and functional
	helloPath := filepath.Join("testdata", "hello.wasm")
	helloManifest := PluginManifest{
		Name:       "hello-after-trap",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}
	helloInst, err := host.LoadPluginFromPath(ctx, helloManifest, helloPath)
	if err != nil {
		t.Fatalf("failed to instantiate healthy plugin after trap: %v", err)
	}
	defer helloInst.Close()

	out, err := helloInst.Call(ctx, "run_test", []byte{})
	if err != nil || string(out) != "Hello, world!" {
		t.Fatalf("host damaged by guest trap: out=%s, err=%v", string(out), err)
	}
}

func TestWasmPluginConcurrentExecution(t *testing.T) {
	ctx := context.Background()

	mockBrain := newMockBrainService()
	mockBrain.addRule(brain.ArchitecturalRule{
		RuleCode: "CONC-001",
		Title:    "Concurrency",
	})

	registry := NewHostFunctionRegistry(WithBrainService(mockBrain))
	host := NewHost()
	defer host.Close()
	registry.RegisterWithHost(host)

	wasmPath := filepath.Join("testdata", "e2e_plugin.wasm")

	const numInstances = 4
	const callsPerInstance = 5

	type instanceWork struct {
		inst   *Instance
		runID  string
		file   string
		expect string
	}

	works := make([]instanceWork, numInstances)
	for i := 0; i < numInstances; i++ {
		runID := fmt.Sprintf("run-concurrent-%d", i)
		worktreeDir := t.TempDir()
		registry.RegisterWorktree(runID, worktreeDir)

		fileName := fmt.Sprintf("data-%d.txt", i)
		content := fmt.Sprintf("concurrent payload %d", i)
		if err := os.WriteFile(filepath.Join(worktreeDir, fileName), []byte(content), 0644); err != nil {
			t.Fatalf("failed to create file for run %d: %v", i, err)
		}

		manifest := PluginManifest{
			Name:       fmt.Sprintf("concurrent-plugin-%d", i),
			Version:    "1.0.0",
			Entrypoint: "execute",
		}
		inst, err := host.LoadPluginFromPath(ctx, manifest, wasmPath)
		if err != nil {
			t.Fatalf("failed to load instance %d: %v", i, err)
		}
		defer inst.Close()

		works[i] = instanceWork{
			inst:   inst,
			runID:  runID,
			file:   fileName,
			expect: content,
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, numInstances*callsPerInstance)

	for i := 0; i < numInstances; i++ {
		w := works[i]
		for j := 0; j < callsPerInstance; j++ {
			wg.Add(1)
			go func(iteration int) {
				defer wg.Done()
				pdkCtx := pdk.Context{
					RunID:  w.runID,
					TaskID: fmt.Sprintf("task-%d", iteration),
					Configs: map[string]string{
						"target_file": w.file,
					},
				}
				res, err := w.inst.CallPDK(ctx, "execute", pdkCtx)
				if err != nil {
					errCh <- fmt.Errorf("instance %s call failed: %w", w.runID, err)
					return
				}
				if !res.Success {
					errCh <- fmt.Errorf("instance %s success was false", w.runID)
					return
				}
				gotContent, _ := res.Data["file_content"].(string)
				if gotContent != w.expect {
					errCh <- fmt.Errorf("instance %s expected %q, got %q", w.runID, w.expect, gotContent)
				}
			}(j)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrency failure: %v", err)
	}
}

func TestEnvMockFallbackParsing(t *testing.T) {
	truthyCases := []string{"true", "True", "TRUE", "1", "t", "T", "yes", "YES", "y", "Y", "on", "ON"}
	for _, tc := range truthyCases {
		t.Setenv("OV_MOCK_PLUGINS", tc)
		t.Setenv("OV_DRY_RUN_PLUGINS", "")
		if !checkMockFallbackEnv() {
			t.Errorf("expected OV_MOCK_PLUGINS=%q to be truthy", tc)
		}

		t.Setenv("OV_MOCK_PLUGINS", "")
		t.Setenv("OV_DRY_RUN_PLUGINS", tc)
		if !checkMockFallbackEnv() {
			t.Errorf("expected OV_DRY_RUN_PLUGINS=%q to be truthy", tc)
		}
	}

	falsyCases := []string{"false", "0", "no", "off", "", "random"}
	for _, fc := range falsyCases {
		t.Setenv("OV_MOCK_PLUGINS", fc)
		t.Setenv("OV_DRY_RUN_PLUGINS", fc)
		if checkMockFallbackEnv() {
			t.Errorf("expected %q to be falsy", fc)
		}
	}
}

func TestHostConfigMockFallbackDefaults(t *testing.T) {
	t.Setenv("OV_MOCK_PLUGINS", "")
	t.Setenv("OV_DRY_RUN_PLUGINS", "")

	cfg := DefaultHostConfig()
	if cfg.EnableMockFallback {
		t.Errorf("expected default EnableMockFallback to be false when env is unset")
	}

	h := NewHost()
	defer h.Close()
	if h.MockFallbackEnabled() {
		t.Errorf("expected h.MockFallbackEnabled() to be false")
	}

	h.SetMockFallback(true)
	if !h.MockFallbackEnabled() {
		t.Errorf("expected h.MockFallbackEnabled() to be true after SetMockFallback(true)")
	}

	// Now with env var set
	t.Setenv("OV_MOCK_PLUGINS", "true")
	cfgEnv := DefaultHostConfig()
	if !cfgEnv.EnableMockFallback {
		t.Errorf("expected DefaultHostConfig().EnableMockFallback to be true when OV_MOCK_PLUGINS=true")
	}

	hEnv := NewHost()
	defer hEnv.Close()
	if !hEnv.MockFallbackEnabled() {
		t.Errorf("expected NewHost() to inherit mock fallback from env var")
	}
}

func TestHostCallMockFallback(t *testing.T) {
	ctx := context.Background()

	// 1. When mock fallback is disabled: missing plugin returns ErrPluginNotFound
	hDisabled := NewHost(HostConfig{EnableMockFallback: false})
	defer hDisabled.Close()

	_, err := hDisabled.Call(ctx, "extism://plugins/missing.wasm", "execute", []byte("{}"))
	if err == nil || !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("expected ErrPluginNotFound when mock is disabled, got %v", err)
	}

	// 2. When mock fallback is enabled: missing plugin returns synthetic pdk.Result
	hEnabled := NewHost(HostConfig{EnableMockFallback: true})
	defer hEnabled.Close()

	pluginRef := "extism://plugins/missing-step.wasm"
	fnName := "execute_step"
	outBytes, err := hEnabled.Call(ctx, pluginRef, fnName, []byte(`{"run_id":"run-1"}`))
	if err != nil {
		t.Fatalf("expected Call to succeed in mock mode, got: %v", err)
	}

	var res pdk.Result
	if err := json.Unmarshal(outBytes, &res); err != nil {
		t.Fatalf("failed to unmarshal synthetic mock result: %v", err)
	}

	if !res.Success {
		t.Errorf("expected res.Success to be true")
	}
	expectedOutput := "[dry-run] simulated execution for " + pluginRef
	if res.Output != expectedOutput {
		t.Errorf("expected Output %q, got %q", expectedOutput, res.Output)
	}
	if res.Data == nil {
		t.Fatalf("expected res.Data to be populated")
	}
	if isDryRun, ok := res.Data["dry_run"].(bool); !ok || !isDryRun {
		t.Errorf("expected Data.dry_run to be true, got %v", res.Data["dry_run"])
	}
	if p, _ := res.Data["plugin"].(string); p != pluginRef {
		t.Errorf("expected Data.plugin %q, got %q", pluginRef, p)
	}
	if f, _ := res.Data["function"].(string); f != fnName {
		t.Errorf("expected Data.function %q, got %q", fnName, f)
	}
	if s, _ := res.Data["status"].(string); s != "completed" {
		t.Errorf("expected Data.status 'completed', got %q", s)
	}

	// 3. Empty plugin ref must still fail even with mock enabled
	_, err = hEnabled.Call(ctx, "", "execute", []byte("{}"))
	if err == nil || !errors.Is(err, ErrPluginNotFound) {
		t.Errorf("expected ErrPluginNotFound for empty pluginRef even when mock is enabled, got %v", err)
	}

	// 4. Existing plugin executes real WASM, does NOT mock
	wasmPath := filepath.Join("testdata", "hello.wasm")
	realOut, err := hEnabled.Call(ctx, wasmPath, "run_test", []byte{})
	if err != nil {
		t.Fatalf("failed to call real plugin with mock enabled: %v", err)
	}
	if string(realOut) != "Hello, world!" {
		t.Errorf("expected real plugin execution output 'Hello, world!', got: %s", string(realOut))
	}
}

func TestHostCallPDKMockFallback(t *testing.T) {
	ctx := context.Background()
	h := NewHost(HostConfig{EnableMockFallback: true})
	defer h.Close()

	pluginRef := "plugins/builder-tdd.wasm"
	fnName := "build"
	pdkCtx := pdk.Context{
		RunID:  "run-mock-123",
		TaskID: "task-mock-abc",
	}

	res, err := h.CallPDK(ctx, pluginRef, fnName, pdkCtx)
	if err != nil {
		t.Fatalf("expected CallPDK to succeed in mock mode, got: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil pdk.Result")
	}
	if !res.Success {
		t.Errorf("expected res.Success to be true")
	}
	if !strings.Contains(res.Output, "[dry-run] simulated execution for plugins/builder-tdd.wasm") {
		t.Errorf("unexpected res.Output: %s", res.Output)
	}
	if isDryRun, ok := res.Data["dry_run"].(bool); !ok || !isDryRun {
		t.Errorf("expected Data.dry_run == true, got %v", res.Data["dry_run"])
	}
}

func TestHostMockFallbackConcurrency(t *testing.T) {
	ctx := context.Background()
	h := NewHost(HostConfig{EnableMockFallback: true})
	defer h.Close()

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ref := fmt.Sprintf("extism://plugins/mock-%d.wasm", idx%5)
			pdkCtx := pdk.Context{
				RunID:  fmt.Sprintf("run-%d", idx),
				TaskID: fmt.Sprintf("task-%d", idx),
			}
			res, err := h.CallPDK(ctx, ref, "execute", pdkCtx)
			if err != nil {
				errCh <- err
				return
			}
			if !res.Success {
				errCh <- fmt.Errorf("call %d returned Success=false", idx)
				return
			}
			_ = h.MockFallbackEnabled()
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent mock execution failed: %v", err)
	}
}
