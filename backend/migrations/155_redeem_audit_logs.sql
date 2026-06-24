-- 兑换码入账审计流水：记录 who(actor_user_id)/when(created_at)/code/amount/subject，资金入账可审计。
-- 与 RedeemForSubject 同一事务写入，保证审计与入账原子一致。沿用 payment_audit_logs 范式。
CREATE TABLE IF NOT EXISTS redeem_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    redeem_code_id BIGINT NOT NULL,
    code VARCHAR(64) NOT NULL,
    actor_user_id BIGINT NOT NULL,
    billing_subject_id BIGINT NULL,
    code_type VARCHAR(20) NOT NULL,
    amount NUMERIC(20,8) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_redeem_audit_logs_code_id ON redeem_audit_logs(redeem_code_id);
CREATE INDEX IF NOT EXISTS idx_redeem_audit_logs_actor ON redeem_audit_logs(actor_user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_redeem_audit_logs_subject ON redeem_audit_logs(billing_subject_id, created_at);
