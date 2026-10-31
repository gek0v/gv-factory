// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"encoding/json"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/gv-factory/gv-factory/pkg/schema"
)

// CanvasPosition represents X/Y canvas coordinates for @xyflow/react nodes.
type CanvasPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// CanvasNodeData holds react-flow custom node payload matching TypeScript types.
type CanvasNodeData struct {
	Label             string         `json:"label"`
	Description       string         `json:"description,omitempty"`
	Status            string         `json:"status"`
	NodeType          string         `json:"nodeType"`
	Role              string         `json:"role,omitempty"`
	ReviewerSpecialty string         `json:"reviewerSpecialty,omitempty"`
	Subtype           string         `json:"subtype,omitempty"`
	Model             string         `json:"model,omitempty"`
	PluginTag         string         `json:"pluginTag,omitempty"`
	Parallel          bool           `json:"parallel,omitempty"`
	Config            map[string]any `json:"config,omitempty"`
	StepID            string         `json:"stepId,omitempty"`
}

// CanvasNode represents an @xyflow/react node.
type CanvasNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Position CanvasPosition `json:"position"`
	Data     CanvasNodeData `json:"data"`
}

// CanvasEdge represents an @xyflow/react edge with source and target step IDs.
type CanvasEdge struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	MarkerEnd any    `json:"markerEnd,omitempty"`
}

// CanvasGraph represents the full serializable state of an active canvas graph.
type CanvasGraph struct {
	Nodes    []CanvasNode       `json:"nodes"`
	Edges    []CanvasEdge       `json:"edges"`
	Metadata schema.Metadata    `json:"metadata"`
	Gates    schema.GatesConfig `json:"gates"`
}

// ParseTemplateYAML unmarshals YAML data into a schema.FactoryTemplate.
func ParseTemplateYAML(data []byte) (*schema.FactoryTemplate, error) {
	var tpl schema.FactoryTemplate
	if err := yaml.Unmarshal(data, &tpl); err != nil {
		return nil, fmt.Errorf("failed to parse template YAML: %w", err)
	}
	return &tpl, nil
}

// ParseTemplateJSON unmarshals JSON data into a schema.FactoryTemplate.
func ParseTemplateJSON(data []byte) (*schema.FactoryTemplate, error) {
	var tpl schema.FactoryTemplate
	if err := json.Unmarshal(data, &tpl); err != nil {
		return nil, fmt.Errorf("failed to parse template JSON: %w", err)
	}
	return &tpl, nil
}

// ExportTemplateYAML marshals a schema.FactoryTemplate to valid YAML bytes.
func ExportTemplateYAML(tpl *schema.FactoryTemplate) ([]byte, error) {
	if tpl == nil {
		return nil, errors.New("template cannot be nil")
	}
	data, err := yaml.Marshal(tpl)
	if err != nil {
		return nil, fmt.Errorf("failed to export template YAML: %w", err)
	}
	return data, nil
}

// ExportTemplateJSON marshals a schema.FactoryTemplate to formatted JSON bytes.
func ExportTemplateJSON(tpl *schema.FactoryTemplate) ([]byte, error) {
	if tpl == nil {
		return nil, errors.New("template cannot be nil")
	}
	data, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to export template JSON: %w", err)
	}
	return data, nil
}
