// APEXONE-EXT: 双边市场——定价参数的实测读数。
//
// 这个文件回答一个问题：**倍率和分成配得对不对**。
//
// 定价方案里的每一个数（供给者能赚多少、平台留多少、要招几个供给者才能养活
// 一个兜底账号）都压在一个假设上：一个订阅账号一个月能产出多少「官方牌价等值」
// 的用量。那个数是估的，而且只能靠跑一个月真实流量测出来。这里把它测出来。
//
// # 三组读数，各自回答一个不同的问题
//
//	钱     —— 流水、营收、分成、毛利。回答「这门生意此刻赚不赚钱」
//	自检   —— 实际生效的倍率/分成 vs 配置里写的。回答「配的东西真的在跑吗」
//	供给   —— 每个账号的产出。回答「供给者赚得到钱吗」，也就是上面那个假设的真值
//
// 第二组最容易被当成冗余，但它抓的是一类没有任何症状的事故：有消费者的密钥绑在
// 别的分组上（那个组还是 1.0 倍率），于是「我明明配了 1.8 折」和账面对不上，
// 而两边各自看都正常。实际生效值是**从钱本身反推**的，配置读的是设置——
// 两个来源不一致时，钱那边才是真的。
//
// # 为什么用中位数而不是平均值
//
// 供给账号的产出分布极不均匀（一两个重度账号能占掉大半流量）。平均值会把
// 「大多数供给者其实赚不到钱」这件事藏起来，而那正是供给流失的前兆。
package service

import (
	"context"
	"sort"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	supplyHealthDefaultWindowDays = 30
	supplyHealthMaxWindowDays     = 90
)

// SupplyMarketHealth 是定价与供给健康度的全部读数。
type SupplyMarketHealth struct {
	// WindowDays 统计窗口。默认 30 天——定价参数不该按日波动去调。
	WindowDays int `json:"window_days"`

	// ---- 钱 ----

	// ListValue 窗口内的官方牌价等值流水（Σ usage_logs.total_cost）。
	//
	// 这是「如果按官方 API 价收费，这些用量值多少钱」，是所有比率的分母。
	ListValue float64 `json:"list_value"`
	// Revenue 平台营收 = 消费者实付（Σ usage_logs.actual_cost）。
	//
	// 与 ListValue 分开报是必须的：两者差着一个倍率，而把 total_cost 当营收
	// 会把收入虚报 5 倍以上（0.18 倍率下正好 5.6 倍）。
	Revenue float64 `json:"revenue"`
	// SupplierPayout 窗口内付给供给者的分成合计。
	SupplierPayout float64 `json:"supplier_payout"`
	// GrossMargin 平台毛利 = Revenue − SupplierPayout。
	//
	// **不含**兜底订阅费与基础设施——那些是固定成本，不在这张表的口径里。
	// 判断"够不够覆盖固定成本"要拿这个数去和月度固定支出比。
	GrossMargin float64 `json:"gross_margin"`

	// ---- 配置自检 ----

	// EffectiveMultiplier 实际生效的倍率 = Revenue ÷ ListValue。
	//
	// 与 ConfiguredMultiplier 对不上时**信这个**：它是从真实扣款反推的。
	// 常见成因是有消费者的密钥绑在别的分组上。
	EffectiveMultiplier float64 `json:"effective_multiplier"`
	// ConfiguredMultiplier 供给池分组此刻配的倍率。0 = 没配供给池。
	ConfiguredMultiplier float64 `json:"configured_multiplier"`
	// EffectiveShare 实际生效的分成 = Σ ledger.amount ÷ Σ ledger.basis_amount。
	EffectiveShare float64 `json:"effective_share"`
	// ConfiguredShare 结算设置里此刻配的分成。
	ConfiguredShare float64 `json:"configured_share"`

	// ---- 兜底 ----

	// OverflowListValue 由**平台自有账号**（owner_user_id IS NULL）承接的牌价等值。
	OverflowListValue float64 `json:"overflow_list_value"`
	// OverflowShare 兜底承接比例 = OverflowListValue ÷ ListValue。
	//
	// 持续偏高说明共享供给不足——该去拉供给者，而不是加兜底账号。
	OverflowShare float64 `json:"overflow_share"`
	// ExhaustedToday 当日「溢出了但兜底池也空了」的次数（迁移 236）。
	//
	// 这是**唯一**该据以增加兜底账号的信号：它数的是消费者真的拿到了
	// "No available accounts"。与 OverflowShare 是两回事——后者高只说明
	// 兜底在被用，前者非零才说明兜底不够用。
	ExhaustedToday int64 `json:"exhausted_today"`

	// ---- 供给 ----

	// SupplyAccounts 供给账号产出榜，按窗口内产出降序。只含他人挂的号。
	SupplyAccounts []SupplyAccountOutput `json:"supply_accounts"`
	// MedianMonthlyOutput 供给账号折月产出的**中位数**（牌价等值）。
	//
	// 定价方案的核心假设（Max 20× 约 $3000/月）就是拿这个数来证伪的。
	// 用中位数而不是平均值：见文件头。
	MedianMonthlyOutput float64 `json:"median_monthly_output"`
	// SupplierCount 窗口内有过产出的供给者人数。
	SupplierCount int `json:"supplier_count"`
}

