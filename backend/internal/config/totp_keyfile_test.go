package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// APEXONE-EXT: 自动管理的加密钥匙的三条命门。
//
// 这把钥匙护着金库私钥、收款账号、TOTP 密钥的密文。它出问题的形态全是静默的：
// 每次启动换一把 → 密文在下一次重启后集体变孤儿（上线第一天真实发生过，
// 症状是 "message authentication failed"）；坏文件被覆盖 → 已有密文永久孤儿。

// 首次启动：生成 + 落盘 + durable；第二次启动：读回**同一把**。
// 「同一把」是这个功能存在的全部意义——寿命跨进程。
func TestTotpKeyfilePersistsAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("CONFIG_FILE", "")

	first, durable := loadOrPersistTotpEncryptionKey()
	require.True(t, durable, "落盘成功的钥匙必须报 durable——它决定控制台允不允许保存金库私钥")
	require.Len(t, first, 64)

	// 文件在、权限对。0600 不是洁癖：这个文件就是全部密文的万能钥匙。
	path := filepath.Join(dir, totpKeyFileName)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	second, durable := loadOrPersistTotpEncryptionKey()
	assert.True(t, durable)
	assert.Equal(t, first, second, "重启拿到了另一把钥匙——所有已有密文当场变孤儿")
}

// 文件内容坏了：**不覆盖**，退回临时钥匙且 durable=false。
//
// 覆盖是这里唯一不可逆的错：那可能只是一把被截断/改坏的真钥匙，运维还有机会
// 从备份里救回来；覆盖之后就没有然后了。durable=false 让金库私钥的保存被
// Seal 那道门挡住，坏状态不会再繁殖新的孤儿密文。
func TestTotpKeyfileRefusesToOverwriteCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("CONFIG_FILE", "")
	path := filepath.Join(dir, totpKeyFileName)
	require.NoError(t, os.WriteFile(path, []byte("not-a-hex-key"), 0o600))

	key, durable := loadOrPersistTotpEncryptionKey()
	assert.False(t, durable)
	assert.Len(t, key, 64, "还是要给一把能用的临时钥匙——TOTP 登录不能因此瘫掉")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "not-a-hex-key", string(raw), "坏文件被覆盖了——被改坏的真钥匙从此救不回来")
}

// 目录写不进去：退回临时钥匙 + durable=false（老行为），不报致命错。
func TestTotpKeyfileFallsBackWhenDirIsReadOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	t.Setenv("DATA_DIR", dir)
	t.Setenv("CONFIG_FILE", "")

	key, durable := loadOrPersistTotpEncryptionKey()
	assert.False(t, durable, "写不进去的钥匙谎报 durable，会放行一次注定孤儿的私钥保存")
	assert.Len(t, key, 64)
}

// 路径解析的优先级：CONFIG_FILE 的目录 > DATA_DIR。钥匙该躺在 config.yaml
// 旁边——备份/迁移数据目录时自然被一起带走。
func TestTotpKeyfileDirFollowsConfigFile(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("CONFIG_FILE", filepath.Join(cfgDir, "config.yaml"))
	t.Setenv("DATA_DIR", t.TempDir())

	assert.Equal(t, cfgDir, totpKeyFileDir())
	assert.True(t, strings.HasSuffix(filepath.Join(totpKeyFileDir(), totpKeyFileName), totpKeyFileName))
}
