// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';
import type { NodeLogEntry } from '../../types/flow';

interface LogStreamViewerProps {
  logs?: NodeLogEntry[];
  onClear?: () => void;
}

export const LogStreamViewer: React.FC<LogStreamViewerProps> = ({ logs = [], onClear }) => {
  if (logs.length === 0) {
    return (
      <div className="logs-empty-state">
        <p>No stdout/stderr telemetry received yet.</p>
        <span className="logs-empty-hint">Logs will stream in real-time when the WASM plugin executes.</span>
      </div>
    );
  }

  return (
    <div className="log-stream-wrapper">
      <div className="log-stream-header">
        <span className="log-count">{logs.length} lines</span>
        {onClear && (
          <button type="button" className="log-clear-btn" onClick={onClear}>
            Clear
          </button>
        )}
      </div>

      <div className="log-stream-console">
        {logs.map((entry, idx) => (
          <div key={idx} className={`log-entry log-${entry.stream}`}>
            <span className="log-idx">{idx + 1}</span>
            <span className={`log-stream-tag stream-${entry.stream}`}>
              {entry.stream.toUpperCase()}
            </span>
            <span className="log-message">{entry.message}</span>
          </div>
        ))}
      </div>
    </div>
  );
};
