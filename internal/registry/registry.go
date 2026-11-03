// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package registry

import (
	"context"
	"os"
)

// ManagerOptions configures roots and storage for the unified registry manager.
type ManagerOptions struct {
	WorkspaceRoot   string
	IncludeUserHome bool
	TemplatesDir    string
}

// Manager coordinates skills, tools, plugins, and agent template registries.
type Manager struct {
	Skills    *SkillsRegistry
	Tools     *ToolsRegistry
	Plugins   *PluginsRegistry
	Templates *TemplateRegistry
}

// NewManager initializes a new unified registry manager.
func NewManager(opts ManagerOptions) *Manager {
	wsRoot := opts.WorkspaceRoot
	if wsRoot == "" {
		if wd, err := os.Getwd(); err == nil {
			wsRoot = wd
		} else {
			wsRoot = "."
		}
	}

	return &Manager{
		Skills: NewSkillsRegistryWithOptions(SkillsRegistryOptions{
			WorkspaceRoot:   wsRoot,
			IncludeUserHome: opts.IncludeUserHome,
		}),
		Tools: NewToolsRegistry(),
		Plugins: NewPluginsRegistryWithOptions(PluginsRegistryOptions{
			WorkspaceRoot:   wsRoot,
			IncludeUserHome: opts.IncludeUserHome,
		}),
		Templates: NewTemplateRegistry(opts.TemplatesDir),
	}
}

// ListSkills returns all discovered and registered skills.
func (m *Manager) ListSkills(ctx context.Context) ([]SkillItem, error) {
	return m.Skills.List(ctx)
}

// ListTools returns all available built-in, brain and custom tools.
func (m *Manager) ListTools() []ToolItem {
	return m.Tools.List()
}

// ListPlugins returns all discovered and registered plugins/extensions.
func (m *Manager) ListPlugins(ctx context.Context) ([]PluginItem, error) {
	return m.Plugins.List(ctx)
}

// ListTemplates returns all agent templates.
func (m *Manager) ListTemplates() []AgentTemplate {
	return m.Templates.List()
}

// GetTemplate retrieves a template by ID.
func (m *Manager) GetTemplate(id string) (*AgentTemplate, error) {
	return m.Templates.Get(id)
}

// SaveTemplate creates or updates an agent template.
func (m *Manager) SaveTemplate(tpl AgentTemplate) error {
	return m.Templates.Save(tpl)
}

// DeleteTemplate removes an agent template by ID.
func (m *Manager) DeleteTemplate(id string) error {
	return m.Templates.Delete(id)
}
