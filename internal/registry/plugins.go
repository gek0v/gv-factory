// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PluginItem represents an installed or discovered Pi plugin or extension.
type PluginItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"` // "wasm", "extension", "typescript", "native"
	Path        string `json:"path"`
	Enabled     bool   `json:"enabled"`
}

// PluginSearchPath defines a search location for plugin discovery.
type PluginSearchPath struct {
	Path   string
	Source string
}

// PluginManifestJSON models minimal manifest formats (package.json, plugin.json, extension.json).
type PluginManifestJSON struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Enabled     *bool  `json:"enabled"`
	Main        string `json:"main"`
}

// PluginsRegistryOptions configures discovery settings for PluginsRegistry.
type PluginsRegistryOptions struct {
	WorkspaceRoot   string
	IncludeUserHome bool
	ExtraPaths      []PluginSearchPath
}

// PluginsRegistry manages discovered and registered Pi plugins and extensions.
type PluginsRegistry struct {
	mu          sync.RWMutex
	searchPaths []PluginSearchPath
	plugins     map[string]PluginItem
	customItems []PluginItem
}

// NewPluginsRegistry creates a PluginsRegistry with default discovery paths in workspace and user home.
func NewPluginsRegistry(workspaceRoot string, extraPaths ...PluginSearchPath) *PluginsRegistry {
	return NewPluginsRegistryWithOptions(PluginsRegistryOptions{
		WorkspaceRoot:   workspaceRoot,
		IncludeUserHome: true,
		ExtraPaths:      extraPaths,
	})
}

// NewPluginsRegistryWithOptions creates a PluginsRegistry with customized discovery options.
func NewPluginsRegistryWithOptions(opts PluginsRegistryOptions) *PluginsRegistry {
	r := &PluginsRegistry{
		plugins:     make(map[string]PluginItem),
		searchPaths: make([]PluginSearchPath, 0),
	}

	if opts.WorkspaceRoot != "" {
		r.searchPaths = append(r.searchPaths,
			PluginSearchPath{Path: filepath.Join(opts.WorkspaceRoot, ".pi", "extensions"), Source: "workspace"},
			PluginSearchPath{Path: filepath.Join(opts.WorkspaceRoot, ".pi", "plugins"), Source: "workspace"},
		)
	}

	if opts.IncludeUserHome {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			r.searchPaths = append(r.searchPaths,
				PluginSearchPath{Path: filepath.Join(homeDir, ".pi", "extensions"), Source: "user"},
				PluginSearchPath{Path: filepath.Join(homeDir, ".pi", "plugins"), Source: "user"},
			)
		}
	}

	r.searchPaths = append(r.searchPaths, opts.ExtraPaths...)
	return r
}

// AddSearchPath appends an additional search path to the registry.
func (r *PluginsRegistry) AddSearchPath(path string, source string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searchPaths = append(r.searchPaths, PluginSearchPath{Path: path, Source: source})
}

// Register registers or updates a plugin item manually.
func (r *PluginsRegistry) Register(item PluginItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.ID == "" {
		item.ID = item.Name
	}
	r.customItems = append(r.customItems, item)
	r.plugins[item.ID] = item
}

// SetEnabled toggles the enabled state of a plugin by ID.
func (r *PluginsRegistry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.plugins[id]
	if !ok {
		return fmt.Errorf("plugin %q not found", id)
	}
	item.Enabled = enabled
	r.plugins[id] = item
	return nil
}

// Discover scans all configured search paths for extensions and manifests.
func (r *PluginsRegistry) Discover(ctx context.Context) ([]PluginItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	discovered := make(map[string]PluginItem)

	for _, sp := range r.searchPaths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		info, err := os.Stat(sp.Path)
		if err != nil || !info.IsDir() {
			continue
		}

		items, err := scanPluginsDirectory(sp.Path)
		if err != nil {
			continue
		}

		for _, item := range items {
			discovered[item.ID] = item
		}
	}

	// Apply custom registered items
	for _, item := range r.customItems {
		discovered[item.ID] = item
	}

	r.plugins = discovered

	res := make([]PluginItem, 0, len(r.plugins))
	for _, item := range r.plugins {
		res = append(res, item)
	}
	return res, nil
}

