-- 000098_search_trigram_indexes.sql
-- Trigram indexes for the workspace search (GET /search).
--
-- The search matches with ILIKE '%…%' (task titles, project names, people's
-- display names and usernames), which a B-tree index cannot serve, so on a
-- large workspace every keystroke would read the whole table. A GIN trigram
-- index serves ILIKE '%…%' for queries of three or more characters; shorter
-- ones still scan, but are also bounded by the search's small LIMIT. The task
-- title index also serves the in-project task search (title ILIKE in
-- TaskRepository's filter), which matches the same way.
--
-- pg_trgm was installed by 000091; IF NOT EXISTS keeps this file safe to run
-- on its own.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_tasks_title_trgm
    ON tasks USING gin (title gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_projects_name_trgm
    ON projects USING gin (name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_accounts_name_trgm
    ON accounts USING gin (name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_users_username_trgm
    ON users USING gin (username gin_trgm_ops);
