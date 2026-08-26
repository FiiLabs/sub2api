// APEXONE-EXT: 双边市场——供给者自助提交中转账号（M7）。
//
// 第二条接入路径。OAuth 那条（M1 之前就有）接的是 Claude 订阅号；这条接的是
// 「URL + API Key」形态的 Anthropic 兼容中转：供给者自己运营（或转售）一个
// /v1/messages 端点，平台把消费者请求转发过去。
//
// # 信任模型在这里翻了个面，这是本文件所有安全动作的出发点
//
// OAuth 号的请求直达 Anthropic；中转号的请求打到**供给者控制的服务器**——
// 消费者的 prompt 对供给者可见。这是中转供给的固有性质，技术上无法规避，
// 产品上必须说出来（表单上的提示、协议条款），而不是当它不存在。
//
// 反过来平台这一侧多了一个攻击面：供给者填的 URL 是让**平台的服务器**去连的
// 地址，一个指向内网的 URL 就是一次 SSRF。所以 URL 校验不是格式洁癖，
// 是这条路径上唯一一道挡「拿平台当内网跳板」的闸。
//
// # 三道既有的闸原样适用
//
// 协议门禁、每人/每 IP 数量上限、失效事件熔断——中转号走的是与 OAuth 完全
// 相同的 finalizeSupplyAccount / 观察期 / 探测 / 结算机器。差异只有两处：
// 身份查重的键不同（中转没有上游身份，用 base_url+api_key 组合），以及
// 提交时当场做一次真实探测（OAuth 有 token 交换兜真伪，中转没有——
// 不当场验，一个抄错的 key 要等观察期第一次探测才暴露）。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	// ErrSupplierRelayDisabled 管理员没开中转接入。与整体接入开关分开报：
	// 「平台不收中转」和「平台不收供给」对供给者是两句不同的话。
	ErrSupplierRelayDisabled = infraerrors.BadRequest(
		"SUPPLIER_RELAY_DISABLED", "relay onboarding is not open")
	// ErrSupplierRelayBaseURLInvalid URL 过不了闸。消息里不复述他填了什么——
	// 表单上就能看到自己填的值。
	ErrSupplierRelayBaseURLInvalid = infraerrors.BadRequest(
		"SUPPLIER_RELAY_BASE_URL_INVALID", "the base url is not acceptable")
	// ErrSupplierRelayKeyInvalid API Key 形状不对（空/超长/带空白）。
	ErrSupplierRelayKeyInvalid = infraerrors.BadRequest(
		"SUPPLIER_RELAY_KEY_INVALID", "the api key is not acceptable")
	// ErrSupplierRelayProbeFailed 提交时探测没打通。4xx：要改的是他填的东西
	// （URL 或 key），不是平台的故障。
	ErrSupplierRelayProbeFailed = infraerrors.BadRequest(
		"SUPPLIER_RELAY_PROBE_FAILED", "the relay endpoint did not answer the probe")
)

const (
	// supplierRelayKeyMaxLen API Key 的长度上限。真实 key 在几十到二百字符之间，
	// 512 只是挡「把整个 .env 粘进来」这类事故。
	supplierRelayKeyMaxLen = 512
	// supplierRelayBaseURLMaxLen 与 credentials jsonb 里合理的存储上限对齐。
	supplierRelayBaseURLMaxLen = 256
	// supplierRelayFallbackProbeModel 观察期没配 probe_model 时的探测模型。
	// 与 AccountTestService 的 Claude 默认（claude-sonnet-4-5）一字不差——
	// 提交时探测与观察期探测必须是同一判据，否则一个端点会「提交时被拒、
	// 观察期却探测得过」或反过来。
	//
	// 教训（2026-08-26，第二次被真实世界纠正）：第一版在这里写死了
	// claude-3-5-haiku-20241022，一个真实中转只路由当代模型（订阅号的正常
	// 形态），探测自己撞 503 把健康端点拒了。探测模型不能钉死在某个年份上。
	supplierRelayFallbackProbeModel = "claude-sonnet-4-5"
	// supplierRelayProbeTimeout 探测超时。一个 15 秒都答不上一条 1 token 请求的
	// 中转，进了池子也只会把消费者的请求拖死。
	supplierRelayProbeTimeout = 15 * time.Second
)

