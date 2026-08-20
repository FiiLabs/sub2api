// APEXONE-EXT: 双边市场——收款账号的密文封装（迁移 232）。
//
// payout_account 装的是银行卡号 / USDT 地址 / 支付宝账号。它与这套系统里其余
// 敏感字段有一点不同：**当事人换不掉**。密码泄漏了可以改，API key 泄漏了可以吊销，
// 而一个人的银行卡号泄漏了就是泄漏了。因此它按 AES-256-GCM 加密入库，
// 复用既有的 SecretEncryptor（密钥即 TOTP_ENCRYPTION_KEY），不引入第二套密钥管理。
//
// 这里刻意**只**封装这一个字段，不做成通用的"加密列"设施：
// 通用设施要回答密钥轮换、字段发现、批量回填一整串问题，而现在只有一个字段需要它。
package repository

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// supplierPayoutCipherPrefix 是密文的版本标签。
//
// 带前缀而不是"试着解密，失败就当明文"：后者要靠 GCM 的认证失败来分辨两种情况，
// 于是"密钥配错了"与"这是一行历史明文"长得一模一样——前者必须炸，后者必须放行。
// 前缀让这个判断变成一次字符串比较，不依赖任何猜测。
//
// 带版本号是为了将来换算法时旧行仍可读：换算法只需新增一个前缀分支。
const supplierPayoutCipherPrefix = "enc.v1:"

// payoutAccountCipher 是 payout_account 的读写两端。
type payoutAccountCipher struct {
	encryptor service.SecretEncryptor
}

// seal 把明文变成入库的形态。
//
// 没有加密器时**报错**，而不是退回明文入库。少数几个"降级成明文"看起来更友好的
// 场合里，这个不是其中之一：调用方以为自己存的是密文，运营以为库里是密文，
// 而实际上不是——这种沉默的降级正是这次改动要消灭的东西。
// 生产路径上它不可能为空：NewAESEncryptor 在密钥不合法时直接让进程起不来。
func (c payoutAccountCipher) seal(plaintext string) (string, error) {
	if c.encryptor == nil {
		return "", fmt.Errorf("payout account cipher is not configured")
	}
	sealed, err := c.encryptor.Encrypt(plaintext)
	if err != nil {
		return "", fmt.Errorf("encrypt payout account: %w", err)
	}
	return supplierPayoutCipherPrefix + sealed, nil
}

// open 把入库形态还原成明文。
//
// 三条分支，每条对应一种真实存在的库内状态：
//   - 没有前缀 → 迁移 232 之前写下的历史明文，原样返回。旧单子照常能打款，
//     不需要停机窗口（理由写在 232 的迁移注释里）。
//   - 有前缀、解得开 → 正常路径。
//   - 有前缀、解不开 → **报错**，绝不返回空串。
//
// 最后那条是这个函数里唯一要紧的决定。密钥配错或轮换过时，解密必然失败；
// 若此时把空串当成结果往上层送，运营看到的是一张收款账号为空的提现单——
// 他会以为是供给者没填，然后去问、或者干脆按备注里的信息打款。
// 一个报错会让这张单子留在待办里，而那正是它此刻应该待的地方。
func (c payoutAccountCipher) open(stored string) (string, error) {
	if !strings.HasPrefix(stored, supplierPayoutCipherPrefix) {
		return stored, nil
	}
	if c.encryptor == nil {
		return "", fmt.Errorf("payout account is encrypted but cipher is not configured")
	}
	plaintext, err := c.encryptor.Decrypt(strings.TrimPrefix(stored, supplierPayoutCipherPrefix))
	if err != nil {
		// 不把 err 原文往上带：它来自 GCM，内容对排查没有帮助，
		// 而"解不开"这件事本身已经说完了要说的。
		return "", fmt.Errorf("decrypt payout account (encryption key changed?): %w", err)
	}
	return plaintext, nil
}
