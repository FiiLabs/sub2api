// APEXONE-EXT: 双边市场——供给激励（挂号奖励）的规则配置。
//
// 第七个 settings key。分家的理由与前六个一样（见 setting_supply_pool.go 头部）：
// 这一组因第七种原因变动——结算参数动的是分成，池配置动的是路由，观察期动的是准入，
// 提现动的是出口，而这里动的是**平台主动花钱去买供给**。
//
// # 这个 key 与提现那组是同一个脾气：写路径严、读路径松
//
// 它决定平台往外发多少钱，所以两个方向的容错刻意相反：
//
//   - **写路径拒绝**结构性错误（slug 非法、没有档位、金额超上限）。静默夹回在这里是
//     危险的——管理员想填 $50 结果填成 $500，夹回到上限 $500 他不会察觉，而钱已经在发了。
//   - **读路径夹回/丢弃**。一份被手工改坏的 JSON（amount_usd: 99999）不该让 worker
//     按它发钱。读不到、解析失败一律退回「没有任何活动」，与其他六个 key 的 fail-closed 同向。
//
// # 为什么只有 7 个字段
//
// 早期设计有 20 个（终身上限、预算、统计窗口、起止时间、冻结小时数、告警邮箱……），
// 砍到 7 个的原则是：**能推导的不配、只有一个合理值的写死、已有 key 能复用的不重建**。
//
//	预算      = Σ(slots × amount)，是结构性硬顶，配了反而多一个会对不上的数
//	冻结窗    复用 supplier_settlement_settings.freeze_hours
//	告警邮箱  复用 supply_withdrawal_settings.notify_emails
//	终身上限  写死成「一个账号每档只发一次」，由幂等键 + 唯一索引兜底，比配数字更严
//	起止时间  常开设计，收口靠 slots 与 Enabled
//
// 门槛用「累计在线天数」而不是「累计产出」，是一次刻意的取舍：产出门槛能让奖励自付
// （平台从该产出里留存的钱多于发出的奖励），经济上更优，但共享者看不懂，而且产出多少
// 不由他控制、由调度控制。在线天数是他唯一能控制的量，也是邮件里一句话能讲清的量。
// 代价是奖励不再自付，因此**每一档都必须有名额**——那是这个设计里唯一的成本闸门。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// SettingKeySupplyIncentive 供给激励规则的 settings key。
const SettingKeySupplyIncentive = "supply_incentive_settings"

// 结构上限。设上限的理由与提现那组的起提额一样：挡住「多打一个零」。
const (
	// SupplyIncentiveProgramsMax 最多几个活动并存。
	//
	// 首版只有一个（Claude 与 ChatGPT 共用一个名额池）。留 5 个是给「按平台拆开」
	// 和「开第二期」用的，再多只会让名额分布没人算得清。
	SupplyIncentiveProgramsMax = 5
	// SupplyIncentiveTiersMax 单个活动最多几档。
	SupplyIncentiveTiersMax = 5
	// SupplyIncentiveSlugMaxLen slug 长度上限。
	//
	// 不是美观问题：slug 进幂等键 `cmp:<slug>:t<i>:a<accountID>`，而
	// supplier_credit_ledger.request_id 是 VARCHAR(64)。12 + 8 位账号 id 留足余量。
	SupplyIncentiveSlugMaxLen = 12
	// SupplyIncentiveAmountMaxUSD 单档奖励金额上限。
	//
	// 一档 $500 已经远超任何合理的挂号奖励（单个共享账号满额月分成也就 $400 量级）。
	// 这个上限存在的意义只有一个：填错一位数时拦下来。
	SupplyIncentiveAmountMaxUSD = 500
	// SupplyIncentiveSlotsMax 单档名额上限。
	SupplyIncentiveSlotsMax = 100
	// SupplyIncentiveMinActiveDaysMax 在线天数门槛上限（一年）。
	//
	// 比这更长的门槛在运营上等于「不打算发」，那应该关掉活动而不是设一个天文数字。
	SupplyIncentiveMinActiveDaysMax = 365
)

