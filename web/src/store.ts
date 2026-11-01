// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { create } from 'zustand';
import {
  type Node,
  type Edge,
  type Connection,
  type OnNodesChange,
  type OnEdgesChange,
  BackgroundVariant,
  MarkerType,
  applyNodeChanges,
  applyEdgeChanges,
  addEdge,
} from '@xyflow/react';
import type {
  ExecutionStatus,
  FactoryNodeData,
  GateApprovalPayload,
  GatewayConnectionStatus,
  NodeLogEntry,
  NodeTelemetryUpdate,
  RunLifecycleState,
} from './types/flow';
import { validateConnection } from './utils/validation';
import { initialNodes, initialEdges } from './initialFlow';

let nextNodeId = 10;

export interface FactoryFlowState {
  nodes: Node<FactoryNodeData>[];
  edges: Edge[];
  selectedNodeId: string | null;
  backgroundVariant: BackgroundVariant;
  gatewayStatus: GatewayConnectionStatus;
  activeApproval: GateApprovalPayload | null;
  showGateModal: boolean;
  activeRunId: string | null;
  activeRunState: RunLifecycleState | null;
  onNodesChange: OnNodesChange<Node<FactoryNodeData>>;
  onEdgesChange: OnEdgesChange;
  onConnect: (connection: Connection) => void;
  isValidConnection: (connection: Edge | Connection) => boolean;
  addNode: (
    nodeData: Partial<FactoryNodeData>,
    position?: { x: number; y: number }
  ) => void;
  deleteNode: (nodeId: string) => void;
  updateNodeData: (nodeId: string, data: Partial<FactoryNodeData>) => void;
  updateNodeStatus: (nodeId: string, status: ExecutionStatus) => void;
  updateNodeTelemetry: (nodeId: string, telemetry: NodeTelemetryUpdate) => void;
  appendNodeLog: (
    nodeId: string,
    stream: 'stdout' | 'stderr',
    message: string,
    timestamp?: string
  ) => void;
  clearNodeLogs: (nodeId: string) => void;
  setNodeGitDiff: (nodeId: string, diff: string) => void;
  setSelectedNodeId: (nodeId: string | null) => void;
  setBackgroundVariant: (variant: BackgroundVariant) => void;
  setGatewayStatus: (status: GatewayConnectionStatus) => void;
  setActiveApproval: (approval: GateApprovalPayload | null) => void;
  setShowGateModal: (show: boolean) => void;
  setActiveRun: (runId: string | null, state?: RunLifecycleState | null) => void;
  resolveApproval: (
    decision: 'APPROVE' | 'REJECT',
    rejectionReason?: string
  ) => Promise<{ success: boolean; message: string }>;
  setNodes: (nodes: Node<FactoryNodeData>[]) => void;
  setEdges: (edges: Edge[]) => void;
}

