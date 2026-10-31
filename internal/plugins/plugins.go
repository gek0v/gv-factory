// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	extism "github.com/extism/go-sdk"
	pdk "github.com/gv-factory/gv-factory/plugins-sdk/go"
	"github.com/tetratelabs/wazero"
)

// PluginManifest details the metadata and capabilities of a WASM plugin node.
type PluginManifest struct {
	Name           string            `json:"name"`
	Version        string            `json:"version"`
	Entrypoint     string            `json:"entrypoint"`
	Permissions    []string          `json:"permissions,omitempty"`
	MemoryMaxPages uint32            `json:"memory_max_pages,omitempty"`
	TimeoutMS      uint64            `json:"timeout_ms,omitempty"`
	Config         map[string]string `json:"config,omitempty"`
	AllowedHosts   []string          `json:"allowed_hosts,omitempty"`
	AllowedPaths   map[string]string `json:"allowed_paths,omitempty"`
}

// Validate validates the manifest metadata and constraints.
func (m PluginManifest) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return &PluginError{
			Code:    ErrCodeInvalidManifest,
			Message: "manifest validation error: name cannot be empty",
			Err:     ErrInvalidManifest,
		}
	}
	if strings.TrimSpace(m.Version) == "" {
		return &PluginError{
			Code:    ErrCodeInvalidManifest,
			Plugin:  m.Name,
			Message: "manifest validation error: version cannot be empty",
			Err:     ErrInvalidManifest,
		}
	}
	if strings.TrimSpace(m.Entrypoint) == "" {
		return &PluginError{
			Code:    ErrCodeInvalidManifest,
			Plugin:  m.Name,
			Message: "manifest validation error: entrypoint cannot be empty",
			Err:     ErrInvalidManifest,
		}
	}
	for i, perm := range m.Permissions {
		if strings.TrimSpace(perm) == "" {
			return &PluginError{
				Code:    ErrCodeInvalidManifest,
				Plugin:  m.Name,
				Message: fmt.Sprintf("manifest validation error: permission at index %d cannot be empty", i),
				Err:     ErrInvalidManifest,
			}
		}
	}
	return nil
}

// PluginSource specifies how a WASM binary is provided: from a file path or in-memory byte buffer.
type PluginSource struct {
	Path string
	Data []byte
}

// FromPath creates a PluginSource referencing a WASM file on disk.
func FromPath(path string) PluginSource {
	return PluginSource{Path: path}
}

// FromBytes creates a PluginSource containing raw WASM bytecode.
func FromBytes(data []byte) PluginSource {
	return PluginSource{Data: data}
}

// InstanceOption configures an individual plugin instance during instantiation.
type InstanceOption func(*instanceConfig)

type instanceConfig struct {
	timeout       time.Duration
	maxPages      uint32
	hostFunctions []extism.HostFunction
	config        map[string]string
	allowedHosts  []string
	allowedPaths  map[string]string
}

// WithTimeout overrides the default execution timeout for this instance.
func WithTimeout(d time.Duration) InstanceOption {
	return func(cfg *instanceConfig) {
		cfg.timeout = d
	}
}

// WithMaxPages configures maximum linear memory pages (64KB per page) for this instance.
func WithMaxPages(pages uint32) InstanceOption {
	return func(cfg *instanceConfig) {
		cfg.maxPages = pages
	}
}

// WithHostFunctions provides host functions accessible to the plugin.
func WithHostFunctions(funcs ...extism.HostFunction) InstanceOption {
	return func(cfg *instanceConfig) {
		cfg.hostFunctions = append(cfg.hostFunctions, funcs...)
	}
}

// WithConfig provides key-value configuration pairs to the guest plugin.
func WithConfig(kv map[string]string) InstanceOption {
	return func(cfg *instanceConfig) {
		if cfg.config == nil {
			cfg.config = make(map[string]string)
		}
		for k, v := range kv {
			cfg.config[k] = v
		}
	}
}

// WithAllowedHosts configures outgoing network domains allowed for this plugin.
func WithAllowedHosts(hosts []string) InstanceOption {
	return func(cfg *instanceConfig) {
		cfg.allowedHosts = append(cfg.allowedHosts, hosts...)
	}
}

// WithAllowedPaths configures filesystem mounts accessible to the plugin.
func WithAllowedPaths(paths map[string]string) InstanceOption {
	return func(cfg *instanceConfig) {
		if cfg.allowedPaths == nil {
			cfg.allowedPaths = make(map[string]string)
		}
		for k, v := range paths {
			cfg.allowedPaths[k] = v
		}
	}
}