var (
	// ErrSupplyIncentiveInvalidSlug slug 不合法。
	ErrSupplyIncentiveInvalidSlug = errors.New("program slug must be 1-12 chars of [a-z0-9]")
	// ErrSupplyIncentiveDuplicateSlug 两个活动用了同一个 slug。
	//
	// 必须拒绝而不是去重：slug 是幂等键的一部分，两个活动同 slug 意味着它们的
	// 发放记录会互相「已经发过了」，后配的那个永远发不出钱，而现象是「配了没反应」。
	ErrSupplyIncentiveDuplicateSlug = errors.New("program slug must be unique")
	// ErrSupplyIncentiveNoTiers 活动没有任何档位。
	ErrSupplyIncentiveNoTiers = errors.New("program must have at least one tier")
	// ErrSupplyIncentiveTooManyPrograms 活动数超上限。
	ErrSupplyIncentiveTooManyPrograms = fmt.Errorf("at most %d programs", SupplyIncentiveProgramsMax)
	// ErrSupplyIncentiveTooManyTiers 档位数超上限。
	ErrSupplyIncentiveTooManyTiers = fmt.Errorf("at most %d tiers per program", SupplyIncentiveTiersMax)
	// ErrSupplyIncentiveAmountOutOfRange 金额越界。
	ErrSupplyIncentiveAmountOutOfRange = fmt.Errorf("tier amount must be within (0, %d]", SupplyIncentiveAmountMaxUSD)
	// ErrSupplyIncentiveSlotsOutOfRange 名额越界。
	ErrSupplyIncentiveSlotsOutOfRange = fmt.Errorf("tier slots must be within [0, %d]", SupplyIncentiveSlotsMax)
	// ErrSupplyIncentiveDaysOutOfRange 天数门槛越界。
	ErrSupplyIncentiveDaysOutOfRange = fmt.Errorf("tier min_active_days must be within [1, %d]", SupplyIncentiveMinActiveDaysMax)
	// ErrSupplyIncentiveDuplicateDays 同一活动里两档的天数门槛相同。
	ErrSupplyIncentiveDuplicateDays = errors.New("tier min_active_days must be distinct within a program")
	// ErrSupplyIncentiveUnknownPlatform platform 不是已知平台。
	ErrSupplyIncentiveUnknownPlatform = errors.New("unknown platform")
	// ErrSupplyIncentiveInvalidStartAt start_at 不是一个 YYYY-MM-DD。
	ErrSupplyIncentiveInvalidStartAt = errors.New("program start_at must be a UTC date (YYYY-MM-DD)")
	// ErrSupplyIncentiveBackdatedStartAt start_at 早于今天。
	//
	// 必须拒绝而不是夹到今天：夹回去之后管理员以为自己配的是过去那个日期，
	// 而实际起算点是今天，差额是要发出去的钱。理由与金额越界不夹回是同一条。
	ErrSupplyIncentiveBackdatedStartAt = errors.New(
		"program start_at cannot be in the past: online-day buckets accrue forward and cannot be rebuilt")
)

// SupplyIncentiveTier 是一个档位：挂满 N 天，发 X 美元，限 S 个名额。
type SupplyIncentiveTier struct {
	// MinActiveDays 累计在线天数门槛。
	//
	// 「在线」= 账号 supply_state=active 且 schedulable。计数器由每日 worker 累加，
	// **断线期间不涨、接回来继续涨、不清零**（见 supplier_incentive_worker.go）。
	MinActiveDays int `json:"min_active_days"`
	// AmountUSD 该档奖励金额。**逐档累加**：跨过第三档的账号拿到的是前三档之和。
	AmountUSD float64 `json:"amount_usd"`
	// Slots 该档名额（按**账号**计，不是按人）。0 = 不限。
	//
	// 门槛改成「在线天数」之后，奖励不再被产出自付，名额是唯一的成本闸门。
	// 把某一档设成 0 之前先算清楚：那一档的敞口从此没有上界。
	Slots int `json:"slots"`
}

