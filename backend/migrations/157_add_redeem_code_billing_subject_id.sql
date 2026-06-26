-- 157: D2 团队余额调整审计 —— 给 redeem_codes 加 billing_subject_id（归属计费主体）。
ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS billing_subject_id BIGINT NULL REFERENCES billing_subjects(id);
CREATE INDEX IF NOT EXISTS idx_redeem_codes_billing_subject_id ON redeem_codes(billing_subject_id);
