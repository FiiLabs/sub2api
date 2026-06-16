CREATE TABLE IF NOT EXISTS billing_subjects (
    id BIGSERIAL PRIMARY KEY,
    type VARCHAR(20) NOT NULL CHECK (type IN ('user', 'team')),
    user_id BIGINT NULL REFERENCES users(id),
    team_id BIGINT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    balance DECIMAL(20,8) NOT NULL DEFAULT 0,
    total_recharged DECIMAL(20,8) NOT NULL DEFAULT 0,
    concurrency INTEGER NOT NULL DEFAULT 5,
    rpm_limit INTEGER NOT NULL DEFAULT 0,
    balance_notify_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    balance_notify_threshold_type VARCHAR(20) NOT NULL DEFAULT 'fixed',
    balance_notify_threshold DECIMAL(20,8) NULL,
    balance_notify_extra_emails TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL,
    CONSTRAINT billing_subjects_owner_check CHECK (
        (type = 'user' AND user_id IS NOT NULL AND team_id IS NULL)
        OR (type = 'team' AND team_id IS NOT NULL AND user_id IS NULL)
    )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_subjects_user_unique
    ON billing_subjects(user_id)
    WHERE type = 'user' AND deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_billing_subjects_team_unique
    ON billing_subjects(team_id)
    WHERE type = 'team' AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS teams (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(120) NOT NULL,
    slug VARCHAR(120) NOT NULL,
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    billing_subject_id BIGINT NULL REFERENCES billing_subjects(id),
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    avatar_url TEXT NOT NULL DEFAULT '',
    created_by_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_slug_unique
    ON teams(slug)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_teams_owner_user_id ON teams(owner_user_id);
CREATE INDEX IF NOT EXISTS idx_teams_billing_subject_id ON teams(billing_subject_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'billing_subjects_team_fk'
          AND conrelid = 'billing_subjects'::regclass
    ) THEN
        ALTER TABLE billing_subjects
            ADD CONSTRAINT billing_subjects_team_fk
            FOREIGN KEY (team_id) REFERENCES teams(id) DEFERRABLE INITIALLY DEFERRED;
    END IF;
END $$;

CREATE OR REPLACE FUNCTION enforce_team_billing_subject_invariant()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.billing_subject_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM billing_subjects bs
        WHERE bs.id = NEW.billing_subject_id
          AND bs.type = 'team'
          AND bs.team_id = NEW.id
          AND bs.deleted_at IS NULL
    ) THEN
        RAISE EXCEPTION 'team billing_subject_id must reference an active team billing subject for the same team';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS teams_billing_subject_invariant ON teams;

CREATE TRIGGER teams_billing_subject_invariant
    BEFORE INSERT OR UPDATE OF billing_subject_id ON teams
    FOR EACH ROW
    EXECUTE FUNCTION enforce_team_billing_subject_invariant();

CREATE OR REPLACE FUNCTION prevent_referenced_billing_subject_invalid_state()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM teams
        WHERE teams.billing_subject_id = OLD.id
          AND teams.deleted_at IS NULL
    ) THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'cannot delete billing subject referenced by a team';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM teams
        WHERE teams.billing_subject_id = OLD.id
          AND teams.deleted_at IS NULL
          AND (
              NEW.type <> 'team'
              OR NEW.team_id <> teams.id
              OR NEW.deleted_at IS NOT NULL
          )
    ) THEN
        RAISE EXCEPTION 'cannot invalidate billing subject referenced by a team';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS billing_subjects_referenced_invariant ON billing_subjects;

CREATE TRIGGER billing_subjects_referenced_invariant
    BEFORE UPDATE OF type, team_id, deleted_at OR DELETE ON billing_subjects
    FOR EACH ROW
    EXECUTE FUNCTION prevent_referenced_billing_subject_invalid_state();