// SupplierRelaySubmission 是一次中转接入申请。
type SupplierRelaySubmission struct {
	UserID   int64
	BaseURL  string
	APIKey   string
	Name     string
	ClientIP string
}

// RelayEnabled 中转接入开没开（总开关 && relay 开关）。给状态接口用。
func (s *SupplierOnboardingService) RelayEnabled(ctx context.Context) bool {
	if s == nil || !s.IsEnabled(ctx) {
		return false
	}
	return s.onboardingSettings(ctx).RelayEnabled
}

// SubmitRelay 供给者提交一个中转端点。成功即挂进供给池的观察期。
func (s *SupplierOnboardingService) SubmitRelay(ctx context.Context, input *SupplierRelaySubmission) (*SupplierAccountView, error) {
	if s == nil || s.repo == nil || s.accountRepo == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	if input == nil || input.UserID <= 0 {
		return nil, ErrSupplierOnboardingDisabled
	}
	groupID, ok := s.supplyGroupID(ctx)
	if !ok {
		return nil, ErrSupplierOnboardingDisabled
	}
	if !s.onboardingSettings(ctx).RelayEnabled {
		return nil, ErrSupplierRelayDisabled
	}
	// 门禁顺序与 CompleteOAuth 一字不差：先答「你能不能接」，再看「你交的是什么」。
	if err := s.requireAgreement(ctx, input.UserID); err != nil {
		return nil, err
	}
	if err := s.requireCapacity(ctx, input.UserID, input.ClientIP); err != nil {
		return nil, err
	}

	baseURL, err := normalizeRelayBaseURL(input.BaseURL)
	if err != nil {
		return nil, err
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" || len(apiKey) > supplierRelayKeyMaxLen || strings.ContainsAny(apiKey, " \t\r\n") {
		return nil, ErrSupplierRelayKeyInvalid
	}

	// 查重在探测之前：一个已经挂着的端点不值得再打一次探测请求。
	// 键是 (base_url, api_key) 组合——中转没有上游身份可查，端点+钥匙就是
	// 它的身份。查的是**全部** apikey 账号（含管理员手动加的自营号）：
	// 同一个端点被挂两次，两个账号会按同一份供给各算各的分成。
	existingID, err := s.repo.FindAccountIDByRelayEndpoint(ctx, PlatformAnthropic, baseURL, apiKey)
	if err != nil {
		// 与订阅查重同一条规矩：查询失败往上抛。查重失败时放行等于关掉闸门。
		return nil, err
	}
	if existingID > 0 {
		slog.Info("[SupplierOnboarding] duplicate relay endpoint rejected",
			"existing_account_id", existingID)
		return nil, ErrSupplierAccountAlreadyBound
	}

	// 提交时当场探测。OAuth 路径有 token 交换兜真伪；中转没有——不验的话，
	// 一个抄错一位的 key 要等观察期第一次探测才暴露，而供给者早已关掉页面。
	if err := s.probeRelay(ctx, baseURL, apiKey, s.relayProbeModel(ctx)); err != nil {
		return nil, err
	}

	account := &Account{
		Name:        s.relayAccountName(input.Name, baseURL),
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": baseURL, "api_key": apiKey},
		Extra: map[string]any{
			SupplyStateExtraKey:          SupplyStatePendingReview,
			SupplyProbationSinceExtraKey: time.Now().Format(time.RFC3339),
		},
		Concurrency: supplierDefaultConcurrency,
		Priority:    supplierDefaultPriority,
		Status:      StatusActive,
		Schedulable: false,
		// 中转没有「订阅到期」概念；失效走上游 401 的既有错误状态机。
		AutoPauseOnExpired: false,
	}
	return s.finalizeSupplyAccount(ctx, account, input.UserID, input.ClientIP, groupID)
}

