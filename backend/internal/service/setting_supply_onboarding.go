// APEXONE-EXT: 双边市场——接入准入的数量上限。
//
// 第六个 settings key。分家的理由与前几个一样（见 setting_supply_pool.go 头部）：
// 这组参数因第六种原因变动——结算参数动的是钱，池配置动的是路由，观察期动的是
// 「什么时候放行」，协议动的是条款，提现动的是出款；这里动的是**一个人（或一个
// 网络）最多能往平台塞几个号**。它随刷号手法的变化而调整，调整的人是风控，
// 调整的时机是看见异常之后，与其它五组的节奏毫无关系。
//
// # 为什么不塞进 supply_probation_settings
//
// 那个 key 上有一个 `Enabled`，意思是「自动入池总开关」。上限必须在它关着的时候
// 照样生效——邀请制人工核验期恰恰是最需要挡住批量挂号的阶段。两者同处一个结构体，
// 迟早有人（包括未来的我）把 `if !settings.Enabled { return }` 写在上限判断前面，
// 而那个 bug 的现象是「限额配了，但一个也没拦住」，没有任何日志会说出来。
//
// # 两道闸挡的不是同一件事
//
//   - **每人上限**挡的是「一个人挂一堆号」。但它有一个绕过成本极低的漏洞：
//     再注册一个用户就是一个新的 user_id，上限重新开始数。
//   - **每 IP 上限**挡的正是那个绕过——注册账号免费，换出口网络不免费。
//
// 所以每人上限单独存在时几乎只是一个礼貌性的护栏，真正有阻力的是第二道。
//
// # 但第二道默认是关的
//
// `MaxAccountsPerIP` 默认 0（不限），而每人上限默认有值。这个不对称是有意的：
// 运营商级 NAT、校园网、公司出口后面站着的是成百上千个真实的人，一个「每 IP 最多
// 3 个」的默认值会把他们中的绝大多数挡在门外，而现象是「注册了但挂不上号」——
// 用户不会来报障，他们只会离开。这道闸应当在运营者看过真实的 IP 分布之后再打开，
// 并且配一个远大于「一户人家」的数。
//
// 读失败一律回退到默认值，方向与其它几个 key 的 fail-closed 同向：读不到配置时
// 每人上限退回默认的 5（比运营配的宽松值更严），每 IP 上限退回不限（比运营配的
// 值更宽松）。后者看似不 fail-closed，但它是对的——一个因为数据库抖动就把整个
// 出口网络的人全部挡住的闸，造成的损失比它防的那点刷号大得多。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/sync/singleflight"
)

// SettingKeySupplyOnboarding 接入准入上限的 settings key。
const SettingKeySupplyOnboarding = "supply_onboarding_settings"

var (
	// ErrSupplierAccountLimitReached 这个人名下的供给账号已经到上限。
	//
	// 说得具体（"你已经挂满了"）是刻意的：这是他自己能纠正的事——解绑一个旧号就能
	// 再挂一个，而他看不到自己有几个号是不可能的（仪表盘上就是那一列）。
	ErrSupplierAccountLimitReached = infraerrors.BadRequest(
		"SUPPLIER_ACCOUNT_LIMIT_REACHED", "you have connected the maximum number of supply accounts")
	// ErrSupplierNetworkLimitReached 这个出口网络上挂的供给账号已经到上限。
	//
	// 文案刻意不说数字、也不说"还有几个人在你这个 IP 上挂了号"：那等于把同一个
	// 出口后面其他人的行为报给一个陌生人。他需要知道的只是「不是你的账号的问题，
	// 是网络的问题」——这句话足以让他去换个网络或者找运营。
	ErrSupplierNetworkLimitReached = infraerrors.BadRequest(
		"SUPPLIER_NETWORK_LIMIT_REACHED", "too many supply accounts have been connected from this network")
)

// 上限的上限。夹回区间而不是报错，理由见 SetSupplyOnboardingSettings。
const (
	// SupplyOnboardingMaxAccountsPerUserMax 每人上限最大可配到 100。
	//
	// 一个真实的供给者手上有几份 Claude 订阅？个位数。配到 100 已经等同于不限，
	// 更大的数字只说明运营想关掉这道闸——那应该填 0（明确的「不限」），
	// 而不是填一个看起来像限制的天文数字。
	SupplyOnboardingMaxAccountsPerUserMax = 100
	// SupplyOnboardingMaxAccountsPerIPMax 每 IP 上限最大可配到 10000。
	//
	// 比每人上限宽两个量级：这道闸的正常取值就该覆盖「一整个校园网后面有多少个
	// 真实供给者」，而那个数可以很大。
	SupplyOnboardingMaxAccountsPerIPMax = 10000
)