// SupplyAccountOutput 是产出榜上的一行。
type SupplyAccountOutput struct {
	AccountID int64  `json:"account_id"`
	Name      string `json:"name"`
	// OwnerUserID 挂号的人。榜上只有他人挂的号，所以恒非零。
	OwnerUserID int64 `json:"owner_user_id"`
	// ListValue 窗口内产出的牌价等值。
	ListValue float64 `json:"list_value"`
	// MonthlyOutput 折算成 30 天的产出——直接拿去和产能估算比。
	MonthlyOutput float64 `json:"monthly_output"`
	// SupplierEarned 这个账号让它的主人赚到了多少。
	SupplierEarned float64 `json:"supplier_earned"`
	// Requests 窗口内服务的请求数。与产出一起看能分辨「请求多但都是小请求」。
	Requests int64 `json:"requests"`
}

// SupplyMarketHealthRepository 是读侧存储接口，实现在 repository（raw SQL 聚合）。
type SupplyMarketHealthRepository interface {
	// Aggregate 一次取齐窗口内的全部读数。
	//
	// 一次而不是拆成多个方法：这些数字互为分母（比率全是它们算出来的），
	// 分几次读会让分子分母来自不同时刻，于是比率可以算出大于 1 的值。
	Aggregate(ctx context.Context, windowDays int) (*SupplyMarketHealth, error)
}

// supplyHealthSettingsReader 只读定价相关的两组配置，用于自检对照。
type supplyHealthSettingsReader interface {
	GetSupplierSettlementSettings(ctx context.Context) *SupplierSettlementSettings
	GetSupplyPoolSettings(ctx context.Context) *SupplyPoolSettings
	GetSupplyOverflowUsage(ctx context.Context) *SupplyOverflowUsage
}

// supplyHealthGroupReader 只读一个分组的倍率。
type supplyHealthGroupReader interface {
	GetByID(ctx context.Context, id int64) (*Group, error)
}

// SupplyMarketHealthService 组装健康度读数。
type SupplyMarketHealthService struct {
	repo     SupplyMarketHealthRepository
	settings supplyHealthSettingsReader
	groups   supplyHealthGroupReader
}

// NewSupplyMarketHealthService 构造健康度服务。groups 可为 nil（自检那一项留零）。
func NewSupplyMarketHealthService(
	repo SupplyMarketHealthRepository,
	settingService *SettingService,
	groupRepo GroupRepository,
) *SupplyMarketHealthService {
	s := &SupplyMarketHealthService{repo: repo}
	// 显式判 nil 再赋值：装着 nil 指针的非 nil 接口会让下面的判断失效。
	if settingService != nil {
		s.settings = settingService
	}
	if groupRepo != nil {
		s.groups = groupRepo
	}
	return s
}