// HostConfig provides default configuration parameters for the plugin host.
type HostConfig struct {
	DefaultTimeout  time.Duration
	DefaultMaxPages uint32
	EnableWasi      bool
}

// DefaultHostConfig returns recommended production defaults for Extism host.
func DefaultHostConfig() HostConfig {
	return HostConfig{
		DefaultTimeout:  30 * time.Second,
		DefaultMaxPages: 1024, // 64 MB (64KB * 1024)
		EnableWasi:      true,
	}
}

// Host coordinates execution of WASM modules using the Extism runtime.
type Host struct {
	config        HostConfig
	mu            sync.RWMutex
	plugins       map[string]*Instance
	hostFunctions []extism.HostFunction
	closed        bool
}

// NewHost returns a new Extism plugin host instance with optional configuration.
func NewHost(configs ...HostConfig) *Host {
	cfg := DefaultHostConfig()
	if len(configs) > 0 {
		cfg = configs[0]
		if cfg.DefaultTimeout <= 0 {
			cfg.DefaultTimeout = 30 * time.Second
		}
		if cfg.DefaultMaxPages == 0 {
			cfg.DefaultMaxPages = 1024
		}
	}
	return &Host{
		config:        cfg,
		plugins:       make(map[string]*Instance),
		hostFunctions: make([]extism.HostFunction, 0),
	}
}

// RegisterHostFunction registers a global host function available to all plugins loaded by this host.
func (h *Host) RegisterHostFunction(fn extism.HostFunction) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hostFunctions = append(h.hostFunctions, fn)
}

// LoadPlugin validates manifest metadata and initializes an isolated Extism plugin instance.
func (h *Host) LoadPlugin(ctx context.Context, manifest PluginManifest, source PluginSource, opts ...InstanceOption) (*Instance, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	h.mu.RLock()
	if h.closed {
		h.mu.RUnlock()
		return nil, &PluginError{
			Code:    ErrCodeClosed,
			Plugin:  manifest.Name,
			Message: "plugin host is closed",
			Err:     ErrPluginClosed,
		}
	}
	h.mu.RUnlock()

	if len(source.Data) == 0 && strings.TrimSpace(source.Path) == "" {
		return nil, &PluginError{
			Code:    ErrCodeInvalidSource,
			Plugin:  manifest.Name,
			Message: "source must specify either a valid file path or non-empty byte buffer",
			Err:     ErrInvalidSource,
		}
	}

	if source.Path != "" && len(source.Data) == 0 {
		if _, err := os.Stat(source.Path); err != nil {
			return nil, &PluginError{
				Code:    ErrCodePluginNotFound,
				Plugin:  manifest.Name,
				Message: fmt.Sprintf("wasm module file not found at %q: %v", source.Path, err),
				Err:     ErrPluginNotFound,
			}
		}
	}

	instCfg := instanceConfig{
		config:       make(map[string]string),
		allowedPaths: make(map[string]string),
	}
	for k, v := range manifest.Config {
		instCfg.config[k] = v
	}
	instCfg.allowedHosts = append(instCfg.allowedHosts, manifest.AllowedHosts...)
	for k, v := range manifest.AllowedPaths {
		instCfg.allowedPaths[k] = v
	}
	for _, opt := range opts {
		opt(&instCfg)
	}

	timeout := h.config.DefaultTimeout
	if manifest.TimeoutMS > 0 {
		timeout = time.Duration(manifest.TimeoutMS) * time.Millisecond
	} else if instCfg.timeout > 0 {
		timeout = instCfg.timeout
	}

	maxPages := h.config.DefaultMaxPages
	if manifest.MemoryMaxPages > 0 {
		maxPages = manifest.MemoryMaxPages
	} else if instCfg.maxPages > 0 {
		maxPages = instCfg.maxPages
	}

	var wasmSource extism.Wasm
	if len(source.Data) > 0 {
		wasmSource = extism.WasmData{
			Data: source.Data,
			Name: manifest.Name,
		}
	} else {
		wasmSource = extism.WasmFile{
			Path: source.Path,
			Name: manifest.Name,
		}
	}

	extManifest := extism.Manifest{
		Wasm:         []extism.Wasm{wasmSource},
		Memory:       &extism.ManifestMemory{MaxPages: maxPages},
		Timeout:      uint64(timeout.Milliseconds()),
		Config:       instCfg.config,
		AllowedHosts: instCfg.allowedHosts,
		AllowedPaths: instCfg.allowedPaths,
	}

	pluginConfig := extism.PluginConfig{
		EnableWasi:    h.config.EnableWasi,
		RuntimeConfig: wazero.NewRuntimeConfig().WithCloseOnContextDone(true),
		ModuleConfig:  wazero.NewModuleConfig().WithSysWalltime().WithSysNanotime(),
	}

	h.mu.RLock()
	allHostFunctions := make([]extism.HostFunction, 0, len(h.hostFunctions)+len(instCfg.hostFunctions))
	allHostFunctions = append(allHostFunctions, h.hostFunctions...)
	allHostFunctions = append(allHostFunctions, instCfg.hostFunctions...)
	h.mu.RUnlock()

	plugin, err := extism.NewPlugin(ctx, extManifest, pluginConfig, allHostFunctions)
	if err != nil {
		return nil, classifyError(manifest.Name, "", 0, err, ctx)
	}

	inst := &Instance{
		manifest: manifest,
		plugin:   plugin,
		host:     h,
		timeout:  timeout,
		maxPages: maxPages,
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = plugin.CloseWithContext(ctx)
		return nil, &PluginError{
			Code:    ErrCodeClosed,
			Plugin:  manifest.Name,
			Message: "plugin host was closed while loading plugin",
			Err:     ErrPluginClosed,
		}
	}
	h.plugins[manifest.Name] = inst
	h.mu.Unlock()

	return inst, nil
}

