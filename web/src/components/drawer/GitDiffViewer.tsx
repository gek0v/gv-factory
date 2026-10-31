// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

import React from 'react';

interface GitDiffViewerProps {
  diff: string;
}

export const GitDiffViewer: React.FC<GitDiffViewerProps> = ({ diff }) => {
  if (!diff || diff.trim().length === 0) {
    return (
      <div className="diff-empty-state">
        <p>No Git diff available for this step execution.</p>
        <span className="diff-empty-hint">Builder & Reviewer steps generate unified diffs upon file modifications.</span>
      </div>
    );
  }

  const lines = diff.split('\n');

  return (
    <div className="diff-container" aria-label="Git Diff Viewer">
      <div className="diff-code-block">
        {lines.map((line, idx) => {
          let lineType = 'context';
          if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('diff ') || line.startsWith('index ')) {
            lineType = 'header';
          } else if (line.startsWith('+')) {
            lineType = 'addition';
          } else if (line.startsWith('-')) {
            lineType = 'deletion';
          } else if (line.startsWith('@@')) {
            lineType = 'hunk';
          }

          return (
            <div key={idx} className={`diff-line diff-line-${lineType}`}>
              <span className="diff-line-number">{idx + 1}</span>
              <span className="diff-line-content">{line || ' '}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
};
