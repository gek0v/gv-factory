// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import { ReactFlow, Controls, Background, BackgroundVariant } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { useFactoryFlowStore } from './store';

export const App: React.FC = () => {
  const { nodes, edges, onNodesChange, onEdgesChange } = useFactoryFlowStore();

  return (
    <div className="canvas-container">
      <header className="header-bar">
        <h1>OV-Factory Orchestrator Canvas</h1>
        <div style={{ fontSize: '0.85rem', color: '#94a3b8' }}>
          Pipeline Status: Ready | Active Sandboxes: 0
        </div>
      </header>
      <main className="flow-wrapper">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          fitView
        >
          <Background color="#334155" variant={BackgroundVariant.Dots} gap={16} size={1} />
          <Controls />
        </ReactFlow>
      </main>
    </div>
  );
};

export default App;