// LoadPluginFromPath loads a plugin from a filesystem path.
func (h *Host) LoadPluginFromPath(ctx context.Context, manifest PluginManifest, path string, opts ...InstanceOption) (*Instance, error) {
	return h.LoadPlugin(ctx, manifest, FromPath(path), opts...)
}

// LoadPluginFromBytes loads a plugin from an in-memory byte slice.
func (h *Host) LoadPluginFromBytes(ctx context.Context, manifest PluginManifest, data []byte, opts ...InstanceOption) (*Instance, error) {
	return h.LoadPlugin(ctx, manifest, FromBytes(data), opts...)
}

// GetPlugin retrieves a previously loaded plugin by name.
func (h *Host) GetPlugin(name string) (*Instance, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	inst, ok := h.plugins[name]
	return inst, ok
}

// ListPlugins returns a snapshot of manifests for all currently loaded plugins.
func (h *Host) ListPlugins() []PluginManifest {
	h.mu.RLock()
	defer h.mu.RUnlock()
	list := make([]PluginManifest, 0, len(h.plugins))
	for _, inst := range h.plugins {
		list = append(list, inst.manifest)
	}
	return list
}

// UnloadPlugin closes and unregisters a loaded plugin instance.
func (h *Host) UnloadPlugin(ctx context.Context, name string) error {
	h.mu.Lock()
	inst, ok := h.plugins[name]
	if !ok {
		h.mu.Unlock()
		return &PluginError{
			Code:    ErrCodePluginNotFound,
			Plugin:  name,
			Message: fmt.Sprintf("plugin %q not found", name),
			Err:     ErrPluginNotFound,
		}
	}
	delete(h.plugins, name)
	h.mu.Unlock()

	return inst.CloseWithContext(ctx)
}

func (h *Host) removePlugin(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.plugins, name)
}

// Call executes a target function on a WASM plugin instance with provided inputs.
// pluginRef can be a loaded plugin name or a path to a .wasm file.
func (h *Host) Call(ctx context.Context, pluginRef string, functionName string, input []byte) ([]byte, error) {
	if strings.TrimSpace(pluginRef) == "" {
		return nil, &PluginError{
			Code:    ErrCodePluginNotFound,
			Message: "plugin reference cannot be empty",
			Err:     ErrPluginNotFound,
		}
	}

	inst, ok := h.GetPlugin(pluginRef)
	if !ok {
		if _, statErr := os.Stat(pluginRef); statErr == nil {
			entrypoint := functionName
			if entrypoint == "" {
				entrypoint = "run_test"
			}
			manifest := PluginManifest{
				Name:       filepath.Base(pluginRef),
				Version:    "1.0.0",
				Entrypoint: entrypoint,
			}
			var loadErr error
			inst, loadErr = h.LoadPluginFromPath(ctx, manifest, pluginRef)
			if loadErr != nil {
				return nil, loadErr
			}
		} else {
			return nil, &PluginError{
				Code:    ErrCodePluginNotFound,
				Plugin:  pluginRef,
				Message: fmt.Sprintf("plugin %q not found", pluginRef),
				Err:     ErrPluginNotFound,
			}
		}
	}

	return inst.Call(ctx, functionName, input)
}

// CallPDK executes a target function passing a pdk.Context and returning a pdk.Result.
func (h *Host) CallPDK(ctx context.Context, pluginRef string, functionName string, req pdk.Context) (*pdk.Result, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pdk.Context: %w", err)
	}

	outBytes, err := h.Call(ctx, pluginRef, functionName, reqBytes)
	if err != nil {
		return nil, err
	}

	var res pdk.Result
	if err := json.Unmarshal(outBytes, &res); err != nil {
		return &pdk.Result{
			Success: true,
			Output:  string(outBytes),
		}, nil
	}

	return &res, nil
}

