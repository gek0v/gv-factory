// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import type { NodeLogEntry } from '../../types/flow';
import { LogViewer } from '../logs/LogViewer';

export interface LogStreamViewerProps {
  logs?: NodeLogEntry[];
  onClear?: () => void;
}

export const LogStreamViewer: React.FC<LogStreamViewerProps> = ({ logs = [], onClear }) => {
  return <LogViewer logs={logs} onClear={onClear} />;
};
