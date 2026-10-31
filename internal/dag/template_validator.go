// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// ValidateTemplate verifies structural integrity, reachability, PDK URIs, and gate thresholds.
func ValidateTemplate(tpl *schema.FactoryTemplate) ([]string, error) {
	var issues []string
	if tpl == nil {
		return nil, errors.New("template is nil")
	}

	if tpl.Metadata.Name == "" {
		issues = append(issues, "template metadata.name is required")
	}
	if len(tpl.Spec.Steps) == 0 {
		issues = append(issues, "template spec.steps cannot be empty")
		return issues, fmt.Errorf("template validation failed with %d errors: %s", len(issues), strings.Join(issues, "; "))
	}

	stepMap := make(map[string]schema.StepConfig, len(tpl.Spec.Steps))
	for _, step := range tpl.Spec.Steps {
		if step.ID == "" {
			issues = append(issues, "step ID cannot be empty")
			continue
		}
		if _, exists := stepMap[step.ID]; exists {
			issues = append(issues, fmt.Sprintf("duplicate step ID %q", step.ID))
		}
		stepMap[step.ID] = step

		if step.Plugin == "" {
			issues = append(issues, fmt.Sprintf("step %q missing plugin URI", step.ID))
		} else if !strings.HasPrefix(step.Plugin, "extism://") {
			issues = append(issues, fmt.Sprintf("step %q plugin URI %q must conform to PDK convention (prefix extism://)", step.ID, step.Plugin))
		}
	}

	if tpl.Spec.Entrypoint == "" {
		issues = append(issues, "spec.entrypoint cannot be empty")
	} else if _, exists := stepMap[tpl.Spec.Entrypoint]; !exists {
		issues = append(issues, fmt.Sprintf("entrypoint %q does not exist in spec.steps", tpl.Spec.Entrypoint))
	}

	adj := make(map[string][]string)
	for _, step := range tpl.Spec.Steps {
		for _, dep := range step.DependsOn {
			if _, exists := stepMap[dep]; !exists {
				issues = append(issues, fmt.Sprintf("step %q depends on non-existent step %q", step.ID, dep))
				continue
			}
			adj[dep] = append(adj[dep], step.ID)
		}
	}

	if len(tpl.Spec.Steps) > 1 && tpl.Spec.Entrypoint != "" {
		if len(adj[tpl.Spec.Entrypoint]) == 0 {
			issues = append(issues, fmt.Sprintf("entrypoint %q does not connect to any subsequent steps", tpl.Spec.Entrypoint))
		}
	}

	visited := make(map[string]int)
	var hasCycle bool
	var dfs func(string)
	dfs = func(u string) {
		visited[u] = 1
		for _, v := range adj[u] {
			if visited[v] == 1 {
				hasCycle = true
				issues = append(issues, fmt.Sprintf("cycle detected involving %q and %q", u, v))
				return
			}
			if visited[v] == 0 {
				dfs(v)
			}
		}
		visited[u] = 2
	}

	for _, step := range tpl.Spec.Steps {
		if visited[step.ID] == 0 {
			dfs(step.ID)
		}
	}

	if tpl.Spec.Entrypoint != "" && !hasCycle {
		reachable := make(map[string]bool)
		var queue []string
		queue = append(queue, tpl.Spec.Entrypoint)
		reachable[tpl.Spec.Entrypoint] = true

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			for _, nxt := range adj[curr] {
				if !reachable[nxt] {
					reachable[nxt] = true
					queue = append(queue, nxt)
				}
			}
		}

		for _, step := range tpl.Spec.Steps {
			if !reachable[step.ID] {
				issues = append(issues, fmt.Sprintf("isolated step node %q is unreachable from entrypoint %q", step.ID, tpl.Spec.Entrypoint))
			}
		}
	}

	gates := tpl.Spec.Gates
	if gates.MinCoveragePercent < 0.0 || gates.MinCoveragePercent > 100.0 {
		issues = append(issues, fmt.Sprintf("min_coverage_percent must be between 0.0 and 100.0 (got %.1f)", gates.MinCoveragePercent))
	}
	if gates.MaxSecurityAlerts < 0 {
		issues = append(issues, fmt.Sprintf("max_security_alerts cannot be negative (got %d)", gates.MaxSecurityAlerts))
	}

	if len(issues) > 0 {
		return issues, fmt.Errorf("template validation failed: %s", strings.Join(issues, "; "))
	}

	return nil, nil
}