// Close closes all loaded plugin instances and releases runtime resources.
func (h *Host) Close() error {
	return h.CloseWithContext(context.Background())
}

// CloseWithContext closes all loaded plugin instances using the provided context.
func (h *Host) CloseWithContext(ctx context.Context) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true

	instances := make([]*Instance, 0, len(h.plugins))
	for _, inst := range h.plugins {
		instances = append(instances, inst)
	}
	h.plugins = make(map[string]*Instance)
	h.mu.Unlock()

	var firstErr error
	for _, inst := range instances {
		if err := inst.CloseWithContext(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// Instance represents an instantiated, isolated WASM plugin runtime node.
type Instance struct {
	manifest PluginManifest
	plugin   *extism.Plugin
	host     *Host
	timeout  time.Duration
	maxPages uint32
	mu       sync.Mutex
	closed   bool
}

// Manifest returns the configuration manifest of the plugin instance.
func (inst *Instance) Manifest() PluginManifest {
	return inst.manifest
}

// Name returns the identifier of the plugin instance.
func (inst *Instance) Name() string {
	return inst.manifest.Name
}

// FunctionExists checks if an exported function exists in the WASM plugin module.
func (inst *Instance) FunctionExists(name string) bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.closed || inst.plugin == nil {
		return false
	}
	return inst.plugin.FunctionExists(name)
}

// Call executes an exported function in the plugin with the given input payload.
func (inst *Instance) Call(ctx context.Context, functionName string, input []byte) (output []byte, retErr error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.closed || inst.plugin == nil {
		return nil, &PluginError{
			Code:    ErrCodeClosed,
			Plugin:  inst.manifest.Name,
			Message: "plugin instance is closed",
			Err:     ErrPluginClosed,
		}
	}

	targetFunc := strings.TrimSpace(functionName)
	if targetFunc == "" {
		targetFunc = inst.manifest.Entrypoint
	}

	if !inst.plugin.FunctionExists(targetFunc) {
		return nil, &PluginError{
			Code:     ErrCodeFunctionNotFound,
			Plugin:   inst.manifest.Name,
			Function: targetFunc,
			Message:  fmt.Sprintf("exported function %q not found in plugin %q", targetFunc, inst.manifest.Name),
			Err:      ErrFunctionNotFound,
		}
	}

	execCtx := ctx
	if inst.timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, inst.timeout)
		defer cancel()
	}

	defer func() {
		if r := recover(); r != nil {
			retErr = &PluginError{
				Code:     ErrCodeTrap,
				Plugin:   inst.manifest.Name,
				Function: targetFunc,
				Message:  fmt.Sprintf("recovered from panic in wasm execution: %v", r),
				Err:      ErrTrap,
			}
		}
	}()

	exitCode, out, err := inst.plugin.CallWithContext(execCtx, targetFunc, input)
	if err != nil {
		return nil, classifyError(inst.manifest.Name, targetFunc, exitCode, err, execCtx)
	}

	if exitCode != 0 {
		return nil, &PluginError{
			Code:     ErrCodeRuntime,
			Plugin:   inst.manifest.Name,
			Function: targetFunc,
			Message:  fmt.Sprintf("plugin exited with code %d: %s", exitCode, string(out)),
			Err:      ErrPluginRuntime,
		}
	}

	return out, nil
}

// CallPDK executes an exported function passing a pdk.Context and returning a pdk.Result.
func (inst *Instance) CallPDK(ctx context.Context, functionName string, req pdk.Context) (*pdk.Result, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pdk.Context: %w", err)
	}

	outBytes, err := inst.Call(ctx, functionName, reqBytes)
	if err != nil {
		return nil, err
	}

	var res pdk.Result
	if err := json.Unmarshal(outBytes, &res); err != nil {
		return &pdk.Result{
			Success: true,
			Output:  string(outBytes),
		}, nil
	}

	return &res, nil
}

// Close releases all native resources and memory associated with the WASM instance.
func (inst *Instance) Close() error {
	return inst.CloseWithContext(context.Background())
}

// CloseWithContext releases all resources with the specified context.
func (inst *Instance) CloseWithContext(ctx context.Context) error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.closed {
		return nil
	}
	inst.closed = true

	var err error
	if inst.plugin != nil {
		err = inst.plugin.CloseWithContext(ctx)
		inst.plugin = nil
	}

	if inst.host != nil {
		inst.host.removePlugin(inst.manifest.Name)
	}

	return err
}

