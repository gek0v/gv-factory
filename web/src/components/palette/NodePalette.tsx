// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import { useFactoryFlowStore } from '../../store';
import { PALETTE_GROUPS, type PaletteItem } from './paletteData';
import {
  WebhookIcon,
  GitHubIcon,
  ChatIcon,
  ClockIcon,
  BrainIcon,
  CodeIcon,
  ShieldIcon,
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

  const handleAdd = (item: PaletteItem) => {
    addNode(item.data);
  };

  return (
    <aside className="node-palette" aria-label="Component Palette">
      <div className="palette-header">
        <h3>Node Palette</h3>
        <span className="palette-badge">v0.2</span>
      </div>
      <div className="palette-scroll">
        {PALETTE_GROUPS.map((group) => (
          <div key={group.name} className="palette-section">
            <div className="palette-section-title">{group.name}</div>
            <div className="palette-grid">
              {group.items.map((item) => (
                <button
                  key={item.label}
                  type="button"
                  className={`palette-item item-${item.type}`}
                  onClick={() => handleAdd(item)}
                  title={`${item.label} - ${item.description}`}
                >
                  <span className="palette-item-icon">
                    {getPaletteIcon(item.iconName)}
                  </span>
                  <div className="palette-item-info">
                    <span className="palette-item-name">{item.label}</span>
                    <span className="palette-item-desc">{item.description}</span>
                  </div>
                  <span className="palette-item-add" aria-hidden="true">
                    <PlusIcon size={14} />
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
