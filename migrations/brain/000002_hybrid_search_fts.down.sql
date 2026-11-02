-- SPDX-License-Identifier: Apache-2.0
-- SPDX-FileCopyrightText: 2026 gv-factory contributors

-- Drop GIN indexes and tsvector generated columns for hybrid search
DROP INDEX IF EXISTS idx_architectural_rules_fts;
ALTER TABLE architectural_rules DROP COLUMN IF EXISTS fts_doc_en;

DROP INDEX IF EXISTS idx_codebase_knowledge_fts;
ALTER TABLE codebase_knowledge DROP COLUMN IF EXISTS fts_doc_en;
