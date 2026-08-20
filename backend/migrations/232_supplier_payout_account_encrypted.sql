-- APEXONE-EXT: 双边市场——收款账号改为密文存储。
--
-- 229 把 payout_account 建成了 VARCHAR(256) 明文。它装的是供给者的银行卡号 /
-- USDT 地址 / 支付宝账号——是这套系统里**唯一**一个泄漏后能被直接用来冒名收款、
-- 且当事人无法自行更换的字段（换银行卡不像换密码）。一份库备份、一次只读账号
-- 的越权、一条走错地方的日志，都会把全部供给者的收款方式一次性带走。
--
-- 从 232 起写入的值是 `enc.v1:` + base64(nonce||ciphertext||tag)（AES-256-GCM，
-- 复用既有的 SecretEncryptor，密钥即 TOTP_ENCRYPTION_KEY）。
--
-- **这条迁移刻意不回填历史行**，只放宽列宽：
--   1. 加密要在应用层做，密钥在应用的配置里，SQL 里够不着。写一段 DO $$ 也拿不到
--      密钥，除非把密钥打进迁移文件——那等于把密钥永久写进 git 历史与每一份备份，
--      比这条迁移要修的问题更糟。
--   2. 读路径按前缀分辨：没有 `enc.v1:` 前缀的当成历史明文原样返回。所以旧单子
--      照常能打款，不需要停机窗口，也不存在"迁移跑到一半新旧混着"的坏状态。
--   3. 升级窗口内的旧单子会保持明文直到进入终态（提现单不可改，也不该为了换个
--      存储格式去改一张已经扣过钱的单子）。上线一个提现周期后这张表里就不会再有
--      明文行了——`SELECT count(*) FROM supplier_withdrawals
--      WHERE payout_account NOT LIKE 'enc.v1:%'` 归零即为完成。
--
-- 列宽必须放开：256 个字符（服务端按 rune 限长，CJK 备注型账号可达 768 字节）
-- 加密后约 1 060 个 base64 字符，塞不进 VARCHAR(256)。改 TEXT 而不是改成
-- VARCHAR(2048)：长度上限对一个密文没有任何业务含义，留一个数字在那里只会让
-- 下一次换算法的人再来算一次。真正的长度限制在服务端（SupplierPayoutAccountMaxLen）。
ALTER TABLE supplier_withdrawals
    ALTER COLUMN payout_account TYPE TEXT;

COMMENT ON COLUMN supplier_withdrawals.payout_account IS
    '收款账号密文：enc.v1: + base64(AES-256-GCM)；无前缀者为 232 之前的历史明文';
