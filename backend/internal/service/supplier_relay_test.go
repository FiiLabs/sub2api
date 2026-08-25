//go:build unit

// APEXONE-EXT: 双边市场——中转接入（M7）的单元测试。
//
// 两个重心，权重不同：
//
//  1. **URL 那道闸**（normalizeRelayBaseURL）。它是 SSRF 的唯一防线——供给者填的
//     URL 是让平台服务器去连的地址，放行一个 169.254.169.254 等于把云元数据端点
//     交给任何注册用户。每一类拒绝都值得一条独立断言。
//  2. **编排顺序**。开关 → 协议 → 容量 → URL/Key → 查重 → 探测 → 建号。
//     顺序错了各有各的事故：探测排在查重前是替人免费打压测，建号排在探测前是
//     把一个死端点挂进观察期。
package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubRoundTripper 让探测不出网：按预置状态码应答，并记录请求。
type stubRoundTripper struct {
	status   int
	requests []*http.Request
	err      error
}

func (rt *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req)
	if rt.err != nil {
		return nil, rt.err
	}
	return &http.Response{
		StatusCode: rt.status,
		Body:       http.NoBody,
		Header:     http.Header{},
	}, nil
}

// relayOnboardingJSON 中转开关开着的接入配置。
func relayOnboardingJSON() string {
	return `{"relay_enabled":true,"max_accounts_per_user":5,"max_accounts_per_ip":0}`
}

func newRelayService(
	t *testing.T,
	repo *supplierOnboardingRepoStub,
	store *supplierAccountStoreStub,
	probe *stubRoundTripper,
) *SupplierOnboardingService {
	t.Helper()
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), relayOnboardingJSON())
	if probe != nil {
		svc.relayProbeClient = &http.Client{Transport: probe}
	}
	return svc
}

func relaySubmission() *SupplierRelaySubmission {
	return &SupplierRelaySubmission{
		UserID:   7,
		BaseURL:  "https://Relay.Example.com/api/",
		APIKey:   "sk-relay-0123456789",
		ClientIP: "203.0.113.9",
	}
}

// ============================================================================
// URL 闸：每一类拒绝对应一种攻击姿势
// ============================================================================

func TestNormalizeRelayBaseURL(t *testing.T) {
	t.Run("合法地址通过并归一化", func(t *testing.T) {
		for raw, want := range map[string]string{
			"https://relay.example.com":        "https://relay.example.com",
			"https://Relay.Example.com/api/":   "https://relay.example.com/api",
			"  https://relay.example.com/  ":   "https://relay.example.com",
			"https://relay.example.com:8443/x": "https://relay.example.com:8443/x",
			// 公网 IP 字面量放行：不少小中转确实没有域名。
			"https://203.0.113.9":  "https://203.0.113.9",
			"https://203.0.113.9/": "https://203.0.113.9",
		} {
			got, err := normalizeRelayBaseURL(raw)
			require.NoError(t, err, raw)
			assert.Equal(t, want, got, "归一化不一致，同一个端点会有多种写法，查重键就废了")
		}
	})

	t.Run("SSRF 姿势逐个拒绝", func(t *testing.T) {
		for name, raw := range map[string]string{
			"明文 http":       "http://relay.example.com",
			"无 scheme":      "relay.example.com",
			"空串":            "",
			"云元数据段":         "https://169.254.169.254/latest/meta-data",
			"环回 v4":         "https://127.0.0.1:8080",
			"环回 v6":         "https://[::1]:8080",
			"私网 10 段":       "https://10.0.0.8",
			"私网 172 段":      "https://172.16.5.4",
			"私网 192 段":      "https://192.168.1.1",
			"未指定地址":         "https://0.0.0.0",
			"localhost":     "https://localhost:9000",
			"localhost 子域名": "https://svc.localhost",
			"带 userinfo":    "https://admin:pw@relay.example.com",
			"带 query":       "https://relay.example.com?x=1",
			"带 fragment":    "https://relay.example.com#frag",
			"超长":            "https://relay.example.com/" + strings.Repeat("x", 300),
		} {
			t.Run(name, func(t *testing.T) {
				_, err := normalizeRelayBaseURL(raw)
				assert.ErrorIs(t, err, ErrSupplierRelayBaseURLInvalid,
					"这一类放行了，平台就是任何注册用户的内网跳板：%s", raw)
			})
		}
	})
}

