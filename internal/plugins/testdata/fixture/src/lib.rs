// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2026 gv-factory contributors

use extism_pdk::*;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[host_fn("extism:host/user")]
extern "ExtismHost" {
    fn brain_query_rules(input: String) -> String;
    fn log_event(input: String) -> String;
    fn workspace_read_file(input: String) -> String;
}

#[derive(Serialize, Deserialize, Debug)]
struct PdkContext {
    run_id: String,
    task_id: String,
    #[serde(default)]
    configs: HashMap<String, String>,
}

#[derive(Serialize, Deserialize, Debug)]
struct PdkResult {
    success: bool,
    output: String,
    #[serde(default)]
    data: HashMap<String, serde_json::Value>,
}

#[derive(Serialize, Deserialize)]
struct LogEventRequest {
    level: String,
    message: String,
}

#[derive(Serialize, Deserialize)]
struct WorkspaceFileRequest {
    run_id: String,
    path: String,
}

#[derive(Serialize, Deserialize)]
struct WorkspaceFileResponse {
    success: bool,
    #[serde(default)]
    content: String,
    #[serde(default)]
    error: String,
}

#[plugin_fn]
pub fn execute(input: String) -> FnResult<String> {
    let ctx: PdkContext = if input.is_empty() {
        PdkContext {
            run_id: "default-run".to_string(),
            task_id: "default-task".to_string(),
            configs: HashMap::new(),
        }
    } else {
        serde_json::from_str(&input)?
    };

    // 1. Call log_event host function
    let log_req = LogEventRequest {
        level: "info".to_string(),
        message: format!("Executing task {} for run {}", ctx.task_id, ctx.run_id),
    };
    let _ = unsafe { log_event(serde_json::to_string(&log_req)?)? };

    // 2. Call brain_query_rules host function
    let rules_json = unsafe { brain_query_rules(r#"{"limit": 5}"#.to_string())? };
    let rules: serde_json::Value = serde_json::from_str(&rules_json).unwrap_or(serde_json::json!([]));
    let rules_count = rules.as_array().map(|a| a.len()).unwrap_or(0);

    // 3. Call workspace_read_file host function
    let target_file = ctx.configs.get("target_file").cloned().unwrap_or_else(|| "task.txt".to_string());
    let ws_req = WorkspaceFileRequest {
        run_id: ctx.run_id.clone(),
        path: target_file,
    };
    let file_resp_str = unsafe { workspace_read_file(serde_json::to_string(&ws_req)?)? };
    let file_resp: WorkspaceFileResponse = serde_json::from_str(&file_resp_str).unwrap_or(WorkspaceFileResponse {
        success: false,
        content: String::new(),
        error: "failed to parse response".to_string(),
    });

    // 4. Build pdk.Result response
    let mut data = HashMap::new();
    data.insert("rules_count".to_string(), serde_json::json!(rules_count));
    data.insert("file_read_success".to_string(), serde_json::json!(file_resp.success));
    data.insert("file_content".to_string(), serde_json::json!(file_resp.content));
    data.insert("run_id".to_string(), serde_json::json!(ctx.run_id));
    data.insert("task_id".to_string(), serde_json::json!(ctx.task_id));

    let res = PdkResult {
        success: true,
        output: format!("Execution completed successfully for task {}", ctx.task_id),
        data,
    };

    Ok(serde_json::to_string(&res)?)
}

#[plugin_fn]
pub fn trap_test(_input: String) -> FnResult<String> {
    panic!("intentional guest panic for fault containment verification");
}
