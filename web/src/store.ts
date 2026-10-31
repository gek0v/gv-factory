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
import type { ExecutionStatus, FactoryNodeData } from './types/flow';
import { validateConnection } from './utils/validation';
import { initialNodes, initialEdges } from './initialFlow';

let nextNodeId = 10;

export interface FactoryFlowState {
  nodes: Node<FactoryNodeData>[];
  edges: Edge[];
  selectedNodeId: string | null;
  backgroundVariant: BackgroundVariant;
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
  setSelectedNodeId: (nodeId: string | null) => void;
  setBackgroundVariant: (variant: BackgroundVariant) => void;
  setNodes: (nodes: Node<FactoryNodeData>[]) => void;
  setEdges: (edges: Edge[]) => void;
}

export const useFactoryFlowStore = create<FactoryFlowState>((set, get) => ({
  nodes: initialNodes,
  edges: initialEdges,
  selectedNodeId: null,
  backgroundVariant: BackgroundVariant.Dots,

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

