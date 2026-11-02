-- SPDX-License-Identifier: Apache-2.0
-- SPDX-FileCopyrightText: 2026 gv-factory contributors

-- Add tsvector generated column and GIN index for codebase_knowledge lexical search
ALTER TABLE codebase_knowledge 
ADD COLUMN IF NOT EXISTS fts_doc_en tsvector 
GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(symbol_name, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(file_path, '')), 'B') ||
    setweight(to_tsvector('english', coalesce(content, '')), 'C')
) STORED;

CREATE INDEX IF NOT EXISTS idx_codebase_knowledge_fts 
ON codebase_knowledge USING gin(fts_doc_en);

-- Add tsvector generated column and GIN index for architectural_rules lexical search
ALTER TABLE architectural_rules 
ADD COLUMN IF NOT EXISTS fts_doc_en tsvector 
GENERATED ALWAYS AS (
    setweight(to_tsvector('english', coalesce(rule_code, '')), 'A') ||
    setweight(to_tsvector('english', coalesce(title, '')), 'B') ||
    setweight(to_tsvector('english', coalesce(description, '')), 'C')
) STORED;

CREATE INDEX IF NOT EXISTS idx_architectural_rules_fts 
ON architectural_rules USING gin(fts_doc_en);