// ============================================================================
// 编排：门禁顺序与建号形态
// ============================================================================

// 快乐路径：探测通过 → 账号以 apikey 形态挂进观察期，归属/来源/分组齐全。
func TestSubmitRelayCreatesAPIKeyAccountInProbation(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	store := newSupplierAccountStoreStub()
	probe := &stubRoundTripper{status: http.StatusOK}
	svc := newRelayService(t, repo, store, probe)

	view, err := svc.SubmitRelay(context.Background(), relaySubmission())
	require.NoError(t, err)
	require.NotNil(t, view)

	require.Len(t, store.accounts, 1)
	var account *Account
	for _, stored := range store.accounts {
		account = stored
	}
	assert.Equal(t, PlatformAnthropic, account.Platform)
	assert.Equal(t, AccountTypeAPIKey, account.Type)
	// 归一化后的地址落进 credentials——查重键与转发目标必须是同一个字符串。
	assert.Equal(t, "https://relay.example.com/api", account.Credentials["base_url"])
	assert.Equal(t, "sk-relay-0123456789", account.Credentials["api_key"])
	assert.False(t, account.Schedulable, "新号直接可调度 = 跳过整个观察期")
	assert.Equal(t, SupplyStatePendingReview, account.Extra[SupplyStateExtraKey])
	assert.NotEmpty(t, account.Extra[SupplyProbationSinceExtraKey])
	// 没起名就用端点域名——列表里一排「未命名」分不出谁是谁。
	assert.Contains(t, account.Name, "relay.example.com")

	// 探测请求的形态：打的是归一化地址 + /v1/messages，带 key。
	require.Len(t, probe.requests, 1)
	assert.Equal(t, "https://relay.example.com/api/v1/messages", probe.requests[0].URL.String())
	assert.Equal(t, "sk-relay-0123456789", probe.requests[0].Header.Get("x-api-key"))
}

// 开关关着 → 明确的「不收中转」，而不是笼统的「不收供给」。
func TestSubmitRelayRequiresTheAdminSwitch(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	store := newSupplierAccountStoreStub()
	probe := &stubRoundTripper{status: http.StatusOK}
	svc := newOnboardingServiceWithLimits(t, repo, store, &supplierOAuthStub{},
		enabledSupplyPoolJSON(), `{"relay_enabled":false}`)
	svc.relayProbeClient = &http.Client{Transport: probe}

	_, err := svc.SubmitRelay(context.Background(), relaySubmission())
	assert.ErrorIs(t, err, ErrSupplierRelayDisabled)
	assert.Empty(t, store.accounts)
	assert.Empty(t, probe.requests, "开关都没开就去探测别人的端点")
}

// 协议门禁在一切之前——与 OAuth 路径同一条规矩。
func TestSubmitRelayRequiresAgreement(t *testing.T) {
	repo := &supplierOnboardingRepoStub{}
	repo.agreementAcceptedByDefault = false
	store := newSupplierAccountStoreStub()
	probe := &stubRoundTripper{status: http.StatusOK}
	svc := newRelayService(t, repo, store, probe)
	repo.agreementAcceptedByDefault = false

	_, err := svc.SubmitRelay(context.Background(), relaySubmission())
	require.Error(t, err)
	assert.Empty(t, store.accounts)
	assert.Empty(t, probe.requests)
}