// SupplyIncentiveProgram 是一期活动。
type SupplyIncentiveProgram struct {
	// Slug 活动标识，进幂等键，**建后不可改**（改了等于新活动，已发过的人会再领一次）。
	Slug string `json:"slug"`
	// Platform 限定平台（anthropic / openai / …）。空 = 全平台共用一套档位与名额池。
	Platform string `json:"platform,omitempty"`
	// StartAt 活动起算日（UTC 日期串 YYYY-MM-DD）。**必填**。
	//
	// # 它是这期活动自己的时间原点
	//
	// 在线天数按活动分桶计数（见 SupplyActiveDaysExtraKey），这个日期决定
	// 「哪一天开始给这期的桶 +1」。于是每期活动天然从 0 开始，第二期不会把
	// 第一期攒下的天数算进来——这正是能连着办几期的前提。
	//
	// # 为什么不允许回填
	//
	// 桶是随着日子一天天累加出来的，历史重建不出来。接受一个过去的日期只会得到
	// 「配了但那段时间没人涨天数」，而现象是活动看起来生效了、却没有人够格——
	// 最难被发现的那种错。所以写路径直接拒绝（见 validateAgainst）。
	//
	// 与 Enabled 的分工：这个决定**从哪天开始算天数**，Enabled 决定**发不发钱**。
	// 两者刻意独立——可以先配好 StartAt 让天数跑起来，等文案定稿再开 Enabled，
	// 届时此前累计的天数照数。
	StartAt string `json:"start_at"`
	// NewUsersOnly 是否只发给新供给者。
	//
	// 新 = 这个人在 StartAt 之前**名下没有任何供给账号**，含已解绑的。
	// 「含已解绑」不是苛刻：不含的话，「先解绑再挂回来」就能把自己洗成新用户，
	// 而那正是 ExcludedUserIDs 那条注释里已经在堵的同一个漏洞。
	//
	// 做成 per-program 而不是全局：拉新和普惠是两种活动，同一套代码要都能表达。
	NewUsersOnly bool `json:"new_users_only,omitempty"`
	// Tiers 档位表，按 MinActiveDays 升序（normalize 会排好）。
	Tiers []SupplyIncentiveTier `json:"tiers"`
}

// SupplyIncentiveDayFormat 活动起算日与在线天数桶共用的日期格式（UTC）。
const SupplyIncentiveDayFormat = "2006-01-02"