// List returns the currently indexed plugins. Performs discovery if unindexed.
func (r *PluginsRegistry) List(ctx context.Context) ([]PluginItem, error) {
	r.mu.RLock()
	count := len(r.plugins)
	r.mu.RUnlock()

	if count == 0 {
		return r.Discover(ctx)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]PluginItem, 0, len(r.plugins))
	for _, item := range r.plugins {
		res = append(res, item)
	}
	return res, nil
}

// Get retrieves a plugin by ID.
func (r *PluginsRegistry) Get(ctx context.Context, id string) (*PluginItem, error) {
	r.mu.RLock()
	item, ok := r.plugins[id]
	r.mu.RUnlock()

	if ok {
		return &item, nil
	}

	if _, err := r.Discover(ctx); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if item, ok := r.plugins[id]; ok {
		return &item, nil
	}

	return nil, fmt.Errorf("plugin %q not found", id)
}

// scanPluginsDirectory inspects a directory for plugin bundles and manifests.
func scanPluginsDirectory(dir string) ([]PluginItem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var items []PluginItem

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			item, err := parsePluginDir(entryPath, entry.Name())
			if err == nil {
				items = append(items, item)
			}
			continue
		}

		// Single-file plugins: .wasm, .ts, .js
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".wasm" || ext == ".ts" || ext == ".js" {
			id := strings.TrimSuffix(entry.Name(), ext)
			pType := "extension"
			if ext == ".wasm" {
				pType = "wasm"
			} else if ext == ".ts" || ext == ".js" {
				pType = "typescript"
			}

			items = append(items, PluginItem{
				ID:          id,
				Name:        id,
				Description: fmt.Sprintf("Pi extension %s", id),
				Type:        pType,
				Path:        filepath.Clean(entryPath),
				Enabled:     true,
			})
		}
	}

	return items, nil
}

// parsePluginDir looks for plugin.json, extension.json or package.json in an extension directory.
func parsePluginDir(dir, defaultName string) (PluginItem, error) {
	manifestCandidates := []string{"plugin.json", "extension.json", "package.json"}
	for _, candidate := range manifestCandidates {
		manifestPath := filepath.Join(dir, candidate)
		if data, err := os.ReadFile(manifestPath); err == nil {
			var mf PluginManifestJSON
			if err := json.Unmarshal(data, &mf); err == nil {
				id := mf.ID
				if id == "" {
					id = mf.Name
				}
				if id == "" {
					id = defaultName
				}

				name := mf.Name
				if name == "" {
					name = id
				}

				pType := mf.Type
				if pType == "" {
					pType = detectPluginType(dir, mf.Main)
				}

				enabled := true
				if mf.Enabled != nil {
					enabled = *mf.Enabled
				}

				return PluginItem{
					ID:          id,
					Name:        name,
					Description: mf.Description,
					Type:        pType,
					Path:        filepath.Clean(dir),
					Enabled:     enabled,
				}, nil
			}
		}
	}

	// If no manifest found, infer from directory contents
	pType := detectPluginType(dir, "")
	return PluginItem{
		ID:          defaultName,
		Name:        defaultName,
		Description: fmt.Sprintf("Extension %s", defaultName),
		Type:        pType,
		Path:        filepath.Clean(dir),
		Enabled:     true,
	}, nil
}

// detectPluginType checks file contents in a plugin directory to deduce its type.
func detectPluginType(dir, mainFile string) string {
	if mainFile != "" {
		ext := strings.ToLower(filepath.Ext(mainFile))
		if ext == ".wasm" {
			return "wasm"
		}
		if ext == ".ts" || ext == ".js" {
			return "typescript"
		}
	}

	// Check if any .wasm file exists
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if ext == ".wasm" {
					return "wasm"
				}
				if ext == ".ts" || ext == ".js" {
					return "typescript"
				}
			}
		}
	}

	return "extension"
}
