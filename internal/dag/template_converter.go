// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// TemplateToCanvas converts a declarative FactoryTemplate into an @xyflow/react canvas graph
// with deterministic topological positioning.
func TemplateToCanvas(tpl *schema.FactoryTemplate) (*CanvasGraph, error) {
	if tpl == nil {
		return nil, errors.New("template cannot be nil")
	}

	depths := make(map[string]int)
	if tpl.Spec.Entrypoint != "" {
		depths[tpl.Spec.Entrypoint] = 0
	}

	changed := true
	for iter := 0; iter < len(tpl.Spec.Steps)+1 && changed; iter++ {
		changed = false
		for _, step := range tpl.Spec.Steps {
			if step.ID == tpl.Spec.Entrypoint {
				continue
			}
			maxDep := -1
			for _, dep := range step.DependsOn {
				if d, ok := depths[dep]; ok {
					if d > maxDep {
						maxDep = d
					}
				}
			}
			if maxDep >= 0 {
				newDepth := maxDep + 1
				if depths[step.ID] != newDepth {
					depths[step.ID] = newDepth
					changed = true
				}
			}
		}
	}

	depthBuckets := make(map[int][]schema.StepConfig)
	for _, step := range tpl.Spec.Steps {
		d := depths[step.ID]
		depthBuckets[d] = append(depthBuckets[d], step)
	}

	var depthKeys []int
	for d := range depthBuckets {
		depthKeys = append(depthKeys, d)
	}
	sort.Ints(depthKeys)

	var nodes []CanvasNode
	var edges []CanvasEdge

	for _, d := range depthKeys {
		bucket := depthBuckets[d]
		sort.Slice(bucket, func(i, j int) bool {
			return bucket[i].ID < bucket[j].ID
		})

		for idx, step := range bucket {
			nodeType, role, reviewerSpecialty, subtype, model := inferNodeTypeAndRole(step, tpl.Spec.Entrypoint)

			cfgCopy := make(map[string]any, len(step.Config))
			for k, v := range step.Config {
				cfgCopy[k] = v
			}

			xPos := float64(d)*280.0 + 80.0
			yPos := float64(idx)*140.0 + 80.0

			node := CanvasNode{
				ID:   step.ID,
				Type: nodeType,
				Position: CanvasPosition{
					X: xPos,
					Y: yPos,
				},
				Data: CanvasNodeData{
					Label:             step.Name,
					Description:       fmt.Sprintf("WASM Plugin: %s", step.Plugin),
					Status:            "pending",
					NodeType:          nodeType,
					Role:              role,
					ReviewerSpecialty: reviewerSpecialty,
					Subtype:           subtype,
					Model:             model,
					PluginTag:         step.Plugin,
					Parallel:          step.Parallel,
					Config:            cfgCopy,
					StepID:            step.ID,
				},
			}
			nodes = append(nodes, node)

			for _, dep := range step.DependsOn {
				edge := CanvasEdge{
					ID:        fmt.Sprintf("e-%s-%s", dep, step.ID),
					Source:    dep,
					Target:    step.ID,
					MarkerEnd: map[string]any{"type": "arrowclosed"},
				}
				edges = append(edges, edge)
			}
		}
	}

	return &CanvasGraph{
		Nodes:    nodes,
		Edges:    edges,
		Metadata: tpl.Metadata,
		Gates:    tpl.Spec.Gates,
	}, nil
}

// CanvasToTemplate converts an @xyflow/react canvas graph back into a valid schema.FactoryTemplate.
func CanvasToTemplate(cg *CanvasGraph) (*schema.FactoryTemplate, error) {
	if cg == nil {
		return nil, errors.New("canvas graph is nil")
	}

	incoming := make(map[string][]string)
	for _, edge := range cg.Edges {
		incoming[edge.Target] = append(incoming[edge.Target], edge.Source)
	}

	sortedNodes := make([]CanvasNode, len(cg.Nodes))
	copy(sortedNodes, cg.Nodes)
	sort.Slice(sortedNodes, func(i, j int) bool {
		if sortedNodes[i].Position.X != sortedNodes[j].Position.X {
			return sortedNodes[i].Position.X < sortedNodes[j].Position.X
		}
		return sortedNodes[i].Position.Y < sortedNodes[j].Position.Y
	})

	var entrypoint string
	steps := make([]schema.StepConfig, 0, len(sortedNodes))

	for _, node := range sortedNodes {
		deps := incoming[node.ID]
		sort.Strings(deps)

		if len(deps) == 0 && entrypoint == "" {
			entrypoint = node.ID
		}

		pluginURI := node.Data.PluginTag
		if pluginURI == "" {
			pluginURI = fmt.Sprintf("extism://plugins/%s.wasm", node.ID)
		}

		name := node.Data.Label
		if name == "" {
			name = node.ID
		}

		step := schema.StepConfig{
			ID:        node.ID,
			Name:      name,
			Plugin:    pluginURI,
			Parallel:  node.Data.Parallel,
			DependsOn: deps,
			Config:    node.Data.Config,
		}
		steps = append(steps, step)
	}

	if entrypoint == "" && len(steps) > 0 {
		entrypoint = steps[0].ID
	}

	meta := cg.Metadata
	if meta.Name == "" {
		meta.Name = "canvas-exported-factory"
		meta.Version = "0.1.0"
		meta.Description = "Exported from interactive DAG canvas"
	}

	tpl := &schema.FactoryTemplate{
		APIVersion: "factory.ov.io/v1alpha1",
		Kind:       "FactoryTemplate",
		Metadata:   meta,
		Spec: schema.FactorySpec{
			Entrypoint: entrypoint,
			Steps:      steps,
			Gates:      cg.Gates,
		},
	}

	return tpl, nil
}

func inferNodeTypeAndRole(step schema.StepConfig, entrypoint string) (nodeType, role, reviewerSpecialty, subtype, model string) {
	p := strings.ToLower(step.Plugin + " " + step.ID)

	if step.ID == entrypoint || strings.Contains(p, "ingestion") || strings.Contains(p, "trigger") {
		nodeType = "trigger"
		subtype = "webhook"
		return
	}

	if strings.Contains(p, "reviewer") {
		nodeType = "agent"
		role = "reviewer"
		model = "claude-3-5-sonnet"
		if strings.Contains(p, "security") {
			reviewerSpecialty = "security"
		} else if strings.Contains(p, "arch") {
			reviewerSpecialty = "arch"
		} else if strings.Contains(p, "uiux") || strings.Contains(p, "ui-ux") {
			reviewerSpecialty = "ui"
		} else {
			reviewerSpecialty = "code"
		}
		return
	}

	if strings.Contains(p, "approver") || strings.Contains(p, "gate") {
		nodeType = "agent"
		role = "approver"
		model = "claude-3-5-sonnet"
		return
	}

	if strings.Contains(p, "builder") || strings.Contains(p, "patch") {
		nodeType = "agent"
		role = "builder_tdd"
		model = "claude-3-5-sonnet"
		return
	}

	nodeType = "agent"
	role = "spec_writer"
	model = "claude-3-5-sonnet"
	return
}