// Get 读一份健康度快照。
//
// 配置侧读不到时**不报错**，只把对照项留零：这是一块给人看的经营读数，
// 让整个面板因为一次设置读失败而打不开是不成比例的。真正不能缺的是聚合本身。
func (s *SupplyMarketHealthService) Get(ctx context.Context, windowDays int) (*SupplyMarketHealth, error) {
	if s == nil || s.repo == nil {
		return nil, infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "supply market health service unavailable")
	}
	windowDays = clampSupplyHealthWindow(windowDays)

	health, err := s.repo.Aggregate(ctx, windowDays)
	if err != nil {
		return nil, err
	}
	if health == nil {
		health = &SupplyMarketHealth{}
	}
	health.WindowDays = windowDays

	// 派生量在服务层算，不在 SQL 里：除以零的分支在 Go 里看得见，
	// 而 SQL 的 NULLIF 会把「没有流水」和「倍率是 0」返回成同一个 NULL。
	health.GrossMargin = health.Revenue - health.SupplierPayout
	health.EffectiveMultiplier = safeRatio(health.Revenue, health.ListValue)
	health.OverflowShare = safeRatio(health.OverflowListValue, health.ListValue)
	health.MedianMonthlyOutput = medianMonthlyOutput(health.SupplyAccounts)

	s.applyConfiguredValues(ctx, health)
	return health, nil
}

// applyConfiguredValues 填自检对照项。每一项独立 fail-open。
func (s *SupplyMarketHealthService) applyConfiguredValues(ctx context.Context, health *SupplyMarketHealth) {
	if s.settings == nil {
		return
	}
	if settlement := s.settings.GetSupplierSettlementSettings(ctx); settlement != nil {
		health.ConfiguredShare = settlement.ShareRatio
	}
	if usage := s.settings.GetSupplyOverflowUsage(ctx); usage != nil {
		health.ExhaustedToday = usage.ExhaustedCount
	}

	pool := s.settings.GetSupplyPoolSettings(ctx)
	if pool == nil || pool.SupplyGroupID <= 0 || s.groups == nil {
		return
	}
	group, err := s.groups.GetByID(ctx, pool.SupplyGroupID)
	if err != nil || group == nil {
		return
	}
	health.ConfiguredMultiplier = group.RateMultiplier
}

// clampSupplyHealthWindow 越界夹取而不是报错。
//
// 与设置类参数（越界报错，见 §3.7）方向相反，理由是这里的越界来自 URL 查询串，
// 而一个手敲 `?window_days=999` 的人要的是"给我看长一点"，不是一个 400。
func clampSupplyHealthWindow(windowDays int) int {
	if windowDays <= 0 {
		return supplyHealthDefaultWindowDays
	}
	if windowDays > supplyHealthMaxWindowDays {
		return supplyHealthMaxWindowDays
	}
	return windowDays
}

// safeRatio 算比率，分母为零时给 0 而不是 NaN。
//
// NaN 会一路走到 JSON 序列化那里炸掉（encoding/json 拒绝 NaN），于是
// 「今天还没有任何流水」这个完全正常的状态会让整个面板返回 500。
func safeRatio(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

// medianMonthlyOutput 取折月产出的中位数。空榜返回 0。
//
// 就地排序一个副本：入参那个切片还要按产出降序发给前端，
// 而中位数要的是同一组数——排乱了榜单顺序，前端那张表就变成随机序。
func medianMonthlyOutput(accounts []SupplyAccountOutput) float64 {
	if len(accounts) == 0 {
		return 0
	}
	values := make([]float64, len(accounts))
	for i, account := range accounts {
		values[i] = account.MonthlyOutput
	}
	sort.Float64s(values)

	middle := len(values) / 2
	if len(values)%2 == 1 {
		return values[middle]
	}
	return (values[middle-1] + values[middle]) / 2
}
