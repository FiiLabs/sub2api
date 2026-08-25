package config

import (
	"os"
	"testing"
)

// TestMain 把整个包的测试隔离进一个临时数据目录。
//
// Load() 会走到加密钥匙的落盘逻辑（loadOrPersistTotpEncryptionKey），
// 不隔离的话每一次 go test 都往**包目录**写一个 totp_encryption.key——
// 真的发生过：一次 git add -A 把它带进了仓库。密钥类文件离 git 越远越好，
// 而最可靠的"远"是让它根本不出现在仓库树里。
//
// 单个测试要自己的目录时照常 t.Setenv("DATA_DIR", …) 覆盖，互不干扰。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sub2api-config-test-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("DATA_DIR", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
