// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package plugins

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	extism "github.com/extism/go-sdk"
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

