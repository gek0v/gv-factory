// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

export function execute(): number {
  const inputStr = Host.inputString();
  const input = JSON.parse(inputStr || '{}');

  const output = {
    success: true,
    run_id: input.run_id,
    summary: `TS Plugin executed task ${input.task_id}`,
  };

  Host.outputString(JSON.stringify(output));
  return 0;
}
