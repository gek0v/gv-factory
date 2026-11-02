// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React, { useMemo, useEffect } from 'react';
import {
  ReactFlow,
  Controls,
  Background,
  BackgroundVariant,
  MiniMap,
  type Node,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import { useFactoryFlowStore } from './store';
import type { FactoryNodeData } from './types/flow';
import { TriggerNode } from './components/nodes/TriggerNode';
import { AgentNode } from './components/nodes/AgentNode';
import { LogicNode } from './components/nodes/LogicNode';
import { ActionNode } from './components/nodes/ActionNode';
import { DecisionGateNode } from './components/nodes/DecisionGateNode';
import { NodePalette } from './components/palette/NodePalette';
import { NodeInspector } from './components/drawer/NodeInspector';
import { Toolbar } from './components/Toolbar';
import { GateResolutionModal } from './components/gate/GateResolutionModal';
import { FactoryRunModal } from './components/factory/FactoryRunModal';
import { WorkerClusterHealthButton, WorkerDashboardModal } from './components/workers/WorkerDashboard';
import { BrainExplorerButton, BrainExplorerModal } from './components/BrainExplorerModal';
import { ExecutionTimelineDrawer, TimelineButton } from './components/timeline/ExecutionTimelineDrawer';
import { CostBudgetButton, CostBudgetModal } from './components/telemetry/CostBudgetModal';
import { defaultGatewayClient } from './api/gateway';

export const App: React.FC = () => {
  const {
    nodes,
    edges,
    onNodesChange,
    onEdgesChange,
    onConnect,
    isValidConnection,
    backgroundVariant,
    setBackgroundVariant,
    selectedNodeId,
    gatewayStatus,
  } = useFactoryFlowStore();

  useEffect(() => {
    defaultGatewayClient.connect();
    return () => {
      defaultGatewayClient.disconnect();
    };
  }, []);

  const nodeTypes = useMemo(
    () => ({
      trigger: TriggerNode,
      agent: AgentNode,
      logic: LogicNode,
      action: ActionNode,
      decision_gate: DecisionGateNode,
    }),
    []
  );

  const stats = useMemo(() => {
    let running = 0;
    let completed = 0;
    let failed = 0;
    let awaitingApproval = 0;

    for (const n of nodes) {
      const status = n.data?.status;
      if (status === 'running') running++;
      else if (status === 'completed') completed++;
      else if (status === 'failed') failed++;
      else if (status === 'awaiting_approval') awaitingApproval++;
    }

    return { total: nodes.length, running, completed, failed, awaitingApproval };
  }, [nodes]);

  const getNodeColor = (node: Node) => {
    const data = node.data as unknown as FactoryNodeData;
    switch (data?.nodeType) {
      case 'trigger':
        return '#0284c7';
      case 'agent':
        return '#7c3aed';
      case 'logic':
        return '#d97706';
      case 'decision_gate':
        return '#f59e0b';
      case 'action':
        return '#059669';
      default:
        return '#475569';
    }
  };

  return (
    <div className="canvas-container">
      <header className="header-bar">
        <div className="header-left">
          <div className="header-logo">
            <span className="logo-icon">⚡</span>
            <h1>OV-Factory Orchestrator Canvas</h1>
          </div>
          <span className="header-badge">v0.2.0 • DAG Engine</span>
        </div>

        <div className="header-stats">
          <span className="stat-pill">
            Nodes: <strong>{stats.total}</strong>
          </span>
          <span className="stat-pill stat-running">
            Running: <strong>{stats.running}</strong>
          </span>
          <span className="stat-pill stat-completed">
            Completed: <strong>{stats.completed}</strong>
          </span>
          {stats.awaitingApproval > 0 && (
            <span className="stat-pill stat-approval">
              Approval Req: <strong>{stats.awaitingApproval}</strong>
            </span>
          )}
          {stats.failed > 0 && (
            <span className="stat-pill stat-failed">
              Failed: <strong>{stats.failed}</strong>
            </span>
          )}
        </div>

        <div className="header-actions">
          <TimelineButton />
          <BrainExplorerButton />
          <WorkerClusterHealthButton />
          <CostBudgetButton />
          <div className="gateway-status-pill" title={`Gateway SSE Status: ${gatewayStatus}`}>
            <span className={`status-dot dot-${gatewayStatus}`} />
            <span className="gateway-status-text">
              {gatewayStatus === 'connected'
                ? 'SSE Live'
                : gatewayStatus === 'connecting'
                ? 'Connecting...'
                : gatewayStatus === 'reconnecting'
                ? 'Reconnecting...'
                : 'SSE Offline'}
            </span>
          </div>

          <Toolbar />
          <div className="bg-toggle-group" title="Canvas Background Style">
            <button
              type="button"
              className={`bg-btn ${
                backgroundVariant === BackgroundVariant.Dots ? 'active' : ''
              }`}
              onClick={() => setBackgroundVariant(BackgroundVariant.Dots)}
            >
              Dots
            </button>
            <button
              type="button"
              className={`bg-btn ${
                backgroundVariant === BackgroundVariant.Lines ? 'active' : ''
              }`}
              onClick={() => setBackgroundVariant(BackgroundVariant.Lines)}
            >
              Lines
            </button>
            <button
              type="button"
              className={`bg-btn ${
                backgroundVariant === BackgroundVariant.Cross ? 'active' : ''
              }`}
              onClick={() => setBackgroundVariant(BackgroundVariant.Cross)}
            >
              Cross
            </button>
          </div>
        </div>
      </header>

      <div className="workspace-layout">
        <NodePalette />

        <main className="flow-wrapper">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            isValidConnection={isValidConnection}
            nodeTypes={nodeTypes}
            fitView
          >
            <Background
              color="#334155"
              variant={backgroundVariant}
              gap={16}
              size={1}
            />
            <Controls className="factory-controls" />
            <MiniMap
              nodeColor={getNodeColor}
              nodeStrokeWidth={2}
              zoomable
              pannable
              className="factory-minimap"
            />
          </ReactFlow>
        </main>

        {selectedNodeId && <NodeInspector />}
      </div>

      <ExecutionTimelineDrawer />
      <GateResolutionModal />
      <WorkerDashboardModal />
      <BrainExplorerModal />
      <FactoryRunModal />
      <CostBudgetModal />
    </div>
  );
};

export default App;

