package postgres

import (
	"fmt"
	"time"
)

// migrations PG 方言迁移（与 sqlite 迁移 id 一一对应；全新部署）
var migrations = []struct {
	id     int
	script string
}{
	{
		id: 1,
		script: `
CREATE TABLE IF NOT EXISTS users (
    id                   TEXT PRIMARY KEY,
    username             TEXT NOT NULL UNIQUE,
    password_hash        TEXT NOT NULL,
    role                 TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'active',
    must_change_password INTEGER NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL,
    updated_at           TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS refresh_sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_refresh_sessions_user ON refresh_sessions(user_id);

CREATE TABLE IF NOT EXISTS audit_logs (
    id         TEXT PRIMARY KEY,
    user_id    TEXT,
    username   TEXT NOT NULL DEFAULT '',
    action     TEXT NOT NULL,
    resource   TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '',
    ip         TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC);
`,
	},
	{
		id: 2,
		script: `
CREATE TABLE IF NOT EXISTS provider_accounts (
    id             TEXT PRIMARY KEY,
    provider       TEXT NOT NULL,
    name           TEXT NOT NULL,
    directory_url  TEXT NOT NULL DEFAULT '',
    contact        TEXT NOT NULL DEFAULT '',
    eab_kid        TEXT NOT NULL DEFAULT '',
    eab_hmac_enc   TEXT NOT NULL DEFAULT '',
    credentials_enc TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'active',
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS certificates (
    id             TEXT PRIMARY KEY,
    domains        TEXT NOT NULL,
    primary_domain TEXT NOT NULL,
    provider       TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending',
    issuer_ca      TEXT NOT NULL DEFAULT '',
    not_before     TIMESTAMPTZ,
    not_after      TIMESTAMPTZ,
    certificate_pem TEXT NOT NULL DEFAULT '',
    private_key_enc TEXT NOT NULL DEFAULT '',
    account_id     TEXT NOT NULL DEFAULT '',
    verification   TEXT NOT NULL DEFAULT '',
    last_error     TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_certificates_status ON certificates(status);
CREATE INDEX IF NOT EXISTS idx_certificates_not_after ON certificates(not_after);

CREATE TABLE IF NOT EXISTS orders (
    id             TEXT PRIMARY KEY,
    certificate_id TEXT NOT NULL REFERENCES certificates(id) ON DELETE CASCADE,
    type           TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'running',
    error          TEXT NOT NULL DEFAULT '',
    attempts       INTEGER NOT NULL DEFAULT 0,
    started_at     TIMESTAMPTZ NOT NULL,
    finished_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_orders_certificate ON orders(certificate_id);
`,
	},
	{
		id: 3,
		script: `
ALTER TABLE certificates ADD COLUMN dns_config_enc TEXT NOT NULL DEFAULT '';
`,
	},
	{
		id: 4,
		script: `
CREATE TABLE IF NOT EXISTS notification_rules (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    event       TEXT NOT NULL,
    channels    TEXT NOT NULL,
    days_before TEXT NOT NULL DEFAULT '[]',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS notification_logs (
    id          TEXT PRIMARY KEY,
    cert_id     TEXT NOT NULL,
    rule_id     TEXT NOT NULL,
    days_before INTEGER NOT NULL DEFAULT 0,
    sent_at     TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_notify_logs_dedupe ON notification_logs(cert_id, rule_id, days_before);

CREATE TABLE IF NOT EXISTS task_logs (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'running',
    detail      TEXT NOT NULL DEFAULT '',
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_task_logs_started ON task_logs(started_at DESC);
`,
	},
	{
		id: 5,
		script: `
ALTER TABLE certificates ADD COLUMN deploy_target_ids TEXT NOT NULL DEFAULT '[]';

CREATE TABLE IF NOT EXISTS deploy_targets (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    config_enc  TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    last_status TEXT NOT NULL DEFAULT '',
    last_error  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS deploy_records (
    id             TEXT PRIMARY KEY,
    target_id      TEXT NOT NULL,
    certificate_id TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'running',
    checksum       TEXT NOT NULL DEFAULT '',
    message        TEXT NOT NULL DEFAULT '',
    retries        INTEGER NOT NULL DEFAULT 0,
    deployed_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_deploy_records_cert ON deploy_records(certificate_id);
CREATE INDEX IF NOT EXISTS idx_deploy_records_failed ON deploy_records(status);
`,
	},
	{
		id: 6,
		script: `
CREATE TABLE IF NOT EXISTS api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    scopes       TEXT NOT NULL DEFAULT '["all"]',
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_tokens_user ON api_tokens(user_id);
`,
	},
	{
		id: 7,
		script: `
CREATE TABLE IF NOT EXISTS agents (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    token_hash    TEXT NOT NULL UNIQUE,
    status        TEXT NOT NULL DEFAULT 'offline',
    last_seen_at  TIMESTAMPTZ,
    version       TEXT NOT NULL DEFAULT '',
    os            TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_tasks (
    id             TEXT PRIMARY KEY,
    agent_id       TEXT NOT NULL,
    certificate_id TEXT NOT NULL,
    target_id      TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pending',
    checksum       TEXT NOT NULL DEFAULT '',
    message        TEXT NOT NULL DEFAULT '',
    claimed_at     TIMESTAMPTZ,
    finished_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_tasks_status ON agent_tasks(status);
CREATE INDEX IF NOT EXISTS idx_agent_tasks_agent ON agent_tasks(agent_id);
`,
	},
	{
		id: 8,
		script: `
ALTER TABLE agents ADD COLUMN upgrade_version TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN upgrade_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN upgrade_requested_at TIMESTAMPTZ;
`,
	},
	{
		id: 9,
		script: `
ALTER TABLE agents ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;
`,
	},
	{
		id: 10,
		script: `
CREATE TABLE IF NOT EXISTS notification_channels (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL,
    config_enc  TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

ALTER TABLE notification_rules ADD COLUMN channel_ids TEXT NOT NULL DEFAULT '[]';
`,
	},
	{
		id: 11,
		script: `
CREATE TABLE IF NOT EXISTS notification_send_logs (
    id           TEXT PRIMARY KEY,
    cert_id      TEXT NOT NULL DEFAULT '',
    rule_id      TEXT NOT NULL DEFAULT '',
    rule_name    TEXT NOT NULL DEFAULT '',
    channel_id   TEXT NOT NULL DEFAULT '',
    channel_name TEXT NOT NULL DEFAULT '',
    event        TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'success',
    detail       TEXT NOT NULL DEFAULT '',
    sent_at      TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_send_logs_sent_at ON notification_send_logs(sent_at);
CREATE INDEX IF NOT EXISTS idx_send_logs_channel ON notification_send_logs(channel_id);
CREATE INDEX IF NOT EXISTS idx_send_logs_status ON notification_send_logs(status);
`,
	},
	{
		id: 12,
		script: `
CREATE TABLE IF NOT EXISTS notification_templates (
    event      TEXT PRIMARY KEY,
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL
);
`,
	},
	{
		id: 13,
		script: `
CREATE TABLE IF NOT EXISTS contact_persons (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    phone      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
`,
	},
	{
		id: 14,
		script: `
ALTER TABLE certificates ADD COLUMN contact_name TEXT NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN contact_email TEXT NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN contact_phone TEXT NOT NULL DEFAULT '';
`,
	},
	{
		id: 15,
		script: `
ALTER TABLE certificates ADD COLUMN notify_events TEXT NOT NULL DEFAULT '[]';
`,
	},
	{
		id: 16,
		script: `
CREATE TABLE IF NOT EXISTS dns_provider_accounts (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    provider        TEXT NOT NULL,
    credentials_enc TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'active',
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);
ALTER TABLE certificates ADD COLUMN dns_provider_account_id TEXT NOT NULL DEFAULT '';
`,
	},
	{
		id: 17,
		script: `
ALTER TABLE agent_tasks ADD COLUMN certificate_domain TEXT NOT NULL DEFAULT '';
`,
	},
	{
		id: 18,
		script: `
ALTER TABLE task_logs ADD COLUMN summary TEXT NOT NULL DEFAULT '';
`,
	},
}

func (s *DB) migrate() error {
	if _, err := s.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        id INTEGER PRIMARY KEY,
        applied_at TIMESTAMPTZ NOT NULL
    )`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	for _, m := range migrations {
		var exists int
		err := s.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE id = $1`, m.id).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", m.id, err)
		}
		if exists > 0 {
			continue
		}
		tx, err := s.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.script); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", m.id, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (id, applied_at) VALUES ($1, $2)`, m.id, time.Now().UTC()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.id, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
