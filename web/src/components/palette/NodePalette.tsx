// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useState, useEffect } from 'react';
import { useFactoryFlowStore } from '../../store';
import { PALETTE_GROUPS, type PaletteItem } from './paletteData';
import {
  fetchAgentTemplates,
  DEFAULT_AGENT_TEMPLATES,
  type AgentTemplate,
} from '../../api/registry';
import type { AgentNodeData } from '../../types/flow';
import {
  WebhookIcon,
  GitHubIcon,
  ChatIcon,
  ClockIcon,
  BrainIcon,
  CodeIcon,
  ShieldIcon,
  ShieldCheckIcon,
  CheckCircleIcon,
  GitBranchIcon,
  SlidersIcon,
  BotIcon,
  GitPullRequestIcon,
  GitCommitIcon,
  SendIcon,
  PlusIcon,
} from '../icons/Icons';

const getPaletteIcon = (iconName: string) => {
  switch (iconName) {
    case 'webhook':
      return <WebhookIcon size={15} />;
    case 'github':
      return <GitHubIcon size={15} />;
    case 'chat':
      return <ChatIcon size={15} />;
    case 'clock':
      return <ClockIcon size={15} />;
    case 'brain':
      return <BrainIcon size={15} />;
    case 'code':
      return <CodeIcon size={15} />;
    case 'shield':
      return <ShieldIcon size={15} />;
    case 'shield-check':
      return <ShieldCheckIcon size={15} />;
    case 'check-circle':
      return <CheckCircleIcon size={15} />;
    case 'branch':
      return <GitBranchIcon size={15} />;
    case 'sliders':
      return <SlidersIcon size={15} />;
    case 'bot':
      return <BotIcon size={15} />;
    case 'pr':
      return <GitPullRequestIcon size={15} />;
    case 'commit':
      return <GitCommitIcon size={15} />;
    case 'send':
      return <SendIcon size={15} />;
    default:
      return <PlusIcon size={15} />;
  }
};

export const NodePalette: React.FC = () => {
  const addNode = useFactoryFlowStore((state) => state.addNode);
  const [templates, setTemplates] = useState<AgentTemplate[]>(DEFAULT_AGENT_TEMPLATES);

  useEffect(() => {
    let isMounted = true;
    fetchAgentTemplates()
      .then((tpls) => {
        if (isMounted && tpls && tpls.length > 0) {
          setTemplates(tpls);
        }
      })
      .catch(() => {
        // Fallback to default templates
      });

    return () => {
      isMounted = false;
    };
  }, []);

  const handleAdd = (item: PaletteItem) => {
    addNode(item.data);
  };

  const handleDragStartItem = (event: React.DragEvent, item: PaletteItem) => {
    event.dataTransfer.setData('application/reactflow-item', JSON.stringify(item.data));
    event.dataTransfer.effectAllowed = 'move';
  };

  const handleAddTemplate = (tpl: AgentTemplate) => {
    const agentNodeData: Partial<AgentNodeData> = {
      nodeType: 'agent',
      role: (tpl.role as AgentNodeData['role']) || 'builder_tdd',
      label: tpl.name,
      description: tpl.description,
      model: tpl.model,
      templateId: tpl.id,
      skills: tpl.skills ? [...tpl.skills] : [],
      tools: tpl.tools ? [...tpl.tools] : [],
      plugins: tpl.plugins ? [...tpl.plugins] : [],
      thinking: tpl.thinking || 'off',
      prompt: tpl.prompt || '',
      pluginTag: tpl.plugins?.[0] || 'extism://plugins/tdd-runner.wasm',
      status: 'pending',
      llmConfig: {
        provider: 'anthropic',
        model: tpl.model,
        temperature: tpl.temperature ?? 0.2,
        maxIterations: tpl.maxIterations ?? 10,
        authorizedTools: tpl.tools ? [...tpl.tools] : ['bash_exec', 'git_read', 'file_writer'],
      },
      sandboxEnabled: true,
    };
    addNode(agentNodeData);
  };

  const handleDragStartTemplate = (event: React.DragEvent, tpl: AgentTemplate) => {
    event.dataTransfer.setData('application/reactflow-template', JSON.stringify(tpl));
    event.dataTransfer.effectAllowed = 'move';
  };

  return (
    <aside className="node-palette" aria-label="Component Palette">
      <div className="palette-header">
        <div className="palette-header-title">
          <span className="palette-header-icon" aria-hidden="true">❖</span>
          <h3>Components</h3>
        </div>
        <span className="palette-badge">Catalog</span>
      </div>
      <div className="palette-scroll">
        {/* AGENT TEMPLATES Section */}
        <div className="palette-section palette-section-templates">
          <div className="palette-section-title">
            <span>Agent Templates</span>
            <span className="palette-section-count">{templates.length}</span>
          </div>
          <div className="palette-grid">
            {templates.map((tpl) => (
              <button
                key={tpl.id}
                type="button"
                className="palette-item item-agent item-template"
                draggable
                onDragStart={(e) => handleDragStartTemplate(e, tpl)}
                onClick={() => handleAddTemplate(tpl)}
                title={`${tpl.name} - ${tpl.description} (Drag or click to add)`}
                data-testid={`palette-template-${tpl.id}`}
              >
                <span className="palette-item-icon">
                  <BotIcon size={15} />
                </span>
                <div className="palette-item-info">
                  <span className="palette-item-name">{tpl.name}</span>
                  <span className="palette-item-desc">{tpl.description}</span>
                </div>
                <span className="palette-item-add" aria-hidden="true">
                  <PlusIcon size={13} />
                </span>
              </button>
            ))}
          </div>
        </div>

        {/* Standard Component Groups */}
        {PALETTE_GROUPS.map((group) => (
          <div key={group.name} className="palette-section">
            <div className="palette-section-title">
              <span>{group.name}</span>
              <span className="palette-section-count">{group.items.length}</span>
            </div>
            <div className="palette-grid">
              {group.items.map((item) => (
                <button
                  key={item.label}
                  type="button"
                  className={`palette-item item-${item.type}`}
                  draggable
                  onDragStart={(e) => handleDragStartItem(e, item)}
                  onClick={() => handleAdd(item)}
                  title={`${item.label} - ${item.description} (Drag or click to add)`}
                >
                  <span className="palette-item-icon">
                    {getPaletteIcon(item.iconName)}
                  </span>
                  <div className="palette-item-info">
                    <span className="palette-item-name">{item.label}</span>
                    <span className="palette-item-desc">{item.description}</span>
                  </div>
                  <span className="palette-item-add" aria-hidden="true">
                    <PlusIcon size={13} />
                  </span>
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
    </aside>
  );
};
