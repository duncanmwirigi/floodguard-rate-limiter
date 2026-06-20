-- Append-only ledger table with hash chaining.
-- Application DB role must NOT have UPDATE or DELETE on this table.

CREATE TABLE IF NOT EXISTS ledger_entries (
    id            BIGSERIAL PRIMARY KEY,
    account_id    TEXT NOT NULL,
    amount        BIGINT NOT NULL,
    tx_type       TEXT NOT NULL CHECK (tx_type IN ('deposit', 'withdrawal', 'bet_settlement')),
    actor_id      TEXT NOT NULL,
    source        TEXT NOT NULL,
    prev_hash     TEXT NOT NULL,
    record_hash   TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_account ON ledger_entries (account_id, id);

-- Revoke mutation from application role (replace app_user with your role name).
-- REVOKE UPDATE, DELETE ON ledger_entries FROM app_user;
-- GRANT INSERT, SELECT ON ledger_entries TO app_user;

COMMENT ON TABLE ledger_entries IS 'Append-only hash-chained audit log; tampering breaks record_hash chain';
