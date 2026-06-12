-- Dynamic user-facing billing multiplier based on the final serving account.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS dynamic_rate_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS dynamic_rate_mode VARCHAR(20) NOT NULL DEFAULT 'off',
    ADD COLUMN IF NOT EXISTS dynamic_rate_margin DECIMAL(10,4) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS dynamic_rate_min_multiplier DECIMAL(10,4),
    ADD COLUMN IF NOT EXISTS dynamic_rate_max_multiplier DECIMAL(10,4);

COMMENT ON COLUMN groups.dynamic_rate_enabled IS '是否启用按最终承接账号倍率动态计算用户侧倍率';
COMMENT ON COLUMN groups.dynamic_rate_mode IS '动态倍率模式：off/account_plus_margin/account_markup';
COMMENT ON COLUMN groups.dynamic_rate_margin IS '动态倍率加价：plus 模式为倍率差值，markup 模式为乘法加价比例';
COMMENT ON COLUMN groups.dynamic_rate_min_multiplier IS '动态倍率下限；NULL 表示不限制';
COMMENT ON COLUMN groups.dynamic_rate_max_multiplier IS '动态倍率上限；NULL 表示不限制';