// 默认值。
const (
	supplyOnboardingDefaultMaxAccountsPerUser = 5
	supplyOnboardingDefaultMaxAccountsPerIP   = 0
)

// SupplyOnboardingSettings 是接入准入的数量上限。
//
// 两个字段都以 **0 = 不限** 为约定。0 在这里是一个有意义的取值而不是「没填」——
// 见 handler 侧用指针接收的理由。
type SupplyOnboardingSettings struct {
	// MaxAccountsPerUser 一个人名下最多能有几个未解绑的供给账号。
	//
	// 数的是**当下**的账号数，不是历史累计：解绑一个号就腾出一个位置。
	// 累计口径会让一个正常换号的供给者永远地耗尽自己的额度。
	MaxAccountsPerUser int `json:"max_accounts_per_user"`
	// MaxAccountsPerIP 同一个接入来源 IP 上最多能有几个未解绑的供给账号。
	//
	// 默认 0（不限），理由见文件头——这道闸开错了会静默地挡住整个 NAT 后面的人。
	MaxAccountsPerIP int `json:"max_accounts_per_ip"`
}

// DefaultSupplyOnboardingSettings 返回默认上限：每人 5 个，每 IP 不限。
func DefaultSupplyOnboardingSettings() *SupplyOnboardingSettings {
	return &SupplyOnboardingSettings{
		MaxAccountsPerUser: supplyOnboardingDefaultMaxAccountsPerUser,
		MaxAccountsPerIP:   supplyOnboardingDefaultMaxAccountsPerIP,
	}
}

// normalize 把越界值夹回区间。
//
// 负数一律夹成 0（不限）而不是夹成 1：一个填成 -1 的配置最可能的来源是手工改坏的
// JSON 或者一个把「不限」写成 -1 的旧前端，把它读成「每人最多 1 个」会在没有任何
// 提示的情况下让几乎所有人都挂不上号。
func (s *SupplyOnboardingSettings) normalize() {
	if s == nil {
		return
	}
	if s.MaxAccountsPerUser < 0 {
		s.MaxAccountsPerUser = 0
	}
	if s.MaxAccountsPerUser > SupplyOnboardingMaxAccountsPerUserMax {
		s.MaxAccountsPerUser = SupplyOnboardingMaxAccountsPerUserMax
	}
	if s.MaxAccountsPerIP < 0 {
		s.MaxAccountsPerIP = 0
	}
	if s.MaxAccountsPerIP > SupplyOnboardingMaxAccountsPerIPMax {
		s.MaxAccountsPerIP = SupplyOnboardingMaxAccountsPerIPMax
	}
}

// userCapEnabled 每人上限是否真的在起作用。
//
// 调用方用它决定「要不要为了这道闸发一次 COUNT」——闸关着的时候那次查询是纯浪费，
// 而接入是一条会被反复重试的路径。
func (s *SupplyOnboardingSettings) userCapEnabled() bool {
	return s != nil && s.MaxAccountsPerUser > 0
}

// ipCapEnabled 每 IP 上限是否真的在起作用。默认配置下它是关的，见文件头。
func (s *SupplyOnboardingSettings) ipCapEnabled() bool {
	return s != nil && s.MaxAccountsPerIP > 0
}

// userCapReached 判断「已经有 current 个号的人还能不能再挂一个」。
//
// 谓词而不是把 `>=` 散在调用点上：这两道闸各有一个「0 = 不限」的分支，
// 而 `current >= 0` 恒真——把这个比较抄到调用点，抄错一次的现象是
// **所有人都挂不上号**，且错的那一行看起来完全正常。
//
// 「关着」这个分支复用 userCapEnabled 而不是再写一遍 `<= 0`：这个判断在调用点上
// 还有第二个用途（省掉那次 COUNT），两处必须永远同义——不同义的后果是查了一遍
// 却不判断（白查），或者不查就判断（拿一个 0 去比上限，恒放行）。
func (s *SupplyOnboardingSettings) userCapReached(current int) bool {
	if !s.userCapEnabled() {
		return false
	}
	return current >= s.MaxAccountsPerUser
}

