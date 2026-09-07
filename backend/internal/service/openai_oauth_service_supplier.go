// APEXONE-EXT: 双边市场 OpenAI 侧——把 Codex OAuth 协议层暴露给「会话存在数据库里」的供给接入。
//
// 与 oauth_service_supplier.go（Claude 侧）一字不差的理由：上游 OpenAIOAuthService 的
// GenerateAuthURL/ExchangeCode 把 PKCE 材料塞进进程内 sessionStore，兑换时按 session_id 取回。
// 管理端用它没问题；供给者自助接入用不了——会话必须有归属人、跨实例、扛重启
// （见 migrations/226_supplier_oauth_sessions.sql）。所以这里只把「生成 PKCE 材料」与
// 「用 PKCE 材料换 token 并富化身份」两步从内存会话里解耦，协议细节（endpoint/client_id/
// scope/id_token 解析/enrich）全部复用同包与 internal/pkg/openai 里已有实现，一行不重写。
//
// gpt-6 Astra 经 Codex 可用，而 OAuthClientConfigByPlatform 恒用 Codex CLI client + simplified flow，
// 所以供给接入走的就是 Codex OAuth——Plus/Pro/Business 订阅都能由此共享 gpt-6。
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// NewSupplierAuthorization 生成一次 Codex OAuth 授权所需的 PKCE 材料与授权链接，**不做任何存储**。
//
// scope 参数只为与 Claude 侧 supplierOAuthProvider 接口同形；OpenAI 侧 scope 固定 DefaultScopes，
// 由 BuildAuthorizationURLForPlatform 内部设置，这里记进 SupplierAuthorization 仅供排查。
func (s *OpenAIOAuthService) NewSupplierAuthorization(scope string) (*SupplierAuthorization, error) {
	state, err := openai.GenerateState()
	if err != nil {
		return nil, fmt.Errorf("generate state: %w", err)
	}
	codeVerifier, err := openai.GenerateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate code verifier: %w", err)
	}
	sessionID, err := openai.GenerateSessionID()
	if err != nil {
		return nil, fmt.Errorf("generate session id: %w", err)
	}
	// platform 传空即 Codex CLI client + simplified flow（见 OAuthClientConfigByPlatform）。
	// redirectURI 传空即 DefaultRedirectURI，与兑换时保持一致。
	authURL := openai.BuildAuthorizationURLForPlatform(state, openai.GenerateCodeChallenge(codeVerifier), "", "")
	if scope == "" {
		scope = openai.DefaultScopes
	}
	return &SupplierAuthorization{
		SessionID:    sessionID,
		AuthURL:      authURL,
		State:        state,
		CodeVerifier: codeVerifier,
		Scope:        scope,
	}, nil
}

// ExchangeSupplierCode 用调用方自己保管的 PKCE 材料兑换 token，并解析 id_token 富化出身份信息。
//
// 不走 sessionStore（调用方持有 CodeVerifier），也不走 proxy（供给者自助接入没有挑出口代理这一步，
// 与 Claude 侧同理）。redirectURI/clientID 必须与 NewSupplierAuthorization 生成授权 URL 时一致。
func (s *OpenAIOAuthService) ExchangeSupplierCode(ctx context.Context, code string, auth *SupplierAuthorization) (*OpenAITokenInfo, error) {
	if auth == nil {
		return nil, fmt.Errorf("authorization material is required")
	}
	tokenResp, err := s.oauthClient.ExchangeCode(ctx, code, auth.CodeVerifier, openai.DefaultRedirectURI, "", openai.ClientID)
	if err != nil {
		return nil, err
	}

	var userInfo *openai.UserInfo
	if tokenResp.IDToken != "" {
		claims, parseErr := openai.ParseIDToken(tokenResp.IDToken)
		if parseErr != nil {
			slog.Warn("openai_supplier_id_token_parse_failed", "error", parseErr)
		} else {
			userInfo = claims.GetUserInfo()
		}
	}

	tokenInfo := &OpenAITokenInfo{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresIn:    int64(tokenResp.ExpiresIn),
		ExpiresAt:    time.Now().Unix() + int64(tokenResp.ExpiresIn),
		ClientID:     openai.ClientID,
	}
	if userInfo != nil {
		tokenInfo.Email = userInfo.Email
		tokenInfo.ChatGPTAccountID = userInfo.ChatGPTAccountID
		tokenInfo.ChatGPTUserID = userInfo.ChatGPTUserID
		tokenInfo.OrganizationID = userInfo.OrganizationID
		tokenInfo.PlanType = userInfo.PlanType
	}
	// 富化订阅到期等信息，与管理端 ExchangeCode 同一步。
	s.enrichTokenInfo(ctx, tokenInfo, "")
	return tokenInfo, nil
}
