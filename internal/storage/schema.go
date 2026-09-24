package storage

const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS folders (
    folder_id TEXT PRIMARY KEY,
    parent_id TEXT REFERENCES folders(folder_id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    current_path TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('managed', 'missing', 'unregistered')),
    locked INTEGER NOT NULL DEFAULT 0 CHECK (locked IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
    file_id TEXT PRIMARY KEY,
    current_folder_id TEXT REFERENCES folders(folder_id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    current_path TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL,
    size INTEGER NOT NULL CHECK (size >= 0),
    mime TEXT NOT NULL DEFAULT '',
    modified_at TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'missing', 'pending')),
    extract_status TEXT NOT NULL,
    extract_error TEXT NOT NULL DEFAULT '',
    observed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS file_versions (
    version_id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    sha256 TEXT NOT NULL,
    size INTEGER NOT NULL CHECK (size >= 0),
    modified_at TEXT NOT NULL,
    observed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS extracted_chunks (
    chunk_id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    version_id TEXT NOT NULL REFERENCES file_versions(version_id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    text TEXT NOT NULL,
    filetext_version TEXT NOT NULL,
    UNIQUE(version_id, ordinal)
);
CREATE TABLE IF NOT EXISTS file_tags (
    file_id TEXT NOT NULL REFERENCES files(file_id) ON DELETE CASCADE,
    tag TEXT NOT NULL,
    PRIMARY KEY(file_id, tag)
);

CREATE TABLE IF NOT EXISTS proposals (
    proposal_id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    from_id TEXT NOT NULL DEFAULT '',
    to_id TEXT NOT NULL DEFAULT '',
    score REAL NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 1),
    reason TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('pending', 'accepted', 'rejected', 'expired', 'applied', 'failed')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS decisions (
    decision_id TEXT PRIMARY KEY,
    proposal_id TEXT NOT NULL REFERENCES proposals(proposal_id) ON DELETE CASCADE,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    decided_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    event_id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    target_id TEXT NOT NULL DEFAULT '',
    payload TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL,
    correlation_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_rules (
    rule_id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    condition TEXT NOT NULL,
    action TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS model_runs (
    run_id TEXT PRIMARY KEY,
    task TEXT NOT NULL,
    model_version TEXT NOT NULL,
    input_refs TEXT NOT NULL DEFAULT '[]',
    scores TEXT NOT NULL DEFAULT '{}',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ingest_jobs (
    job_id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    source_size INTEGER NOT NULL DEFAULT 0,
    source_mtime TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL CHECK (kind IN ('scan', 'file')),
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    finished_at TEXT,
    file_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    UNIQUE(kind, path)
);
CREATE INDEX IF NOT EXISTS idx_ingest_jobs_claim ON ingest_jobs(kind, status, available_at, created_at);
CREATE TABLE IF NOT EXISTS queue_lease (
    name TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE IF NOT EXISTS file_search USING fts5(
    file_id UNINDEXED,
    name,
    path,
    body,
    tags,
    folder_description,
    raw_name UNINDEXED,
    raw_path UNINDEXED,
    raw_body UNINDEXED,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE INDEX IF NOT EXISTS idx_files_folder ON files(current_folder_id);
CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files(sha256);
CREATE INDEX IF NOT EXISTS idx_versions_file ON file_versions(file_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_chunks_file ON extracted_chunks(file_id, version_id, ordinal);
CREATE INDEX IF NOT EXISTS idx_proposals_state ON proposals(state, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_runs_file_folder_recent ON model_runs(
    task, json_extract(input_refs, '$[0]'), json_extract(input_refs, '$[1]'), created_at DESC, run_id DESC
);
CREATE INDEX IF NOT EXISTS idx_events_target ON events(target_id, created_at DESC);
`