// 查重命中 → 拒绝，且**不探测**：一个已经挂着的端点不值得再打一次请求。
func TestSubmitRelayRejectsDuplicateEndpointBeforeProbing(t *testing.T) {
	repo := &supplierOnboardingRepoStub{
		relayEndpoints: map[string]int64{
			"https://relay.example.com/api|sk-relay-0123456789": 42,
		},
	}
	store := newSupplierAccountStoreStub()
	probe := &stubRoundTripper{status: http.StatusOK}
	svc := newRelayService(t, repo, store, probe)

	_, err := svc.SubmitRelay(context.Background(), relaySubmission())
	assert.ErrorIs(t, err, ErrSupplierAccountAlreadyBound)
	assert.Empty(t, store.accounts)
	assert.Empty(t, probe.requests, "重复端点还被探测了一次——查重必须排在探测之前")
}

// 查重查询失败 → 拒绝而不是放行。查重失败时放行等于关掉闸门。
func TestSubmitRelayFailsClosedWhenDedupeLookupErrors(t *testing.T) {
	repo := &supplierOnboardingRepoStub{relayFindErr: assert.AnError}
	store := newSupplierAccountStoreStub()
	svc := newRelayService(t, repo, store, &stubRoundTripper{status: http.StatusOK})

	_, err := svc.SubmitRelay(context.Background(), relaySubmission())
	require.Error(t, err)
	assert.Empty(t, store.accounts)
}

// 探测不过 → 一行账号都不建。401 与 5xx 对提交的人是同一句话：回去改你填的东西。
func TestSubmitRelayRejectsWhenProbeFails(t *testing.T) {
	for name, probe := range map[string]*stubRoundTripper{
		"key 错（401）":  {status: http.StatusUnauthorized},
		"路径错（404）":   {status: http.StatusNotFound},
		"端点坏（500）":   {status: http.StatusInternalServerError},
		"连不上（传输失败）": {err: assert.AnError},
	} {
		t.Run(name, func(t *testing.T) {
			repo := &supplierOnboardingRepoStub{}
			store := newSupplierAccountStoreStub()
			svc := newRelayService(t, repo, store, probe)

			_, err := svc.SubmitRelay(context.Background(), relaySubmission())
			assert.ErrorIs(t, err, ErrSupplierRelayProbeFailed)
			assert.Empty(t, store.accounts, "探测都没过就建号——一个死端点被挂进了观察期")
		})
	}
}

// key 的形状闸：空 / 超长 / 带空白，全部在探测之前拒绝。
func TestSubmitRelayRejectsMalformedKeys(t *testing.T) {
	for name, key := range map[string]string{
		"空":    "",
		"全空白":  "   ",
		"带换行":  "sk-abc\ndef",
		"带空格":  "sk-abc def",
		"超长":   strings.Repeat("k", supplierRelayKeyMaxLen+1),
	} {
		t.Run(name, func(t *testing.T) {
			probe := &stubRoundTripper{status: http.StatusOK}
			svc := newRelayService(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(), probe)
			input := relaySubmission()
			input.APIKey = key

			_, err := svc.SubmitRelay(context.Background(), input)
			assert.ErrorIs(t, err, ErrSupplierRelayKeyInvalid)
			assert.Empty(t, probe.requests)
		})
	}
}

// RelayEnabled：总开关与 relay 开关都要开。
func TestRelayEnabledNeedsBothSwitches(t *testing.T) {
	t.Run("都开", func(t *testing.T) {
		svc := newRelayService(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(), nil)
		assert.True(t, svc.RelayEnabled(context.Background()))
	})
	t.Run("relay 开但供给池没配", func(t *testing.T) {
		svc := newOnboardingServiceWithLimits(t, &supplierOnboardingRepoStub{}, newSupplierAccountStoreStub(),
			&supplierOAuthStub{}, `{"enabled":false}`, relayOnboardingJSON())
		assert.False(t, svc.RelayEnabled(context.Background()),
			"供给池整体没开，中转标签页却亮着——点进去每一步都失败")
	})
}