// StartDay 解析 StartAt。第二个返回值为 false 表示这期活动不该被计数或发放。
func (p SupplyIncentiveProgram) StartDay() (time.Time, bool) {
	day, err := time.ParseInLocation(SupplyIncentiveDayFormat, strings.TrimSpace(p.StartAt), time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}

// Started 这期活动在 now（UTC）这一天是否已经开始。
func (p SupplyIncentiveProgram) Started(now time.Time) bool {
	day, ok := p.StartDay()
	if !ok {
		return false
	}
	return !now.UTC().Truncate(24 * time.Hour).Before(day)
}

// SupplyIncentiveSettings 是供给激励的全部可配内容。
type SupplyIncentiveSettings struct {
	// Enabled 总开关。关闭时 worker 不发任何奖励，但**在线天数照常累加**——
	// 计数器是账号的客观属性，不该因为活动关了就停摆，否则活动一开一关，
	// 所有人的天数都要从头再来。
	Enabled bool `json:"enabled"`
	// Programs 并存的活动列表。空 = 没有任何活动。
	Programs []SupplyIncentiveProgram `json:"programs,omitempty"`
}

// DefaultSupplyIncentiveSettings 返回「没有任何活动」的默认配置。
func DefaultSupplyIncentiveSettings() *SupplyIncentiveSettings {
	return &SupplyIncentiveSettings{Enabled: false, Programs: nil}
}

// Active 活动是否真的会发钱。
func (s *SupplyIncentiveSettings) Active() bool {
	return s != nil && s.Enabled && len(s.Programs) > 0
}

// BudgetCapUSD 结构性预算上限 = Σ(slots × amount)。
//
// 不是配置项，是**算出来的**：任何一档 Slots=0（不限）时整体无上界，返回 (0, false)。
// 管理端把这个数显示在设置卡上，让人在保存前看见自己配了多大的敞口。
func (s *SupplyIncentiveSettings) BudgetCapUSD() (float64, bool) {
	if s == nil {
		return 0, true
	}
	total := 0.0
	for _, p := range s.Programs {
		for _, t := range p.Tiers {
			if t.Slots <= 0 {
				return 0, false
			}
			total += float64(t.Slots) * t.AmountUSD
		}
	}
	return total, true
}

// validateAgainst 写路径校验。结构性错误一律拒绝，理由见文件头。
//
// 需要 prev 与 today 两个额外入参，只为了 start_at 那一条：**回填要拒绝，但改别的
// 字段不能被连坐**。一期已经开跑的活动，管理员多半会回来调 slots；那时 payload 里
// 带的 start_at 必然是过去的日期，如果一律按回填拒绝，活动一开始就再也改不动了。
// 所以判据是「这个 slug 的 start_at 有没有**变**」，不是「它是不是过去」。
func (s *SupplyIncentiveSettings) validateAgainst(prev *SupplyIncentiveSettings, today time.Time) error {
	if s == nil {
		return errors.New("settings cannot be nil")
	}
	if len(s.Programs) > SupplyIncentiveProgramsMax {
		return ErrSupplyIncentiveTooManyPrograms
	}
	prevStart := map[string]string{}
	if prev != nil {
		for _, p := range prev.Programs {
			prevStart[p.Slug] = strings.TrimSpace(p.StartAt)
		}
	}
	today = today.UTC().Truncate(24 * time.Hour)

	seenSlug := make(map[string]struct{}, len(s.Programs))
	for i := range s.Programs {
		p := &s.Programs[i]
		p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
		p.Platform = strings.ToLower(strings.TrimSpace(p.Platform))
		p.StartAt = strings.TrimSpace(p.StartAt)

		if !isValidIncentiveSlug(p.Slug) {
			return fmt.Errorf("%w: %q", ErrSupplyIncentiveInvalidSlug, p.Slug)
		}

		if _, dup := seenSlug[p.Slug]; dup {
			return fmt.Errorf("%w: %q", ErrSupplyIncentiveDuplicateSlug, p.Slug)
		}
		seenSlug[p.Slug] = struct{}{}

		// 起算日的两条校验排在身份（slug 合法、不重复）之后：slug 是这份配置的
		// 主键，它错的时候报别的字段只会让人找错地方。
		startDay, ok := p.StartDay()
		if !ok {
			return fmt.Errorf("%w: %q got %q", ErrSupplyIncentiveInvalidStartAt, p.Slug, p.StartAt)
		}
		// 只有**新配的活动或改过起算日的活动**才查回填。一期已经开跑的活动，管理员
		// 回来调 slots 时 payload 里带的必然是过去的日期，一律拒绝会让活动一开始
		// 就再也改不动了。
		if before, exists := prevStart[p.Slug]; !exists || before != p.StartAt {
			if startDay.Before(today) {
				return fmt.Errorf("%w: %q got %q", ErrSupplyIncentiveBackdatedStartAt, p.Slug, p.StartAt)
			}
		}

		if p.Platform != "" && !isKnownIncentivePlatform(p.Platform) {
			return fmt.Errorf("%w: %q", ErrSupplyIncentiveUnknownPlatform, p.Platform)
		}
		if len(p.Tiers) == 0 {
			return fmt.Errorf("%w: %q", ErrSupplyIncentiveNoTiers, p.Slug)
		}
		if len(p.Tiers) > SupplyIncentiveTiersMax {
			return fmt.Errorf("%w: %q", ErrSupplyIncentiveTooManyTiers, p.Slug)
		}

		seenDays := make(map[int]struct{}, len(p.Tiers))
		for _, t := range p.Tiers {
			if t.MinActiveDays < 1 || t.MinActiveDays > SupplyIncentiveMinActiveDaysMax {
				return fmt.Errorf("%w: %q", ErrSupplyIncentiveDaysOutOfRange, p.Slug)
			}
			if _, dup := seenDays[t.MinActiveDays]; dup {
				return fmt.Errorf("%w: %q day %d", ErrSupplyIncentiveDuplicateDays, p.Slug, t.MinActiveDays)
			}
			seenDays[t.MinActiveDays] = struct{}{}

			if t.AmountUSD <= 0 || t.AmountUSD > SupplyIncentiveAmountMaxUSD {
				return fmt.Errorf("%w: %q day %d", ErrSupplyIncentiveAmountOutOfRange, p.Slug, t.MinActiveDays)
			}
			if t.Slots < 0 || t.Slots > SupplyIncentiveSlotsMax {
				return fmt.Errorf("%w: %q day %d", ErrSupplyIncentiveSlotsOutOfRange, p.Slug, t.MinActiveDays)
			}
		}
		sortIncentiveTiers(p.Tiers)
	}
	return nil
}

// normalize 读路径夹回：把一份手工改坏的 JSON 收拾成「可以照着发钱」的样子。
//
// 与 validate 的分工是刻意的——这里**丢弃**不合法的部分而不是报错，因为读路径没有
// 报错的去处，而「整份配置读不出来就当没有活动」比「按一份坏配置发钱」安全得多。
// 具体到每一条：越界的金额夹回上限而不是丢弃该档（丢弃会让后面的档静默变成第一档），
// 非法的 program 整个丢掉（它的 slug 进幂等键，收拾不出一个安全的替代值）。
func (s *SupplyIncentiveSettings) normalize() {
	if s == nil {
		return
	}
	if len(s.Programs) > SupplyIncentiveProgramsMax {
		s.Programs = s.Programs[:SupplyIncentiveProgramsMax]
	}

	kept := make([]SupplyIncentiveProgram, 0, len(s.Programs))
	seenSlug := make(map[string]struct{}, len(s.Programs))
	for _, p := range s.Programs {
		p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
		p.Platform = strings.ToLower(strings.TrimSpace(p.Platform))
		p.StartAt = strings.TrimSpace(p.StartAt)
		if !isValidIncentiveSlug(p.Slug) {
			slog.Warn("[SupplyIncentive] dropping program with invalid slug", "slug", p.Slug)
			continue
		}
		// 起算日读不出来的活动整个丢掉，与 slug 非法同样处理：没有时间原点就
		// 没法判断「这期跑了几天」，而**猜**一个原点会直接决定发多少钱。
		// 这也是本功能上线前写进去的旧配置（没有 start_at）的归宿——
		// fail-closed，管理员重新配一次即可。
		if _, ok := p.StartDay(); !ok {
			slog.Warn("[SupplyIncentive] dropping program with invalid start_at",
				"slug", p.Slug, "start_at", p.StartAt)
			continue
		}
		if _, dup := seenSlug[p.Slug]; dup {
			slog.Warn("[SupplyIncentive] dropping program with duplicate slug", "slug", p.Slug)
			continue
		}
		if p.Platform != "" && !isKnownIncentivePlatform(p.Platform) {
			slog.Warn("[SupplyIncentive] dropping program with unknown platform",
				"slug", p.Slug, "platform", p.Platform)
			continue
		}
		if len(p.Tiers) > SupplyIncentiveTiersMax {
			p.Tiers = p.Tiers[:SupplyIncentiveTiersMax]
		}

		tiers := make([]SupplyIncentiveTier, 0, len(p.Tiers))
		seenDays := make(map[int]struct{}, len(p.Tiers))
		for _, t := range p.Tiers {
			if t.MinActiveDays < 1 {
				t.MinActiveDays = 1
			}
			if t.MinActiveDays > SupplyIncentiveMinActiveDaysMax {
				t.MinActiveDays = SupplyIncentiveMinActiveDaysMax
			}
			if _, dup := seenDays[t.MinActiveDays]; dup {
				continue
			}
			if t.AmountUSD <= 0 {
				continue
			}
			if t.AmountUSD > SupplyIncentiveAmountMaxUSD {
				t.AmountUSD = SupplyIncentiveAmountMaxUSD
			}
			if t.Slots < 0 {
				t.Slots = 0
			}
			if t.Slots > SupplyIncentiveSlotsMax {
				t.Slots = SupplyIncentiveSlotsMax
			}
			seenDays[t.MinActiveDays] = struct{}{}
			tiers = append(tiers, t)
		}
		if len(tiers) == 0 {
			slog.Warn("[SupplyIncentive] dropping program with no usable tier", "slug", p.Slug)
			continue
		}
		sortIncentiveTiers(tiers)
		p.Tiers = tiers
		seenSlug[p.Slug] = struct{}{}
		kept = append(kept, p)
	}
	if len(kept) == 0 {
		s.Programs = nil
		return
	}
	s.Programs = kept
}

// sortIncentiveTiers 按天数升序。档位顺序决定幂等键里的下标 t<i>，
// **排序必须在写库之前完成**——否则同一份配置换个书写顺序，同一个档位就换了 key，
// 已经发过的人会再领一次。
func sortIncentiveTiers(tiers []SupplyIncentiveTier) {
	sort.SliceStable(tiers, func(i, j int) bool {
		return tiers[i].MinActiveDays < tiers[j].MinActiveDays
	})
}

func isValidIncentiveSlug(slug string) bool {
	if slug == "" || len(slug) > SupplyIncentiveSlugMaxLen {
		return false
	}
	for _, r := range slug {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// isKnownIncentivePlatform 只接受供给侧真正跑得起来的平台。
//
// 收窄到这两个不是保守：platform 会直接进 SQL 的 WHERE，写错一个词的现象是
// 「配了活动但一个人都没发」，而这种「静默无事发生」最难被发现。
func isKnownIncentivePlatform(platform string) bool {
	return platform == PlatformAnthropic || platform == PlatformOpenAI
}

// ============================================================================
// 进程内缓存。形态与 setting_supply_probation.go 一致。
// ============================================================================

type cachedSupplyIncentiveSettings struct {
	settings  *SupplyIncentiveSettings
	expiresAt int64 // unix nano
}

var supplyIncentiveCache atomic.Value // *cachedSupplyIncentiveSettings
var supplyIncentiveSF singleflight.Group

const supplyIncentiveCacheTTL = 60 * time.Second
const supplyIncentiveErrorTTL = 5 * time.Second
const supplyIncentiveDBTimeout = 5 * time.Second

func invalidateSupplyIncentiveCache() {
	supplyIncentiveCache.Store(&cachedSupplyIncentiveSettings{})
	supplyIncentiveSF.Forget(SettingKeySupplyIncentive)
}

// GetSupplyIncentiveSettings 读激励规则，永不返回错误。读不到 = 没有活动。
func (s *SettingService) GetSupplyIncentiveSettings(ctx context.Context) *SupplyIncentiveSettings {
	if s == nil || s.settingRepo == nil {
		return DefaultSupplyIncentiveSettings()
	}
	if cached, ok := supplyIncentiveCache.Load().(*cachedSupplyIncentiveSettings); ok {
		if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
			return cloneSupplyIncentiveSettings(cached.settings)
		}
	}

	result, err, _ := supplyIncentiveSF.Do(SettingKeySupplyIncentive, func() (any, error) {
		if cached, ok := supplyIncentiveCache.Load().(*cachedSupplyIncentiveSettings); ok {
			if cached != nil && cached.settings != nil && time.Now().UnixNano() < cached.expiresAt {
				return cloneSupplyIncentiveSettings(cached.settings), nil
			}
		}

		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), supplyIncentiveDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeySupplyIncentive)
		if err != nil {
			settings := DefaultSupplyIncentiveSettings()
			ttl := supplyIncentiveErrorTTL
			if errors.Is(err, ErrSettingNotFound) {
				ttl = supplyIncentiveCacheTTL
			} else {
				slog.Warn("[SupplyIncentive] failed to read incentive settings, no rewards will be granted",
					"error", err, "key", SettingKeySupplyIncentive)
			}
			storeSupplyIncentiveCache(settings, ttl)
			return cloneSupplyIncentiveSettings(settings), nil
		}

		settings := parseSupplyIncentiveSettings(raw)
		storeSupplyIncentiveCache(settings, supplyIncentiveCacheTTL)
		return cloneSupplyIncentiveSettings(settings), nil
	})
	if err != nil {
		return DefaultSupplyIncentiveSettings()
	}
	if settings, ok := result.(*SupplyIncentiveSettings); ok && settings != nil {
		return settings
	}
	return DefaultSupplyIncentiveSettings()
}