CREATE TABLE IF NOT EXISTS team_members (
    id BIGSERIAL PRIMARY KEY,
    team_id BIGINT NOT NULL REFERENCES teams(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    role VARCHAR(20) NOT NULL CHECK (role IN ('owner', 'admin', 'billing', 'developer', 'viewer')),
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'left')),
    invited_by_user_id BIGINT NULL REFERENCES users(id),
    joined_at TIMESTAMPTZ NULL,
    last_active_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_team_members_team_user_unique
    ON team_members(team_id, user_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_team_members_user_status ON team_members(user_id, status);
CREATE INDEX IF NOT EXISTS idx_team_members_team_status ON team_members(team_id, status);

CREATE TABLE IF NOT EXISTS team_invitations (
    id BIGSERIAL PRIMARY KEY,
    team_id BIGINT NOT NULL REFERENCES teams(id),
    email VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL CHECK (role IN ('admin', 'billing', 'developer', 'viewer')),
    token_hash VARCHAR(128) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'expired', 'revoked')),
    invited_by_user_id BIGINT NOT NULL REFERENCES users(id),
    accepted_by_user_id BIGINT NULL REFERENCES users(id),
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_team_invitations_token_hash_unique
    ON team_invitations(token_hash)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_team_invitations_team_status ON team_invitations(team_id, status);
CREATE INDEX IF NOT EXISTS idx_team_invitations_email_status ON team_invitations(email, status);

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT NULL REFERENCES billing_subjects(id);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS team_id BIGINT NULL REFERENCES teams(id);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS created_by_user_id BIGINT NULL REFERENCES users(id);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS updated_by_user_id BIGINT NULL REFERENCES users(id);

ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT NULL REFERENCES billing_subjects(id);
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS team_id BIGINT NULL REFERENCES teams(id);
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS actor_user_id BIGINT NULL REFERENCES users(id);

ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT NULL REFERENCES billing_subjects(id);
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS team_id BIGINT NULL REFERENCES teams(id);
ALTER TABLE payment_orders ADD COLUMN IF NOT EXISTS created_by_user_id BIGINT NULL REFERENCES users(id);

ALTER TABLE user_subscriptions ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT NULL REFERENCES billing_subjects(id);
ALTER TABLE user_platform_quotas ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT NULL REFERENCES billing_subjects(id);

INSERT INTO billing_subjects (
    type, user_id, status, balance, total_recharged, concurrency, rpm_limit,
    balance_notify_enabled, balance_notify_threshold_type, balance_notify_threshold,
    balance_notify_extra_emails, created_at, updated_at
)
SELECT
    'user', u.id, u.status, u.balance, u.total_recharged, u.concurrency, u.rpm_limit,
    u.balance_notify_enabled, u.balance_notify_threshold_type, u.balance_notify_threshold,
    u.balance_notify_extra_emails, u.created_at, NOW()
FROM users u
WHERE u.deleted_at IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM billing_subjects bs
      WHERE bs.type = 'user' AND bs.user_id = u.id AND bs.deleted_at IS NULL
  );

UPDATE api_keys ak
SET billing_subject_id = bs.id,
    created_by_user_id = COALESCE(ak.created_by_user_id, ak.user_id)
FROM billing_subjects bs
WHERE bs.type = 'user'
  AND bs.user_id = ak.user_id
  AND bs.deleted_at IS NULL
  AND ak.billing_subject_id IS NULL;

UPDATE usage_logs ul
SET billing_subject_id = bs.id,
    actor_user_id = COALESCE(ul.actor_user_id, ul.user_id)
FROM billing_subjects bs
WHERE bs.type = 'user'
  AND bs.user_id = ul.user_id
  AND bs.deleted_at IS NULL
  AND ul.billing_subject_id IS NULL;

UPDATE payment_orders po
SET billing_subject_id = bs.id,
    created_by_user_id = COALESCE(po.created_by_user_id, po.user_id)
FROM billing_subjects bs
WHERE bs.type = 'user'
  AND bs.user_id = po.user_id
  AND bs.deleted_at IS NULL
  AND po.billing_subject_id IS NULL;

UPDATE user_subscriptions us
SET billing_subject_id = bs.id
FROM billing_subjects bs
WHERE bs.type = 'user'
  AND bs.user_id = us.user_id
  AND bs.deleted_at IS NULL
  AND us.billing_subject_id IS NULL;

UPDATE user_platform_quotas upq
SET billing_subject_id = bs.id
FROM billing_subjects bs
WHERE bs.type = 'user'
  AND bs.user_id = upq.user_id
  AND bs.deleted_at IS NULL
  AND upq.billing_subject_id IS NULL;
