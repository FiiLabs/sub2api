// APEXONE-EXT: 收款账号密文封装的单元测试。
//
// 这一层的性质全在进程内（前缀怎么分辨、解不开时返回什么），不需要数据库，
// 因此与本包其余仓储测试一样不带 build tag，`make test-unit` 与
// `make test-integration` 两边都跑到。
//
// 四条性质，每条对应一种「钱打错地方」的方式：
//   - 密文不含明文（否则加了等于没加）；
//   - 往返回得来（否则一张单子永远打不出去）；
//   - 历史明文照常读（否则升级当天所有待办单同时失效）；
//   - 解不开时报错而**不是**空串（否则运营看到一张收款账号为空的单子）。
package repository

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func payoutCipherWithKey(b byte) payoutAccountCipher {
	return payoutAccountCipher{encryptor: &AESEncryptor{key: bytes.Repeat([]byte{b}, 32)}}
}

// 一个真实的收款账号：足够长、含分隔符，且在密文里一眼能认出来。
const testPayoutAccount = "6222 0202 0001 2345 678 / 张三 / 招商银行深圳分行"

func TestPayoutAccountSealHidesPlaintext(t *testing.T) {
	cipher := payoutCipherWithKey(0x11)

	sealed, err := cipher.seal(testPayoutAccount)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(sealed, supplierPayoutCipherPrefix),
		"没有版本前缀，读路径就没法把它与历史明文分开：%s", sealed)
	assert.NotContains(t, sealed, "6222", "卡号出现在密文里")
	assert.NotContains(t, sealed, "张三")
	assert.NotContains(t, sealed, "招商银行")
}

func TestPayoutAccountRoundTrips(t *testing.T) {
	cipher := payoutCipherWithKey(0x11)

	sealed, err := cipher.seal(testPayoutAccount)
	require.NoError(t, err)

	opened, err := cipher.open(sealed)
	require.NoError(t, err)
	assert.Equal(t, testPayoutAccount, opened)
}

// 同一个明文两次加密必须不同：GCM 的 nonce 是随机的。
//
// 这不是密码学炫技。相同明文得到相同密文，意味着任何拿到库的人不必解密就能看出
// "这两个供给者填的是同一张卡"——那正好是刷号与洗钱排查最想知道的信息，
// 也正好是我们不该在一份泄漏的备份里免费送出去的信息。
func TestPayoutAccountSealIsNotDeterministic(t *testing.T) {
	cipher := payoutCipherWithKey(0x11)

	first, err := cipher.seal(testPayoutAccount)
	require.NoError(t, err)
	second, err := cipher.seal(testPayoutAccount)
	require.NoError(t, err)

	assert.NotEqual(t, first, second,
		"同一个账号两次加密结果相同：不解密就能比对出两个人填了同一张卡")
}

// 迁移 232 之前写下的明文行原样返回。
//
// 这条是"不需要停机窗口"的全部依据：升级当天库里全是明文行，
// 如果它们读不出来，所有待办提现单会在同一时刻一起失效。
func TestPayoutAccountLegacyPlaintextPassesThrough(t *testing.T) {
	cipher := payoutCipherWithKey(0x11)

	opened, err := cipher.open(testPayoutAccount)
	require.NoError(t, err)
	assert.Equal(t, testPayoutAccount, opened)
}

// 换过密钥就解不开——而解不开必须是**错误**，不能是空串。
//
// 这是这个文件里唯一要紧的一条。若此时返回空串，运营看到的是一张收款账号为空的
// 提现单：他会以为供给者没填，然后去问、或者按备注里的信息打款。
// 一个错误会让这张单子留在待办里，而那正是它此刻该待的地方。
func TestPayoutAccountWrongKeyErrorsInsteadOfBlanking(t *testing.T) {
	sealed, err := payoutCipherWithKey(0x11).seal(testPayoutAccount)
	require.NoError(t, err)

	opened, err := payoutCipherWithKey(0x22).open(sealed)
	require.Error(t, err, "换了密钥居然解开了")
	assert.Empty(t, opened)
	assert.Contains(t, err.Error(), "payout account",
		"报错要说清是哪个字段——运营拿着这行日志才知道该去查密钥配置")
}

// 密文被截断 / 被改过同样是错误。
func TestPayoutAccountTamperedCiphertextErrors(t *testing.T) {
	cipher := payoutCipherWithKey(0x11)
	sealed, err := cipher.seal(testPayoutAccount)
	require.NoError(t, err)

	for name, broken := range map[string]string{
		"截断":        sealed[:len(sealed)-6],
		"不是 base64": supplierPayoutCipherPrefix + "这不是密文",
		"只有前缀":      supplierPayoutCipherPrefix,
	} {
		t.Run(name, func(t *testing.T) {
			opened, err := cipher.open(broken)
			assert.Error(t, err)
			assert.Empty(t, opened)
		})
	}
}

// 没配加密器时写入直接失败，而不是悄悄存明文。
//
// 生产路径上到不了这里（NewAESEncryptor 密钥不合法时进程就起不来），
// 但"降级成明文"是一个足够诱人的写法，得有一条测试挡着。
func TestPayoutAccountWithoutCipherRefusesToWrite(t *testing.T) {
	var missing service.SecretEncryptor
	cipher := payoutAccountCipher{encryptor: missing}

	sealed, err := cipher.seal(testPayoutAccount)
	require.Error(t, err, "没有加密器却把明文存下去了")
	assert.Empty(t, sealed)

	// 读则要分两种：历史明文仍要能读（否则没配密钥的部署连旧单子都打不了款），
	// 而已经是密文的行必须报错。
	legacy, err := cipher.open(testPayoutAccount)
	require.NoError(t, err)
	assert.Equal(t, testPayoutAccount, legacy)

	_, err = cipher.open(supplierPayoutCipherPrefix + "AAAA")
	assert.Error(t, err)
}
