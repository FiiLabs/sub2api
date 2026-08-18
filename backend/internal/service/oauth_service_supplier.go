// APEXONE-EXT: 双边市场——把 Claude OAuth 的协议层暴露给「会话存在数据库里」的调用方。
//
// 上游 OAuthService 的 GenerateAuthURL/ExchangeCode 是一对闭合的流程：PKCE 材料生成后
// 塞进进程内的 sessionStore，兑换时再按 session_id 取回来。管理端用它没问题——只有管理员
// 能调，重试成本可忽略。供给者自助接入用不了：会话必须有归属人、必须跨实例、必须扛重启
// （三条理由写在 migrations/226_supplier_oauth_sessions.sql 顶部）。
//
// 所以这里只做一件事：把「生成 PKCE 材料」和「用 PKCE 材料换 token」两步从内存会话里
// 解耦出来，让会话怎么存变成调用方的事。协议本身（endpoint、client_id、scope、
// code_challenge 算法、TokenInfo 组装）一行都不重写——全部复用同包里已有的实现。
//
// 放在本包新文件里，是为了让这次扩展的 core 侵入为零：`exchangeCodeForToken` 是未导出的，
// 同包才调得到；换成新包就得导出它或复制一遍协议细节，前者改上游文件，后者会在
// Anthropic 改协议时留下一份必然过期的副本。
package service

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

// SupplierAuthorization 是一次待完成的授权所需的全部材料。
//
// CodeVerifier 由调用方保管（供给者接入把它写进 supplier_oauth_sessions），
// 本服务不再持有任何一份——同一份 PKCE 材料存两处，就会有两处需要清理、
// 两处可能不一致。
type SupplierAuthorization struct {
	// SessionID 交给前端的不透明句柄。
	SessionID string
	// AuthURL 供给者要去访问的授权页。
	AuthURL string
	// State 防 CSRF 的随机串，兑换时要原样带回。
	State string
	// CodeVerifier PKCE 校验串，兑换时要原样带回。
	CodeVerifier string
	// Scope 决定兑换时按不按 setup-token 走。
	Scope string
}

// NewSupplierAuthorization 生成一次授权所需的 PKCE 材料与授权链接，**不做任何存储**。
//
// 与 generateAuthURLWithScope 的唯一区别就是最后那句 sessionStore.Set 没有了。
func (s *OAuthService) NewSupplierAuthorization(scope string) (*SupplierAuthorization, error) {
	state, err := oauth.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	codeVerifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate code verifier: %w", err)
	}
	sessionID, err := oauth.GenerateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}

	return &SupplierAuthorization{
		SessionID:    sessionID,
		AuthURL:      oauth.BuildAuthorizationURL(state, oauth.GenerateCodeChallenge(codeVerifier), scope),
		State:        state,
		CodeVerifier: codeVerifier,
		Scope:        scope,
	}, nil
}

// ExchangeSupplierCode 用调用方自己保管的 PKCE 材料兑换 token。
//
// 不走 proxy：供给者自助接入没有「挑一个出口代理」这一步，也不该有——代理是平台的
// 基础设施配置，不是供给者能选的东西。需要时由管理员事后在账号上配。
func (s *OAuthService) ExchangeSupplierCode(ctx context.Context, code string, auth *SupplierAuthorization) (*TokenInfo, error) {
	if auth == nil {
		return nil, fmt.Errorf("authorization material is required")
	}
	isSetupToken := auth.Scope == oauth.ScopeInference
	return s.exchangeCodeForToken(ctx, code, auth.CodeVerifier, auth.State, "", isSetupToken)
}
