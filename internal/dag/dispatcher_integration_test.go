// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package dag

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gv-factory/gv-factory/internal/gateway"
	"github.com/gv-factory/gv-factory/internal/plugins"
	"github.com/gv-factory/gv-factory/pkg/schema"
)

func TestDispatcher_RealWASMIntegration(t *testing.T) {
	wasmPath := filepath.Join("..", "plugins", "testdata", "hello.wasm")
	if _, err := os.Stat(wasmPath); os.IsNotExist(err) {
		t.Skipf("hello.wasm fixture not found at %s, skipping real WASM test", wasmPath)
	}

	ctx := context.Background()
	host := plugins.NewHost()
	defer host.Close()

	manifest := plugins.PluginManifest{
		Name:       "hello-plugin",
		Version:    "1.0.0",
		Entrypoint: "run_test",
	}

	_, err := host.LoadPluginFromPath(ctx, manifest, wasmPath)
	if err != nil {
		t.Fatalf("failed to load WASM plugin: %v", err)
	}

	engine := NewEngine()
	gw := gateway.NewGatewayWithConfig(gateway.Config{DisableNATS: true})
	defer gw.Close()

	dispatcher := NewDispatcher(engine, host, gw, DispatcherConfig{
		DefaultStepPolicy: StepPolicy{
			MaxRetries:     1,
			InitialBackoff: 10 * time.Millisecond,
			Timeout:        5 * time.Second,
		},
	})

	tpl := schema.FactoryTemplate{
		Metadata: schema.Metadata{
			Name: "wasm-pipeline",
		},
		Spec: schema.FactorySpec{
			Steps: []schema.StepConfig{
				{
					ID:     "step-hello",
					Name:   "Run Hello WASM",
					Plugin: "hello-plugin",
					Config: map[string]any{
						"entrypoint": "run_test",
					},
				},
			},
		},
	}

	report, err := dispatcher.ExecuteWorkflow(ctx, "run-wasm-e2e", tpl)
	if err != nil {
		t.Fatalf("WASM workflow execution failed: %v", err)
	}

	if report.Status != StatusCompleted {
		t.Fatalf("expected StatusCompleted, got %s", report.Status)
	}
	if len(report.CompletedSteps) != 1 || report.CompletedSteps[0] != "step-hello" {
		t.Fatalf("unexpected completed steps: %v", report.CompletedSteps)
	}

	res := report.StepResults["step-hello"]
	if res == nil || !res.Success || res.Output != "Hello, world!" {
		t.Errorf("unexpected WASM output: %+v", res)
	}
}