// SetSupplyIncentiveSettings 写激励规则。
//
// 与观察期那组（越界夹回）刻意不同：这里越界**直接拒绝**。理由见文件头——
// 夹回一个能用的值再回读，管理员会以为自己填的就是生效的那个，而中间差的是钱。
func (s *SettingService) SetSupplyIncentiveSettings(ctx context.Context, settings *SupplyIncentiveSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	// 先读一次现值：start_at 的回填校验要能分辨「新配的活动」与「调已有活动的档位」，
	// 见 validateAgainst 的注释。读失败时拿一份空的去比——那样所有 slug 都会被
	// 当成新的、必须填今天或以后，方向偏严，不会放过一个回填。
	if err := settings.validateAgainst(s.GetSupplyIncentiveSettings(ctx), time.Now()); err != nil {
		return err
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal supply incentive settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeySupplyIncentive, string(data)); err != nil {
		return fmt.Errorf("save supply incentive settings: %w", err)
	}
	invalidateSupplyIncentiveCache()
	return nil
}

func parseSupplyIncentiveSettings(raw string) *SupplyIncentiveSettings {
	settings := DefaultSupplyIncentiveSettings()
	if raw == "" {
		return settings
	}
	var parsed SupplyIncentiveSettings
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		slog.Warn("[SupplyIncentive] incentive settings JSON is corrupt, no rewards will be granted",
			"error", err, "key", SettingKeySupplyIncentive)
		return settings
	}
	parsed.normalize()
	return &parsed
}

