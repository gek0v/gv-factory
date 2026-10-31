-- SPDX-License-Identifier: Apache-2.0
-- SPDX-FileCopyrightText: 2026 gv-factory contributors

-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Table 1: Architectural Rules & Invariants
CREATE TABLE IF NOT EXISTS architectural_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_code VARCHAR(64) UNIQUE NOT NULL,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    severity VARCHAR(32) NOT NULL DEFAULT 'ERROR',
    embedding vector(1536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Table 2: Codebase Knowledge (AST symbols, contracts, signatures)
CREATE TABLE IF NOT EXISTS codebase_knowledge (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_path TEXT NOT NULL,
    symbol_name VARCHAR(255) NOT NULL,
    symbol_type VARCHAR(64) NOT NULL,
    content TEXT NOT NULL,
    content_hash VARCHAR(64) NOT NULL,
    embedding vector(1536),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Table 3: Task Episodes (Past learnings, review rejections, solutions)
CREATE TABLE IF NOT EXISTS task_episodes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id VARCHAR(128) NOT NULL,
    task_name VARCHAR(255) NOT NULL,
    outcome VARCHAR(64) NOT NULL,
    summary TEXT NOT NULL,
    lessons_learned TEXT,
    embedding vector(1536),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- HNSW Indexes for fast cosine distance vector search
CREATE INDEX IF NOT EXISTS idx_architectural_rules_hnsw 
ON architectural_rules USING hnsw (embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS idx_codebase_knowledge_hnsw 
ON codebase_knowledge USING hnsw (embedding vector_cosine_ops);

CREATE UNIQUE INDEX IF NOT EXISTS idx_codebase_knowledge_file_symbol
ON codebase_knowledge (file_path, symbol_name);

CREATE INDEX IF NOT EXISTS idx_task_episodes_hnsw 
ON task_episodes USING hnsw (embedding vector_cosine_ops);
