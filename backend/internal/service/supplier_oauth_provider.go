// APEXONE-EXT: 双边市场——把「某平台的供给 OAuth 协议能力」抽象成一个接口，
// 让 SupplierOnboardingService 的接入编排与具体平台（Claude / OpenAI）解耦。
//
// 抽象的边界刻意选在「造授权材料」+「换 token 并组装成平台无关的接入产物」两步：
// CompleteOAuth 拿到 supplierOnboardResult 就能建号，不必知道任何一个平台的 token 形状、
// 凭证键名或身份字段。新增一个平台 = 加一个 provider 适配器，编排一行不改。
package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

// supplierIdentityValue 是一条「用来判定是不是同一份上游订阅」的身份键值，按强度排序使用。
type supplierIdentityValue struct {
	Key   SupplierIdentityKey
	Value string
}

// supplierOnboardResult 是一次供给 OAuth 兑换的平台无关产物，供 CompleteOAuth 建号。
type supplierOnboardResult struct {
	// AccountType 建号时写入 accounts.type：Claude=setup-token，OpenAI=oauth。
	AccountType string
	// Credentials 已按平台组装好的 accounts.credentials。
	Credentials map[string]any
	// Identity 去重用的身份键值，**强度从高到低**（第一个能唯一标识订阅）。空则拒绝建号。
	Identity []supplierIdentityValue
	// DefaultName 默认账号名（一般是邮箱），供给者没起名时用；空则由 accountName 兜底。
	DefaultName string
}

// supplierOAuthProvider 是某平台的供给 OAuth 协议能力。
//
// 窄接口：只需要「造材料」和「换 token 并组装产物」两件事，声明成两个方法能让测试
// 不必造一个带 sessionStore / HTTP client 的真服务（同 supplierClaudeOAuth 当年的理由）。
type supplierOAuthProvider interface {
	NewSupplierAuthorization() (*SupplierAuthorization, error)
	ExchangeSupplierCode(ctx context.Context, code string, auth *SupplierAuthorization) (*supplierOnboardResult, error)
}

// nonEmptyIdentity 过滤掉值为空的身份键，保持顺序。
func nonEmptyIdentity(values ...supplierIdentityValue) []supplierIdentityValue {
	out := make([]supplierIdentityValue, 0, len(values))
	for _, v := range values {
		if v.Value != "" {
			out = append(out, v)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Claude 适配器：包装 *OAuthService（setup-token / user:inference scope）。
// ---------------------------------------------------------------------------

type claudeSupplierProvider struct{ svc *OAuthService }

func (p claudeSupplierProvider) NewSupplierAuthorization() (*SupplierAuthorization, error) {
	return p.svc.NewSupplierAuthorization(oauth.ScopeInference)
}

func (p claudeSupplierProvider) ExchangeSupplierCode(ctx context.Context, code string, auth *SupplierAuthorization) (*supplierOnboardResult, error) {
	ti, err := p.svc.ExchangeSupplierCode(ctx, code, auth)
	if err != nil {
		return nil, err
	}
	return &supplierOnboardResult{
		AccountType: AccountTypeSetupToken,
		Credentials: buildSupplierClaudeCredentials(ti),
		Identity: nonEmptyIdentity(
			supplierIdentityValue{SupplierIdentityAccountUUID, ti.AccountUUID},
			supplierIdentityValue{SupplierIdentityEmailAddress, ti.EmailAddress},
		),
		DefaultName: ti.EmailAddress,
	}, nil
}

// ---------------------------------------------------------------------------
// OpenAI 适配器：包装 *OpenAIOAuthService（Codex OAuth / oauth type）。
// ---------------------------------------------------------------------------

type openaiSupplierProvider struct{ svc *OpenAIOAuthService }

func (p openaiSupplierProvider) NewSupplierAuthorization() (*SupplierAuthorization, error) {
	return p.svc.NewSupplierAuthorization("")
}

func (p openaiSupplierProvider) ExchangeSupplierCode(ctx context.Context, code string, auth *SupplierAuthorization) (*supplierOnboardResult, error) {
	ti, err := p.svc.ExchangeSupplierCode(ctx, code, auth)
	if err != nil {
		return nil, err
	}
	// chatgpt_account_id 最强（一份订阅一个）；email 次之。**不用 organization_id**——
	// 团队/工作区席位会让同事的合法第二个号被判成重复（同 Claude 侧排除 org_uuid）。
	return &supplierOnboardResult{
		AccountType: AccountTypeOAuth,
		Credentials: p.svc.BuildAccountCredentials(ti),
		Identity: nonEmptyIdentity(
			supplierIdentityValue{SupplierIdentityChatGPTAccountID, ti.ChatGPTAccountID},
			supplierIdentityValue{SupplierIdentityEmail, ti.Email},
		),
		DefaultName: ti.Email,
	}, nil
}
