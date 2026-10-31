// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

package schema

import "testing"

func TestSchema_Instantiation(t *testing.T) {
	tpl := FactoryTemplate{
		APIVersion: "factory.ov.io/v1alpha1",
		Kind:       "FactoryTemplate",
		Metadata: Metadata{
			Name: "test-template",
		},
	}
	if tpl.Metadata.Name != "test-template" {
		t.Fatalf("unexpected template name: %s", tpl.Metadata.Name)
	}
}