// relayAccountName 起名：用户给了就用，没给用端点主机名——
// 供给者列表里一排「未命名」分不出谁是谁。
func (s *SupplierOnboardingService) relayAccountName(requested, baseURL string) string {
	if name := strings.TrimSpace(requested); name != "" {
		return name
	}
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Host != "" {
		return "中转 · " + parsed.Host
	}
	return "中转账号"
}

// normalizeRelayBaseURL 校验并归一化中转地址。
//
// 这是 SSRF 的闸（见文件头），拒绝的每一类都对应一次真实的攻击姿势：
//
//   - 非 https：明文里跑的是消费者的 prompt 和中转的 key；
//   - 带 userinfo / query / fragment：转发时会被拼接进请求，语义没人说得清；
//   - localhost / 私网 / 环回 / 链路本地 / 元数据段的 IP 字面量：
//     「让平台去连 169.254.169.254」是云上 SSRF 的标准第一步。
//
// 边界说明：只挡 IP **字面量**与 localhost 域名。一个解析到内网的公网域名
// （DNS rebinding）这里不挡——那需要在**拨号时**按解析结果再判一次，
// 属于传输层的活；此处的闸挡掉的是不需要任何基础设施就能试的那批。
func normalizeRelayBaseURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > supplierRelayBaseURLMaxLen {
		return "", ErrSupplierRelayBaseURLInvalid
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrSupplierRelayBaseURLInvalid
	}
	if parsed.Scheme != "https" {
		return "", ErrSupplierRelayBaseURLInvalid
	}
	if parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", ErrSupplierRelayBaseURLInvalid
	}
	hostname := parsed.Hostname()
	if hostname == "" {
		return "", ErrSupplierRelayBaseURLInvalid
	}
	lowered := strings.ToLower(hostname)
	if lowered == "localhost" || strings.HasSuffix(lowered, ".localhost") {
		return "", ErrSupplierRelayBaseURLInvalid
	}
	if ip := net.ParseIP(hostname); ip != nil {
		if !ip.IsGlobalUnicast() || ip.IsPrivate() {
			// 环回、链路本地（含 169.254 元数据段）、组播、未指定地址、私网段
			// 全部落在这两个判断里。
			return "", ErrSupplierRelayBaseURLInvalid
		}
	}
	// 归一化：去掉尾部斜杠，丢弃大小写差异的 host——
	// 查重键要求同一个端点只有一种写法。
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String(), nil
}

// relayProbeModel 提交时探测用哪个模型：观察期配了 probe_model 就用它
//（同一个号提交时和进观察期后必须被同一根尺子量），没配用平台默认。
func (s *SupplierOnboardingService) relayProbeModel(ctx context.Context) string {
	if settings := s.probationSettings(ctx); settings != nil {
		if model := strings.TrimSpace(settings.ProbeModel); model != "" {
			return model
		}
	}
	return supplierRelayFallbackProbeModel
}

// probeRelay 对端点打一条最小的真实请求（1 token）。
//
// 只认 HTTP 200：401/403 是 key 错，404 是路径错，5xx 是端点坏——
// 对提交的人全都是「回去改你填的东西」，所以归成同一个 4xx，
// 细节进日志不进响应（响应会被脚本重放，日志才是给排查看的）。
func (s *SupplierOnboardingService) probeRelay(ctx context.Context, baseURL, apiKey, model string) error {
	body, err := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	})
	if err != nil {
		return fmt.Errorf("encode relay probe: %w", err)
	}

	probeCtx, cancel := context.WithTimeout(ctx, supplierRelayProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost,
		strings.TrimRight(baseURL, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return ErrSupplierRelayBaseURLInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := s.relayProbeClient
	if client == nil {
		client = &http.Client{Timeout: supplierRelayProbeTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		slog.Info("[SupplierOnboarding] relay probe transport failure", "error", err)
		return ErrSupplierRelayProbeFailed
	}
	defer func() { _ = resp.Body.Close() }()
	// 读掉一点响应体让连接可复用；内容只进日志。
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode != http.StatusOK {
		slog.Info("[SupplierOnboarding] relay probe rejected",
			"status", resp.StatusCode, "body", string(snippet))
		return ErrSupplierRelayProbeFailed
	}
	return nil
}
