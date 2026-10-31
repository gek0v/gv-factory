// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useMemo } from 'react';
import { useFactoryFlowStore } from '../../store';
import { CANONICAL_TEMPLATES } from '../../templates/catalog';
import { templateToCanvas } from '../../templates/parser';
import { canvasToTemplate, exportTemplateToYaml, exportTemplateToJson } from '../../templates/exporter';
import { validateFactoryTemplate } from '../../templates/validator';

export const TemplateToolbar: React.FC = () => {
  const { nodes, edges, setNodes, setEdges } = useFactoryFlowStore();
  const [selectedTemplateKey, setSelectedTemplateKey] = useState<string>('feature-complete');
  const [showExportModal, setShowExportModal] = useState<boolean>(false);
  const [exportFormat, setExportFormat] = useState<'yaml' | 'json'>('yaml');
  const [copied, setCopied] = useState<boolean>(false);

  // Derive template from current canvas to validate in real-time
  const currentTemplate = useMemo(() => {
    return canvasToTemplate(nodes, edges);
  }, [nodes, edges]);

  const validation = useMemo(() => {
    return validateFactoryTemplate(currentTemplate);
  }, [currentTemplate]);

  const exportedText = useMemo(() => {
    if (exportFormat === 'yaml') {
      return exportTemplateToYaml(currentTemplate);
    }
    return exportTemplateToJson(currentTemplate);
  }, [currentTemplate, exportFormat]);

  const handleLoadTemplate = (key: string) => {
    setSelectedTemplateKey(key);
    const entry = CANONICAL_TEMPLATES[key];
    if (entry) {
      const parsed = templateToCanvas(entry.template);
      setNodes(parsed.nodes);
      setEdges(parsed.edges);
    }
  };

  const handleCopy = () => {
    navigator.clipboard.writeText(exportedText);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleDownload = () => {
    const filename = `${currentTemplate.metadata.name || 'factory-template'}.${exportFormat}`;
    const blob = new Blob([exportedText], {
      type: exportFormat === 'yaml' ? 'text/yaml' : 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="template-toolbar">
      <div className="template-select-group">
        <label htmlFor="template-select" className="template-label">
          Template:
        </label>
        <select
          id="template-select"
          className="template-dropdown"
          value={selectedTemplateKey}
          onChange={(e) => handleLoadTemplate(e.target.value)}
        >
          {Object.entries(CANONICAL_TEMPLATES).map(([key, item]) => (
            <option key={key} value={key}>
              {item.label}
            </option>
          ))}
        </select>
      </div>

      <div className="template-validation-pill-container" title={validation.errors.join('\n')}>
        {validation.isValid ? (
          <span className="template-valid-pill">✓ Valid Template</span>
        ) : (
          <span className="template-invalid-pill" title={validation.errors.join('; ')}>
            ⚠ {validation.errors.length} {validation.errors.length === 1 ? 'Error' : 'Errors'}
          </span>
        )}
      </div>

      <button
        type="button"
        className="template-export-btn"
        onClick={() => setShowExportModal(true)}
      >
        <span>Export DAG</span>
      </button>

      {showExportModal && (
        <div className="export-modal-backdrop" onClick={() => setShowExportModal(false)}>
          <div className="export-modal" onClick={(e) => e.stopPropagation()}>
            <div className="export-modal-header">
              <h3>Export Factory Template</h3>
              <button
                type="button"
                className="export-modal-close"
                onClick={() => setShowExportModal(false)}
              >
                ✕
              </button>
            </div>

            <div className="export-format-selector">
              <button
                type="button"
                className={`format-btn ${exportFormat === 'yaml' ? 'active' : ''}`}
                onClick={() => setExportFormat('yaml')}
              >
                YAML
              </button>
              <button
                type="button"
                className={`format-btn ${exportFormat === 'json' ? 'active' : ''}`}
                onClick={() => setExportFormat('json')}
              >
                JSON
              </button>
            </div>

            <pre className="export-code-view">
              <code>{exportedText}</code>
            </pre>

            <div className="export-modal-actions">
              <button type="button" className="copy-btn" onClick={handleCopy}>
                {copied ? '✓ Copied!' : 'Copy to Clipboard'}
              </button>
              <button type="button" className="download-btn" onClick={handleDownload}>
                Download File
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
