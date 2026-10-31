// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

use extism_pdk::*;
use serde::{Deserialize, Serialize};

#[derive(Serialize, Deserialize)]
struct PluginInput {
    run_id: String,
    task_id: String,
}

#[derive(Serialize, Deserialize)]
struct PluginOutput {
    success: bool,
    summary: String,
}

#[plugin_fn]
pub fn execute(input: String) -> FnResult<String> {
    let req: PluginInput = serde_json::from_str(&input)?;
    let res = PluginOutput {
        success: true,
        summary: format!("Executed task {} for run {}", req.task_id, req.run_id),
    };
    Ok(serde_json::to_string(&res)?)
}
