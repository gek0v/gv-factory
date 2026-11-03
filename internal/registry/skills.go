// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package registry

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// SkillItem represents a discovered or registered skill in the Pi ecosystem.
type SkillItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
	Source      string `json:"source"` // "workspace", "user", "custom", "builtin"
}

// SkillFrontmatter represents optional YAML frontmatter at the top of a SKILL.md.
type SkillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// SkillSearchPath specifies a search directory and its associated source classification.
type SkillSearchPath struct {
	Path   string
	Source string
}

// SkillsRegistryOptions configures discovery settings for SkillsRegistry.
type SkillsRegistryOptions struct {
	WorkspaceRoot   string
	IncludeUserHome bool
	ExtraPaths      []SkillSearchPath
}

// SkillsRegistry manages the discovery, indexing and lookup of skills.
type SkillsRegistry struct {
	mu          sync.RWMutex
	searchPaths []SkillSearchPath
	skills      map[string]SkillItem
	customItems []SkillItem
}

// NewSkillsRegistry creates a new SkillsRegistry with default discovery paths (including user home).
func NewSkillsRegistry(workspaceRoot string, extraPaths ...SkillSearchPath) *SkillsRegistry {
	return NewSkillsRegistryWithOptions(SkillsRegistryOptions{
		WorkspaceRoot:   workspaceRoot,
		IncludeUserHome: true,
		ExtraPaths:      extraPaths,
	})
}

// NewSkillsRegistryWithOptions creates a SkillsRegistry with customized discovery options.
func NewSkillsRegistryWithOptions(opts SkillsRegistryOptions) *SkillsRegistry {
	r := &SkillsRegistry{
		skills:      make(map[string]SkillItem),
		searchPaths: make([]SkillSearchPath, 0),
	}

	if opts.WorkspaceRoot != "" {
		r.searchPaths = append(r.searchPaths,
			SkillSearchPath{Path: filepath.Join(opts.WorkspaceRoot, ".agents", "skills"), Source: "workspace"},
			SkillSearchPath{Path: filepath.Join(opts.WorkspaceRoot, ".pi", "skills"), Source: "workspace"},
			SkillSearchPath{Path: filepath.Join(opts.WorkspaceRoot, ".pi", "agent", "skills"), Source: "workspace"},
		)
	}

	if opts.IncludeUserHome {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			r.searchPaths = append(r.searchPaths,
				SkillSearchPath{Path: filepath.Join(homeDir, ".agents", "skills"), Source: "user"},
				SkillSearchPath{Path: filepath.Join(homeDir, ".pi", "skills"), Source: "user"},
				SkillSearchPath{Path: filepath.Join(homeDir, ".pi", "agent", "skills"), Source: "user"},
			)
		}
	}

	r.searchPaths = append(r.searchPaths, opts.ExtraPaths...)
	return r
}

// AddSearchPath appends an additional search path to the registry.
func (r *SkillsRegistry) AddSearchPath(path string, source string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.searchPaths = append(r.searchPaths, SkillSearchPath{Path: path, Source: source})
}

// Register registers a custom skill item into the registry.
func (r *SkillsRegistry) Register(item SkillItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if item.Source == "" {
		item.Source = "custom"
	}
	r.customItems = append(r.customItems, item)
	r.skills[item.Name] = item
}

// Discover scans all configured search paths and indexes discovered skills.
// Workspace skills override user skills with the same name. Custom skills take highest precedence.
func (r *SkillsRegistry) Discover(ctx context.Context) ([]SkillItem, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	discovered := make(map[string]SkillItem)

	for _, sp := range r.searchPaths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		info, err := os.Stat(sp.Path)
		if err != nil || !info.IsDir() {
			continue
		}

		items, err := scanSkillsDirectory(sp.Path, sp.Source)
		if err != nil {
			continue
		}

		for _, item := range items {
			existing, exists := discovered[item.Name]
			if !exists {
				discovered[item.Name] = item
				continue
			}

			// Precedence: custom > workspace > user
			if existing.Source == "user" && item.Source == "workspace" {
				discovered[item.Name] = item
			}
		}
	}

	// Apply custom registered items (highest priority)
	for _, item := range r.customItems {
		discovered[item.Name] = item
	}

	r.skills = discovered

	res := make([]SkillItem, 0, len(r.skills))
	for _, item := range r.skills {
		res = append(res, item)
	}
	return res, nil
}