func storeSupplyIncentiveCache(settings *SupplyIncentiveSettings, ttl time.Duration) {
	supplyIncentiveCache.Store(&cachedSupplyIncentiveSettings{
		settings:  cloneSupplyIncentiveSettings(settings),
		expiresAt: time.Now().Add(ttl).UnixNano(),
	})
}

// cloneSupplyIncentiveSettings 深拷贝。
//
// 与前六个 key 的 clone 不同，这里**必须逐层复制 slice**：Programs 和 Tiers 是引用，
// 浅拷贝会让调用方（管理端的表单回写、worker 的遍历）改到缓存里那一份，
// 下一个读到的人拿到的是被别人改过的规则——而那规则决定发多少钱。
func cloneSupplyIncentiveSettings(settings *SupplyIncentiveSettings) *SupplyIncentiveSettings {
	if settings == nil {
		return DefaultSupplyIncentiveSettings()
	}
	clone := &SupplyIncentiveSettings{Enabled: settings.Enabled}
	if len(settings.Programs) == 0 {
		return clone
	}
	clone.Programs = make([]SupplyIncentiveProgram, 0, len(settings.Programs))
	for _, p := range settings.Programs {
		cp := SupplyIncentiveProgram{
			Slug:         p.Slug,
			Platform:     p.Platform,
			StartAt:      p.StartAt,
			NewUsersOnly: p.NewUsersOnly,
		}
		if len(p.Tiers) > 0 {
			cp.Tiers = make([]SupplyIncentiveTier, len(p.Tiers))
			copy(cp.Tiers, p.Tiers)
		}
		clone.Programs = append(clone.Programs, cp)
	}
	return clone
}
