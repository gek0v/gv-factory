-- SPDX-License-Identifier: Apache-2.0
-- SPDX-FileCopyrightText: 2026 gv-factory contributors

-- Tabla de control de migraciones
CREATE TABLE IF NOT EXISTS schema_migrations (
    version VARCHAR(255) PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Tabla de ejecuciones (runs)
CREATE TABLE IF NOT EXISTS runs (
    id VARCHAR(128) PRIMARY KEY,
    template_name VARCHAR(255) NOT NULL,
    status VARCHAR(64) NOT NULL DEFAULT 'QUEUED',
    initiated_by VARCHAR(255) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    error_message TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status);
CREATE INDEX IF NOT EXISTS idx_runs_template ON runs(template_name);
CREATE INDEX IF NOT EXISTS idx_runs_created_at ON runs(created_at DESC);

-- Tabla de pasos de ejecución (run_steps)
CREATE TABLE IF NOT EXISTS run_steps (
    id VARCHAR(128) PRIMARY KEY,
    run_id VARCHAR(128) NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    step_name VARCHAR(255) NOT NULL,
    status VARCHAR(64) NOT NULL DEFAULT 'PENDING',
    duration_ms BIGINT NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    output JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_run_steps_run_id ON run_steps(run_id);

-- Tabla de compuertas pendientes (pending_gates)
CREATE TABLE IF NOT EXISTS pending_gates (
    id VARCHAR(128) PRIMARY KEY,
    run_id VARCHAR(128) NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    token_hash VARCHAR(64) NOT NULL UNIQUE,
    gate_type VARCHAR(64) NOT NULL DEFAULT 'HUMAN_APPROVAL',
    tool_name VARCHAR(255) NOT NULL DEFAULT '',
    arguments JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(64) NOT NULL DEFAULT 'PENDING',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_pending_gates_run_id ON pending_gates(run_id);
CREATE INDEX IF NOT EXISTS idx_pending_gates_status_exp ON pending_gates(status, expires_at);

-- Tabla inmutable de auditoría de compuertas (gate_audits)
CREATE TABLE IF NOT EXISTS gate_audits (
    id BIGSERIAL PRIMARY KEY,
    run_id VARCHAR(128) NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    gate_type VARCHAR(64) NOT NULL,
    action VARCHAR(64) NOT NULL,
    actor VARCHAR(255) NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gate_audits_run_id ON gate_audits(run_id);
CREATE INDEX IF NOT EXISTS idx_gate_audits_token_hash ON gate_audits(token_hash);
CREATE INDEX IF NOT EXISTS idx_gate_audits_created_at ON gate_audits(created_at DESC);