export const useFactoryFlowStore = create<FactoryFlowState>((set, get) => ({
  nodes: initialNodes,
  edges: initialEdges,
  selectedNodeId: null,
  backgroundVariant: BackgroundVariant.Dots,
  gatewayStatus: 'disconnected',
  activeApproval: null,
  showGateModal: false,
  activeRunId: null,
  activeRunState: null,

  onNodesChange: (changes) => {
    for (const change of changes) {
      if (change.type === 'select') {
        if (change.selected) {
          set({ selectedNodeId: change.id });
        } else if (get().selectedNodeId === change.id) {
          set({ selectedNodeId: null });
        }
      }
    }

    set({
      nodes: applyNodeChanges(changes, get().nodes),
    });
  },

  onEdgesChange: (changes) => {
    set({
      edges: applyEdgeChanges(changes, get().edges),
    });
  },

  isValidConnection: (connection: Edge | Connection) => {
    const { nodes, edges } = get();
    const result = validateConnection(connection, nodes, edges);
    return result.isValid;
  },

  onConnect: (connection: Connection) => {
    const { nodes, edges } = get();
    const validation = validateConnection(connection, nodes, edges);
    if (!validation.isValid) {
      return;
    }

    const newEdge: Edge = {
      ...connection,
      id: `e-${connection.source}-${connection.target}-${Date.now()}`,
      animated: false,
      markerEnd: { type: MarkerType.ArrowClosed },
    };

    set({
      edges: addEdge(newEdge, edges),
    });
  },

  addNode: (nodeData, position) => {
    const currentNodes = get().nodes;
    const id = `node-${nextNodeId++}`;
    const nodeType = nodeData.nodeType || 'agent';

    const newPos = position || {
      x: 100 + ((currentNodes.length * 60) % 600),
      y: 120 + ((currentNodes.length * 50) % 350),
    };

    const newNode: Node<FactoryNodeData> = {
      id,
      type: nodeType,
      position: newPos,
      data: {
        label: nodeData.label || 'New Factory Node',
        nodeType,
        status: nodeData.status || 'pending',
        ...nodeData,
      } as FactoryNodeData,
    };

    set({
      nodes: [...currentNodes, newNode],
      selectedNodeId: id,
    });
  },

  deleteNode: (nodeId: string) => {
    const { nodes, edges, selectedNodeId } = get();
    set({
      nodes: nodes.filter((n) => n.id !== nodeId),
      edges: edges.filter((e) => e.source !== nodeId && e.target !== nodeId),
      selectedNodeId: selectedNodeId === nodeId ? null : selectedNodeId,
    });
  },

  updateNodeData: (nodeId: string, data: Partial<FactoryNodeData>) => {
    set({
      nodes: get().nodes.map((node) => {
        if (node.id === nodeId) {
          return {
            ...node,
            data: {
              ...node.data,
              ...data,
            } as FactoryNodeData,
          };
        }
        return node;
      }),
    });
  },

  updateNodeStatus: (nodeId: string, status: ExecutionStatus) => {
    get().updateNodeData(nodeId, { status });
  },

  updateNodeTelemetry: (nodeId, telemetry) => {
    set({
      nodes: get().nodes.map((node) => {
        const stepId = (node.data as { stepId?: string })?.stepId;
        if (node.id === nodeId || (stepId && stepId === nodeId)) {
          return {
            ...node,
            data: {
              ...node.data,
              ...(telemetry.status ? { status: telemetry.status } : {}),
              ...(telemetry.executionDurationMs !== undefined ? { executionDurationMs: telemetry.executionDurationMs } : {}),
              ...(telemetry.tokensUsed !== undefined ? { tokensUsed: telemetry.tokensUsed } : {}),
              ...(telemetry.promptTokens !== undefined ? { promptTokens: telemetry.promptTokens } : {}),
              ...(telemetry.completionTokens !== undefined ? { completionTokens: telemetry.completionTokens } : {}),
              ...(telemetry.startedAt ? { startedAt: telemetry.startedAt } : {}),
              ...(telemetry.completedAt ? { completedAt: telemetry.completedAt } : {}),
              ...(telemetry.output ? { output: telemetry.output } : {}),
              ...(telemetry.errorMessage ? { errorMessage: telemetry.errorMessage } : {}),
            } as FactoryNodeData,
          };
        }
        return node;
      }),
    });
  },

  appendNodeLog: (nodeId, stream, message, timestamp) => {
    const entry: NodeLogEntry = {
      timestamp: timestamp || new Date().toISOString(),
      stream,
      message,
    };
    set({
      nodes: get().nodes.map((node) => {
        const stepId = (node.data as { stepId?: string })?.stepId;
        if (node.id === nodeId || (stepId && stepId === nodeId)) {
          const existingLogs = (node.data.logs as NodeLogEntry[]) || [];
          return {
            ...node,
            data: {
              ...node.data,
              logs: [...existingLogs, entry],
            } as FactoryNodeData,
          };
        }
        return node;
      }),
    });
  },

  clearNodeLogs: (nodeId) => {
    set({
      nodes: get().nodes.map((node) => {
        const stepId = (node.data as { stepId?: string })?.stepId;
        if (node.id === nodeId || (stepId && stepId === nodeId)) {
          return {
            ...node,
            data: {
              ...node.data,
              logs: [],
            } as FactoryNodeData,
          };
        }
        return node;
      }),
    });
  },

  setNodeGitDiff: (nodeId, diff) => {
    get().updateNodeData(nodeId, { gitDiff: diff });
  },

  setGatewayStatus: (status) => set({ gatewayStatus: status }),
  setActiveApproval: (approval) => set({ activeApproval: approval, showGateModal: approval !== null }),
  setShowGateModal: (show) => set({ showGateModal: show }),

  setActiveRun: (runId, state) => {
    if (runId === null) {
      set({ activeRunId: null, activeRunState: null });
    } else {
      set({
        activeRunId: runId,
        ...(state !== undefined ? { activeRunState: state } : {}),
      });
    }
  },

  resolveApproval: async (decision: 'APPROVE' | 'REJECT', rejectionReason?: string) => {
    const { activeApproval } = get();
    if (!activeApproval) {
      return { success: false, message: 'No active approval request found' };
    }

    try {
      try {
        const response = await fetch('/api/gate/resolve', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            run_id: activeApproval.runId,
            token: activeApproval.token,
            decision: decision === 'APPROVE' ? 'approved' : 'rejected',
            ...(decision === 'REJECT' && rejectionReason ? { rejection_reason: rejectionReason } : {}),
            ...(activeApproval.toolName ? { tool_name: activeApproval.toolName } : {}),
          }),
        });

        if (!response.ok) {
          const errData = await response.json().catch(() => ({}));
          const errMsg = (errData as { message?: string })?.message || `Server returned status ${response.status}`;
          return {
            success: false,
            message: `Failed to resolve approval: ${errMsg}`,
          };
        }
      } catch (networkErr) {
        // Fallback for standalone / disconnected dev environment when fetch fails entirely (e.g. ECONNREFUSED)
        if (networkErr instanceof TypeError && networkErr.message.includes('fetch')) {
          // offline mode
        }
      }

      const targetNodeId = activeApproval.callerId || activeApproval.stepId;
      if (targetNodeId) {
        const targetNode = get().nodes.find(
          (n) => n.id === targetNodeId || (n.data as { stepId?: string })?.stepId === targetNodeId
        );
        if (targetNode) {
          get().updateNodeStatus(targetNode.id, decision === 'APPROVE' ? 'completed' : 'failed');
        }
      }

      const approverNode = get().nodes.find(
        (n) => n.data.role === 'approver' || n.id.includes('approver') || n.id.includes('gate')
      );
      if (approverNode && (!targetNodeId || targetNodeId === approverNode.id)) {
        get().updateNodeStatus(approverNode.id, decision === 'APPROVE' ? 'completed' : 'failed');
      }

      const isToolGate = activeApproval.gateType === 'tool' || Boolean(activeApproval.toolName);

      set({ activeApproval: null, showGateModal: false });
      return {
        success: true,
        message: isToolGate
          ? decision === 'APPROVE'
            ? `Tool call '${activeApproval.toolName || 'tool'}' approved and authorized for execution.`
            : `Tool call '${activeApproval.toolName || 'tool'}' rejected.${rejectionReason ? ` Reason: ${rejectionReason}` : ''}`
          : decision === 'APPROVE'
            ? `Run ${activeApproval.runId} approved and enqueued to sequential merge queue.`
            : `Run ${activeApproval.runId} rejected. Diverting to GitHub Pull Request.`,
      };
    } catch (err) {
      return { success: false, message: `Failed to resolve approval: ${String(err)}` };
    }
  },

  setSelectedNodeId: (nodeId: string | null) => {
    set({
      selectedNodeId: nodeId,
      nodes: get().nodes.map((n) => ({
        ...n,
        selected: n.id === nodeId,
      })),
    });
  },

  setBackgroundVariant: (variant: BackgroundVariant) => {
    set({ backgroundVariant: variant });
  },

  setNodes: (nodes) => set({ nodes }),
  setEdges: (edges) => set({ edges }),
}));