// List returns the currently indexed skills. If unindexed, it performs an initial discovery.
func (r *SkillsRegistry) List(ctx context.Context) ([]SkillItem, error) {
	r.mu.RLock()
	count := len(r.skills)
	r.mu.RUnlock()

	if count == 0 {
		return r.Discover(ctx)
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]SkillItem, 0, len(r.skills))
	for _, item := range r.skills {
		res = append(res, item)
	}
	return res, nil
}

// Get finds a skill by its name.
func (r *SkillsRegistry) Get(ctx context.Context, name string) (*SkillItem, error) {
	r.mu.RLock()
	item, ok := r.skills[name]
	r.mu.RUnlock()

	if ok {
		return &item, nil
	}

	// Trigger discovery if map is empty
	if _, err := r.Discover(ctx); err != nil {
		return nil, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if item, ok := r.skills[name]; ok {
		return &item, nil
	}

	return nil, fmt.Errorf("skill %q not found", name)
}

// scanSkillsDirectory inspects a directory looking for skill bundles or markdown files.
func scanSkillsDirectory(dir string, source string) ([]SkillItem, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var items []SkillItem

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			// Subdirectory format: dir/SKILL.md or dir/skill.md
			candidate := filepath.Join(entryPath, "SKILL.md")
			if _, err := os.Stat(candidate); err != nil {
				candidate = filepath.Join(entryPath, "skill.md")
				if _, err := os.Stat(candidate); err != nil {
					continue
				}
			}

			item, err := parseSkillFile(candidate, entry.Name(), source)
			if err == nil {
				items = append(items, item)
			}
			continue
		}

		// Standalone markdown file: dir/<name>.md
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			item, err := parseSkillFile(entryPath, name, source)
			if err == nil {
				items = append(items, item)
			}
		}
	}

	return items, nil
}

// parseSkillFile reads and extracts metadata from a skill markdown file.
func parseSkillFile(filePath, defaultName, source string) (SkillItem, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return SkillItem{}, err
	}

	name := defaultName
	desc := ""

	// Check for YAML frontmatter between "---" markers
	trimmed := bytes.TrimSpace(content)
	if bytes.HasPrefix(trimmed, []byte("---")) {
		parts := bytes.SplitN(trimmed, []byte("---"), 3)
		if len(parts) >= 3 {
			var fm SkillFrontmatter
			if err := yaml.Unmarshal(parts[1], &fm); err == nil {
				if strings.TrimSpace(fm.Name) != "" {
					name = strings.TrimSpace(fm.Name)
				}
				if strings.TrimSpace(fm.Description) != "" {
					desc = strings.TrimSpace(fm.Description)
				}
			}
		}
	}

	// Fallback description extraction if frontmatter did not provide one
	if desc == "" {
		desc = extractFallbackDescription(content)
	}
	if desc == "" {
		desc = fmt.Sprintf("Skill %s", name)
	}

	cleanPath := filepath.Clean(filePath)
	return SkillItem{
		Name:        name,
		Description: desc,
		Path:        cleanPath,
		Source:      source,
	}, nil
}

// extractFallbackDescription finds the first non-heading text line in markdown content.
func extractFallbackDescription(content []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	inFrontmatter := false
	frontmatterMarkers := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			inFrontmatter = !inFrontmatter
			frontmatterMarkers++
			continue
		}
		if inFrontmatter || frontmatterMarkers == 1 {
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// First textual line
		if len(line) > 200 {
			return line[:197] + "..."
		}
		return line
	}
	return ""
}
