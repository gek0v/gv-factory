// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import { create } from 'zustand';
import { type Node, type Edge, applyNodeChanges, applyEdgeChanges, type OnNodesChange, type OnEdgesChange } from '@xyflow/react';

interface FactoryFlowState {
  nodes: Node[];
  edges: Edge[];
  onNodesChange: OnNodesChange;
  onEdgesChange: OnEdgesChange;
  setNodes: (nodes: Node[]) => void;
  setEdges: (edges: Edge[]) => void;
}

const initialNodes: Node[] = [
  {
    id: 'ingestion',
    type: 'input',
    data: { label: 'Ingest (Issue / Webhook)' },
    position: { x: 50, y: 150 },
  },
  {
    id: 'spec-writer',
    data: { label: 'Spec Writer (SDD & TDD)' },
    position: { x: 280, y: 150 },
  },
  {
    id: 'builder-tdd',
    data: { label: 'Builder TDD (Sandbox)' },
    position: { x: 520, y: 150 },
  },
  {
    id: 'reviewers',
    data: { label: '4x Parallel Reviewers' },
    position: { x: 760, y: 150 },
  },
  {
    id: 'approver',
    data: { label: 'Approver & Gate' },
    position: { x: 1000, y: 150 },
  },
];

const initialEdges: Edge[] = [
  { id: 'e1-2', source: 'ingestion', target: 'spec-writer' },
  { id: 'e2-3', source: 'spec-writer', target: 'builder-tdd' },
  { id: 'e3-4', source: 'builder-tdd', target: 'reviewers' },
  { id: 'e4-5', source: 'reviewers', target: 'approver' },
];

export const useFactoryFlowStore = create<FactoryFlowState>((set, get) => ({
  nodes: initialNodes,
  edges: initialEdges,
  onNodesChange: (changes) => {
    set({
      nodes: applyNodeChanges(changes, get().nodes),
    });
  },
  onEdgesChange: (changes) => {
    set({
      edges: applyEdgeChanges(changes, get().edges),
    });
  },
  setNodes: (nodes) => set({ nodes }),
  setEdges: (edges) => set({ edges }),
}));