// ipCapReached 判断「这个来源已经挂了 current 个号，还能不能再挂一个」。
func (s *SupplyOnboardingSettings) ipCapReached(current int) bool {
	if !s.ipCapEnabled() {
		return false
	}
	return current >= s.MaxAccountsPerIP
}

// ============================================================================
// 进程内缓存。形态与 setting_supply_pool.go 一致，见那里的说明。
// ============================================================================

type cachedSupplyOnboardingSettings struct {
	settings  *SupplyOnboardingSettings
	expiresAt int64 // unix nano
}

var supplyOnboardingCache atomic.Value // *cachedSupplyOnboardingSettings
var supplyOnboardingSF singleflight.Group

const supplyOnboardingCacheTTL = 60 * time.Second
const supplyOnboardingErrorTTL = 5 * time.Second
const supplyOnboardingDBTimeout = 5 * time.Second

func invalidateSupplyOnboardingCache() {
	supplyOnboardingCache.Store(&cachedSupplyOnboardingSettings{})
	supplyOnboardingSF.Forget(SettingKeySupplyOnboarding)
}

// GetSupplyOnboardingSettings 读接入上限，永不返回错误。
func (s *SettingService) GetSupplyOnboardingSettings(ctx context.Context) *SupplyOnboardingSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyOnboardingSettings()
	}
	if cached, ok := supplyOnboardingCache.Load().(*cachedSupplyOnboardingSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyOnboardingSettings(cached.settings)
		}
	}

	result, err, _ := supplyOnboardingSF.Do(SettingKeySupplyOnboarding, func() (any, error) {
		if cached, ok := supplyOnboardingCache.Load().(*cachedSupplyOnboardingSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyOnboardingSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyOnboardingDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyOnboarding)
		if err != nil {
			settings := DefaultSupplyOnboardingSettings()
			ttl := supplyOnboardingErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyOnboardingCacheTTL
			} else {
				slog.Warn("[SupplyOnboarding] failed to read onboarding limits, falling back to defaults",
					"error", err, "key", SettingKeySupplyOnboarding)
			}
			storeSupplyOnboardingCache(settings, ttl)
			return cloneSupplyOnboardingSettings(settings), nil
		}

		settings := parseSupplyOnboardingSettings(raw)
		storeSupplyOnboardingCache(settings, supplyOnboardingCacheTTL)
		return cloneSupplyOnboardingSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyOnboardingSettings()
	}
	if settings, ok := result.(*SupplyOnboardingSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyOnboardingSettings()
}

// SetSupplyOnboardingSettings 写接入上限。
//
// 越界值**夹回区间**而不是报错，与观察期参数一致、与结算参数刻意不同：结算参数
// 越界改的是钱的分法，管理员必须知道自己填错了；这里越界改的是闸门宽窄，夹回一个
// 可用值再回读给他看更顺手。回读是接口契约的一部分——管理端把返回值写回表单，
// 所以他看到的一定是库里真正生效的那份。
func (s *SettingService) SetSupplyOnboardingSettings(ctx context.Context, settings *SupplyOnboardingSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	settings.normalize()

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply onboarding settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyOnboarding, string(data)); err != nil {
		return fmt.Errorf("save supply onboarding settings: %w", err)
	}
	invalidateSupplyOnboardingCache()
	return nil
}

// parseSupplyOnboardingSettings 解析库里那份 JSON。
//
// 解析失败退回默认值而不是"不限"：一份坏掉的配置不该被读成一道敞开的门。
// 每 IP 那一项的默认值本来就是不限，所以这条回退的实际效果只作用在每人上限上——
// 那正是我们希望在配置不可读时仍然保留的那道闸。
func parseSupplyOnboardingSettings(raw string) *SupplyOnboardingSettings {
	settings := DefaultSupplyOnboardingSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyOnboardingSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyOnboarding] onboarding settings JSON is corrupt, falling back to defaults",
			"error", err, "key", SettingKeySupplyOnboarding)
		return settings
	}
	parsed.normalize()
	return &parsed
}

func storeSupplyOnboardingCache(settings *SupplyOnboardingSettings, ttl time.Duration) {
	supplyOnboardingCache.Store(&cachedSupplyOnboardingSettings{
		settings:  cloneSupplyOnboardingSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

func cloneSupplyOnboardingSettings(settings *SupplyOnboardingSettings) *SupplyOnboardingSettings {
	if settings == nil {
		return DefaultSupplyOnboardingSettings()
	}
	clone := *settings
	return &clone
}
