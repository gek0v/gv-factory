// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gv-factory/gv-factory/internal/registry"
)

func writeRegistryJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeRegistryError(w http.ResponseWriter, status int, message string) {
	writeRegistryJSON(w, status, map[string]string{"error": message})
}

// registerRegistryEndpoints mounts all global registry REST endpoints on the HTTP multiplexer.
func registerRegistryEndpoints(mux *http.ServeMux, mgr *registry.Manager) {
	if mgr == nil {
		mgr = registry.NewManager(registry.ManagerOptions{})
	}

	// GET /api/v1/skills
	mux.HandleFunc("/api/v1/skills", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRegistryError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		skills, err := mgr.ListSkills(r.Context())
		if err != nil {
			writeRegistryError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if skills == nil {
			skills = make([]registry.SkillItem, 0)
		}
		writeRegistryJSON(w, http.StatusOK, skills)
	})

	// GET /api/v1/tools
	mux.HandleFunc("/api/v1/tools", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRegistryError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		category := strings.TrimSpace(r.URL.Query().Get("category"))
		var tools []registry.ToolItem
		if category != "" {
			tools = mgr.Tools.ListByCategory(category)
		} else {
			tools = mgr.ListTools()
		}
		if tools == nil {
			tools = make([]registry.ToolItem, 0)
		}
		writeRegistryJSON(w, http.StatusOK, tools)
	})

	// GET /api/v1/plugins
	mux.HandleFunc("/api/v1/plugins", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeRegistryError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		plugins, err := mgr.ListPlugins(r.Context())
		if err != nil {
			writeRegistryError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if plugins == nil {
			plugins = make([]registry.PluginItem, 0)
		}
		writeRegistryJSON(w, http.StatusOK, plugins)
	})

	// GET /api/v1/agent-templates, POST /api/v1/agent-templates
	mux.HandleFunc("/api/v1/agent-templates", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			templates := mgr.ListTemplates()
			if templates == nil {
				templates = make([]registry.AgentTemplate, 0)
			}
			writeRegistryJSON(w, http.StatusOK, templates)

		case http.MethodPost:
			var tpl registry.AgentTemplate
			if err := json.NewDecoder(r.Body).Decode(&tpl); err != nil {
				writeRegistryError(w, http.StatusBadRequest, "invalid json payload: "+err.Error())
				return
			}

			if err := mgr.SaveTemplate(tpl); err != nil {
				writeRegistryError(w, http.StatusBadRequest, err.Error())
				return
			}

			writeRegistryJSON(w, http.StatusCreated, map[string]any{
				"success":  true,
				"template": tpl,
			})

		default:
			writeRegistryError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})

	// GET /api/v1/agent-templates/{id}
	mux.HandleFunc("/api/v1/agent-templates/", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			path := strings.TrimPrefix(r.URL.Path, "/api/v1/agent-templates/")
			id = strings.Trim(path, "/")
		}

		if id == "" {
			writeRegistryError(w, http.StatusBadRequest, "template id is required")
			return
		}

		switch r.Method {
		case http.MethodGet:
			tpl, err := mgr.GetTemplate(id)
			if err != nil {
				writeRegistryError(w, http.StatusNotFound, err.Error())
				return
			}
			writeRegistryJSON(w, http.StatusOK, tpl)

		default:
			writeRegistryError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}
