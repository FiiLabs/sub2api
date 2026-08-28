// APEXONE-EXT: 双边市场——结算参数与供给池路由的管理端读写。
//
// 挂在**已有的** `*SettingHandler` 上而不是新起一个 handler 类型：那样要动
// `AdminHandlers` 结构体、`ProvideAdminHandlers` 的形参表、admin ProviderSet 和
// wire_gen——四处上游合并热区，换来的只是一个新名字。方法挂在既有类型上，
// 本文件是纯新增，wire 层改动为零，路由层只多一行。
//
// 两组配置分成两对端点，与 setting_supply_pool.go 里「刻意分成两个 key」同一个理由：
// 改分成比例和改兜底池是两件不同的事，共用一个端点会让审计日志分不清谁改了什么。
package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SupplierSettlementSettingsResponse 是结算参数的对外形态。
//
// 单独定义而不是直接回 service 结构体：service 那个类型在计费热路径上被读，
// 给它加 json tag 之外的展示字段（比如下面的 *_max）会让热路径带上只有面板需要的东西。
type SupplierSettlementSettingsResponse struct {
	Enabled              bool    `json:"enabled"`
	ShareRatio           float64 `json:"share_ratio"`
	FreezeHours          int     `json:"freeze_hours"`
	SpendFromWalletFirst bool    `json:"spend_from_wallet_first"`

	// 边界值随配置一起下发，前端不必把这两个数抄一遍。抄一遍的下场是后端改了上限、
	// 前端还在按旧值拦，用户看到的是一个前端说不行、后端其实允许的值。
	ShareRatioMax  float64 `json:"share_ratio_max"`
	FreezeHoursMax int     `json:"freeze_hours_max"`
}

func newSupplierSettlementSettingsResponse(s *service.SupplierSettlementSettings) SupplierSettlementSettingsResponse {
	resp := SupplierSettlementSettingsResponse{
		ShareRatioMax:  service.SupplierShareRatioMax,
		FreezeHoursMax: service.SupplierFreezeHoursMax,
	}
	if s == nil {
		return resp
	}
	resp.Enabled = s.Enabled
	resp.ShareRatio = s.ShareRatio
	resp.FreezeHours = s.FreezeHours
	resp.SpendFromWalletFirst = s.SpendFromWalletFirst
	return resp
}

// GetSupplierSettlementSettings 读结算参数
// GET /api/v1/admin/settings/supplier-settlement
//
// service 侧这个 getter 永不返回错误（fail-closed，读不到就是「关闭」），
// 所以这里没有错误分支。面板上看到的「关闭」因此有两种成因——真的没配过，
// 或者读配置出错了——后者在服务端有 warn 日志。
func (h *SettingHandler) GetSupplierSettlementSettings(c *gin.Context) {
	settings := h.settingService.GetSupplierSettlementSettings(c.Request.Context())
	response.Success(c, newSupplierSettlementSettingsResponse(settings))
}

// UpdateSupplierSettlementSettingsRequest 更新结算参数请求。
//
// 全字段必传（不是 PATCH 语义）：这三个数必须一起看——比例调高而冻结窗不动
// 等于放大拒付敞口。部分更新会让运营在只想改一个数时，另两个数悄悄沿用了
// 他没看见的旧值。
type UpdateSupplierSettlementSettingsRequest struct {
	Enabled              bool    `json:"enabled"`
	ShareRatio           float64 `json:"share_ratio"`
	FreezeHours          int     `json:"freeze_hours"`
	SpendFromWalletFirst bool    `json:"spend_from_wallet_first"`
}

// UpdateSupplierSettlementSettings 写结算参数
// PUT /api/v1/admin/settings/supplier-settlement
//
// 不在这里重复 service 侧的区间校验：那边写路径已经对「开着开关却配了不可能的值」
// 直接报错（而不是像读路径那样 clamp）。在 handler 再抄一份区间，等于给同一条规则
// 立两个源头，改上限时必然漏掉一个。
func (h *SettingHandler) UpdateSupplierSettlementSettings(c *gin.Context) {
	var req UpdateSupplierSettlementSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &service.SupplierSettlementSettings{
		Enabled:              req.Enabled,
		ShareRatio:           req.ShareRatio,
		FreezeHours:          req.FreezeHours,
		SpendFromWalletFirst: req.SpendFromWalletFirst,
	}
	if err := h.settingService.SetSupplierSettlementSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	// 回读而不是回显请求体：写路径会 normalize（夹回区间），回显请求体会让面板显示
	// 一个数据库里并不存在的值。
	response.Success(c, newSupplierSettlementSettingsResponse(
		h.settingService.GetSupplierSettlementSettings(c.Request.Context())))
}

// SupplyPoolSettingsResponse 是供给池路由配置的对外形态。
//
// 配置与当日用量放在同一个响应里，是因为它们只有对照着看才有意义：单看「配额 500」
// 说明不了任何事，「配额 500 / 今天已用 487」才是一个要管理员做决定的画面。
type SupplyPoolSettingsResponse struct {
	Enabled            bool  `json:"enabled"`
	SupplyGroupID      int64 `json:"supply_group_id"`
	OverflowGroupID    int64 `json:"overflow_group_id"`
	DailyOverflowLimit int   `json:"daily_overflow_limit"`

	// 以下为只读用量，PUT 时会被忽略。
	UsageDay            string `json:"usage_day"`
	OverflowUsedToday   int64  `json:"overflow_used_today"`
	OverflowDeniedToday int64  `json:"overflow_denied_today"`
}

func newSupplyPoolSettingsResponse(s *service.SupplyPoolSettings, usage *service.SupplyOverflowUsage) SupplyPoolSettingsResponse {
	resp := SupplyPoolSettingsResponse{}
	if s != nil {
		resp.Enabled = s.Enabled
		resp.SupplyGroupID = s.SupplyGroupID
		resp.OverflowGroupID = s.OverflowGroupID
		resp.DailyOverflowLimit = s.DailyOverflowLimit
	}
	if usage != nil {
		resp.UsageDay = usage.Day
		resp.OverflowUsedToday = usage.OverflowCount
		resp.OverflowDeniedToday = usage.DeniedCount
	}
	return resp
}

// GetSupplyPoolSettings 读供给池路由配置
// GET /api/v1/admin/settings/supply-pool
func (h *SettingHandler) GetSupplyPoolSettings(c *gin.Context) {
	ctx := c.Request.Context()
	response.Success(c, newSupplyPoolSettingsResponse(
		h.settingService.GetSupplyPoolSettings(ctx),
		h.settingService.GetSupplyOverflowUsage(ctx),
	))
}

// UpdateSupplyPoolSettingsRequest 更新供给池路由配置请求。
//
// DailyOverflowLimit 用指针：0 是「不限量」这个有意义的取值，不是「没填」。
// 用值类型的话，任何漏传这个字段的旧前端都会把管理员配好的配额静默清成不限量。
type UpdateSupplyPoolSettingsRequest struct {
	Enabled            bool  `json:"enabled"`
	SupplyGroupID      int64 `json:"supply_group_id"`
	OverflowGroupID    int64 `json:"overflow_group_id"`
	DailyOverflowLimit *int  `json:"daily_overflow_limit"`
}

// UpdateSupplyPoolSettings 写供给池路由配置
// PUT /api/v1/admin/settings/supply-pool
//
// 这里同样不校验分组是否存在，理由见 setting_supply_pool.go：分组可能在配置之后
// 被删掉，配置侧的存在性校验给不出「以后也有效」的保证，真正的兜底在调度侧。
func (h *SettingHandler) UpdateSupplyPoolSettings(c *gin.Context) {
	var req UpdateSupplyPoolSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	ctx := c.Request.Context()
	settings := &service.SupplyPoolSettings{
		Enabled:            req.Enabled,
		SupplyGroupID:      req.SupplyGroupID,
		OverflowGroupID:    req.OverflowGroupID,
		DailyOverflowLimit: h.settingService.GetSupplyPoolSettings(ctx).DailyOverflowLimit,
	}
	if req.DailyOverflowLimit != nil {
		if *req.DailyOverflowLimit < 0 {
			response.BadRequest(c, "daily_overflow_limit cannot be negative")
			return
		}
		settings.DailyOverflowLimit = *req.DailyOverflowLimit
	}
	if err := h.settingService.SetSupplyPoolSettings(ctx, settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, newSupplyPoolSettingsResponse(
		h.settingService.GetSupplyPoolSettings(ctx),
		h.settingService.GetSupplyOverflowUsage(ctx),
	))
}

// SupplyProbationSettingsResponse 是观察期参数的对外形态。
type SupplyProbationSettingsResponse struct {
	Enabled               bool   `json:"enabled"`
	MinObservationMinutes int    `json:"min_observation_minutes"`
	RequiredSuccesses     int    `json:"required_successes"`
	ProbeIntervalMinutes  int    `json:"probe_interval_minutes"`
	ProbeModel            string `json:"probe_model"`
	DrainWindowMinutes    int    `json:"drain_window_minutes"`

	// 边界值随配置下发，理由同结算参数：前端抄一遍上限，后端改了就对不上。
	// 探测间隔的**下限**尤其要下发——它不是一个防呆值，它是「不要拿供给者的额度当
	// 探针耗材」这条规则的具体数字，运营需要在界面上看见它。
	MinObservationMinutesMax int `json:"min_observation_minutes_max"`
	RequiredSuccessesMax     int `json:"required_successes_max"`
	ProbeIntervalMinutesMin  int `json:"probe_interval_minutes_min"`
	ProbeIntervalMinutesMax  int `json:"probe_interval_minutes_max"`
	DrainWindowMinutesMax    int `json:"drain_window_minutes_max"`
}

func newSupplyProbationSettingsResponse(s *service.SupplyProbationSettings) SupplyProbationSettingsResponse {
	resp := SupplyProbationSettingsResponse{
		MinObservationMinutesMax: service.SupplyProbationMinObservationMinutesMax,
		RequiredSuccessesMax:     service.SupplyProbationRequiredSuccessesMax,
		ProbeIntervalMinutesMin:  service.SupplyProbationProbeIntervalMinutesMin,
		ProbeIntervalMinutesMax:  service.SupplyProbationProbeIntervalMinutesMax,
		DrainWindowMinutesMax:    service.SupplyProbationDrainWindowMinutesMax,
	}
	if s == nil {
		return resp
	}
	resp.Enabled = s.Enabled
	resp.MinObservationMinutes = s.MinObservationMinutes
	resp.RequiredSuccesses = s.RequiredSuccesses
	resp.ProbeIntervalMinutes = s.ProbeIntervalMinutes
	resp.ProbeModel = s.ProbeModel
	resp.DrainWindowMinutes = s.DrainWindowMinutes
	return resp
}

// GetSupplyProbationSettings 读观察期参数
// GET /api/v1/admin/settings/supply-probation
func (h *SettingHandler) GetSupplyProbationSettings(c *gin.Context) {
	settings := h.settingService.GetSupplyProbationSettings(c.Request.Context())
	response.Success(c, newSupplyProbationSettingsResponse(settings))
}

// UpdateSupplyProbationSettingsRequest 更新观察期参数请求。
type UpdateSupplyProbationSettingsRequest struct {
	Enabled               bool   `json:"enabled"`
	MinObservationMinutes int    `json:"min_observation_minutes"`
	RequiredSuccesses     int    `json:"required_successes"`
	ProbeIntervalMinutes  int    `json:"probe_interval_minutes"`
	ProbeModel            string `json:"probe_model"`
	DrainWindowMinutes    int    `json:"drain_window_minutes"`
}

// UpdateSupplyProbationSettings 写观察期参数
// PUT /api/v1/admin/settings/supply-probation
//
// 与结算参数不同，越界值在 service 侧被夹回区间而不是报错（理由见
// SetSupplyProbationSettings）。所以这里的回读不只是习惯——它是运营看到自己
// 填的 1 分钟变成 5 分钟的唯一途径。
func (h *SettingHandler) UpdateSupplyProbationSettings(c *gin.Context) {
	var req UpdateSupplyProbationSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &service.SupplyProbationSettings{
		Enabled:               req.Enabled,
		MinObservationMinutes: req.MinObservationMinutes,
		RequiredSuccesses:     req.RequiredSuccesses,
		ProbeIntervalMinutes:  req.ProbeIntervalMinutes,
		ProbeModel:            req.ProbeModel,
		DrainWindowMinutes:    req.DrainWindowMinutes,
	}
	if err := h.settingService.SetSupplyProbationSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, newSupplyProbationSettingsResponse(
		h.settingService.GetSupplyProbationSettings(c.Request.Context())))
}

// SupplyAgreementSettingsResponse 是供给者协议配置的对外形态。
type SupplyAgreementSettingsResponse struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Body    string `json:"body"`

	// Published 版本号是否非空。这就是「自助接入现在能不能用」的判据之一，
	// 让面板不必自己去判断空字符串的含义。
	Published bool `json:"published"`

	// 长度上限随配置下发，理由同结算参数的 *_max：前端抄一份就等于给同一条规则
	// 立了两个源头，而后端改了上限之后，用户会看到一个前端说不行、后端其实允许的值。
	VersionMaxLen int `json:"version_max_len"`
	URLMaxLen     int `json:"url_max_len"`
	BodyMaxLen    int `json:"body_max_len"`
}

func newSupplyAgreementSettingsResponse(s *service.SupplyAgreementSettings) SupplyAgreementSettingsResponse {
	resp := SupplyAgreementSettingsResponse{
		VersionMaxLen: service.SupplyAgreementVersionMaxLen,
		URLMaxLen:     service.SupplyAgreementURLMaxLen,
		BodyMaxLen:    service.SupplyAgreementBodyMaxLen,
	}
	if s == nil {
		return resp
	}
	resp.Version = s.Version
	resp.URL = s.URL
	resp.Body = s.Body
	resp.Published = s.Published()
	return resp
}

// GetSupplyAgreementSettings 读供给者协议配置
// GET /api/v1/admin/settings/supply-agreement
func (h *SettingHandler) GetSupplyAgreementSettings(c *gin.Context) {
	settings := h.settingService.GetSupplyAgreementSettings(c.Request.Context())
	response.Success(c, newSupplyAgreementSettingsResponse(settings))
}

// UpdateSupplyAgreementSettingsRequest 更新协议配置请求。
type UpdateSupplyAgreementSettingsRequest struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Body    string `json:"body"`
}

// UpdateSupplyAgreementSettings 写供给者协议配置
// PUT /api/v1/admin/settings/supply-agreement
//
// 与观察期参数不同，越界值在 service 侧**拒绝**而不是夹回：这是法律文本，
// 悄悄把正文截断或把版本号截短，比报一个错糟得多（理由见 setting_supply_agreement.go）。
//
// 改动 version 的后果要在面板上说清楚：那一刻起，所有人都得重新点一次同意才能接入
// 新号（存量号不受影响）。这里不做任何"确认"式的拦截——那是界面的事，不是接口的事。
func (h *SettingHandler) UpdateSupplyAgreementSettings(c *gin.Context) {
	var req UpdateSupplyAgreementSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	settings := &service.SupplyAgreementSettings{
		Version: req.Version,
		URL:     req.URL,
		Body:    req.Body,
	}
	if err := h.settingService.SetSupplyAgreementSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, newSupplyAgreementSettingsResponse(
		h.settingService.GetSupplyAgreementSettings(c.Request.Context())))
}

// SupplyWithdrawalSettingsResponse 是提现参数的对外形态。
type SupplyWithdrawalSettingsResponse struct {
	Enabled    bool    `json:"enabled"`
	MinAmount  float64 `json:"min_amount"`
	MaxPending int     `json:"max_pending"`
	Notice     string  `json:"notice"`

	// NotifyEmails 新申请到达时通知谁。见 service.SupplyWithdrawalSettings 的
	// 同名字段：这个列表为空 + 提现开着，是一个真实的坏状态。
	NotifyEmails []string `json:"notify_emails"`

	// Available 提现开关是否开着（M6b 起渠道由链上金库派生，这里只答开关；
	// 「真能提吗」看金库卡的 status）。
	Available bool `json:"available"`

	// 边界值随配置下发，理由同结算参数的 *_max。
	MinAmountMax  float64 `json:"min_amount_max"`
	MaxPendingCap int     `json:"max_pending_cap"`
	NoticeMaxLen  int     `json:"notice_max_len"`

	NotifyEmailsMax   int `json:"notify_emails_max"`
	NotifyEmailMaxLen int `json:"notify_email_max_len"`

	// NotifyConfigured 开着提现 **且** 至少配了一个收件人。与 Available 同一个
	// 用途：把一个只在运行期才会暴露的坏状态提前搬到设置页上。
	NotifyConfigured bool `json:"notify_configured"`
}

func newSupplyWithdrawalSettingsResponse(s *service.SupplyWithdrawalSettings) SupplyWithdrawalSettingsResponse {
	resp := SupplyWithdrawalSettingsResponse{
		MinAmountMax:  service.SupplyWithdrawalMinAmountMax,
		MaxPendingCap: service.SupplyWithdrawalMaxPendingCap,
		NoticeMaxLen:  service.SupplyWithdrawalNoticeMaxLen,

		NotifyEmailsMax:   service.SupplyWithdrawalNotifyEmailsMax,
		NotifyEmailMaxLen: service.SupplyWithdrawalNotifyEmailMaxLen,
		NotifyEmails:      []string{},
	}
	if s == nil {
		return resp
	}
	resp.Enabled = s.Enabled
	resp.MinAmount = s.MinAmount
	resp.MaxPending = s.MaxPending
	if len(s.NotifyEmails) > 0 {
		resp.NotifyEmails = append([]string(nil), s.NotifyEmails...)
	}
	resp.Notice = s.Notice
	resp.Available = s.Available()
	resp.NotifyConfigured = s.Enabled && len(s.NotifyEmails) > 0
	return resp
}

// GetSupplyWithdrawalSettings 读提现参数
// GET /api/v1/admin/settings/supply-withdrawal
func (h *SettingHandler) GetSupplyWithdrawalSettings(c *gin.Context) {
	settings := h.settingService.GetSupplyWithdrawalSettings(c.Request.Context())
	response.Success(c, newSupplyWithdrawalSettingsResponse(settings))
}

// UpdateSupplyWithdrawalSettingsRequest 更新提现参数请求。
//
// Channels 用 []string 而不是指针：一个不传 channels 的请求就是把渠道清空，
// 而清空渠道 = 关掉提现入口，这是一个运营应当能一步做到的动作。
type UpdateSupplyWithdrawalSettingsRequest struct {
	Enabled    bool    `json:"enabled"`
	MinAmount  float64 `json:"min_amount"`
	MaxPending int     `json:"max_pending"`
	Notice     string  `json:"notice"`
	// NotifyEmails 同理：不传等于清空收件人。
	NotifyEmails []string `json:"notify_emails"`
}

// UpdateSupplyWithdrawalSettings 写提现参数
// PUT /api/v1/admin/settings/supply-withdrawal
//
// 与协议那组一样，越界值在 service 侧**拒绝**而不是夹回：起提额被悄悄夹到上限，
// 结果是所有人都提不了钱，而面板上看不出任何异常。开着却不给一个渠道同样被拒——
// 那个组合唯一的效果是让供给者点一个必定失败的按钮。
func (h *SettingHandler) UpdateSupplyWithdrawalSettings(c *gin.Context) {
	var req UpdateSupplyWithdrawalSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Channels 结转库里现值：字段在 settings 里还活着（兼容旧 JSON），
	// 只是不再被读、也不再暴露编辑面——保存别把它洗掉。
	current := h.settingService.GetSupplyWithdrawalSettings(c.Request.Context())
	settings := &service.SupplyWithdrawalSettings{
		Enabled:      req.Enabled,
		MinAmount:    req.MinAmount,
		MaxPending:   req.MaxPending,
		Channels:     current.Channels,
		Notice:       req.Notice,
		NotifyEmails: req.NotifyEmails,
	}
	if err := h.settingService.SetSupplyWithdrawalSettings(c.Request.Context(), settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, newSupplyWithdrawalSettingsResponse(
		h.settingService.GetSupplyWithdrawalSettings(c.Request.Context())))
}

// SupplyOnboardingSettingsResponse 是接入数量上限的对外形态。
type SupplyOnboardingSettingsResponse struct {
	// RelayEnabled 「URL + API Key」中转接入开关（M7）。
	RelayEnabled bool `json:"relay_enabled"`

	MaxAccountsPerUser int `json:"max_accounts_per_user"`
	MaxAccountsPerIP   int `json:"max_accounts_per_ip"`

	// 边界值随配置下发，理由同结算参数的 *_max。
	MaxAccountsPerUserCap int `json:"max_accounts_per_user_cap"`
	MaxAccountsPerIPCap   int `json:"max_accounts_per_ip_cap"`

	// 两个「这道闸现在有没有在起作用」的布尔。
	//
	// 0 在这两项上表示「不限」，而一个显示成 0 的输入框看起来更像「没配」。前端
	// 抄一遍 `> 0` 也能算出来，但那就把「0 是什么意思」这条规则复制到了第二处——
	// 后端哪天改用 -1 表示不限，面板会在没有任何报错的情况下说反话。
	UserCapEnabled bool `json:"user_cap_enabled"`
	IPCapEnabled   bool `json:"ip_cap_enabled"`
}

func newSupplyOnboardingSettingsResponse(s *service.SupplyOnboardingSettings) SupplyOnboardingSettingsResponse {
	resp := SupplyOnboardingSettingsResponse{
		MaxAccountsPerUserCap: service.SupplyOnboardingMaxAccountsPerUserMax,
		MaxAccountsPerIPCap:   service.SupplyOnboardingMaxAccountsPerIPMax,
	}
	if s == nil {
		return resp
	}
	resp.RelayEnabled = s.RelayEnabled
	resp.MaxAccountsPerUser = s.MaxAccountsPerUser
	resp.MaxAccountsPerIP = s.MaxAccountsPerIP
	resp.UserCapEnabled = s.MaxAccountsPerUser > 0
	resp.IPCapEnabled = s.MaxAccountsPerIP > 0
	return resp
}

// GetSupplyOnboardingSettings 读接入数量上限
// GET /api/v1/admin/settings/supply-onboarding
func (h *SettingHandler) GetSupplyOnboardingSettings(c *gin.Context) {
	settings := h.settingService.GetSupplyOnboardingSettings(c.Request.Context())
	response.Success(c, newSupplyOnboardingSettingsResponse(settings))
}

// UpdateSupplyOnboardingSettingsRequest 更新接入数量上限请求。
//
// 两个字段都用指针，与供给池的 DailyOverflowLimit 同一个理由，而且这里更要紧：
// 0 是「不限」这个有意义的取值，用值类型的话，任何漏传字段的旧前端都会把管理员
// 配好的上限静默清成不限——一道消失了的闸不会报错，只会在事后从刷号数据里被看见。
type UpdateSupplyOnboardingSettingsRequest struct {
	MaxAccountsPerUser *int `json:"max_accounts_per_user"`
	MaxAccountsPerIP   *int `json:"max_accounts_per_ip"`
	// RelayEnabled 同样用指针：漏传不该把一个开着的中转入口静默关掉。
	RelayEnabled *bool `json:"relay_enabled"`
}

// UpdateSupplyOnboardingSettings 写接入数量上限
// PUT /api/v1/admin/settings/supply-onboarding
//
// 越界值在 service 侧夹回区间（与观察期参数一致），所以回读不只是习惯——它是运营
// 看到自己填的 99999 变成 10000 的唯一途径。
//
// 漏传的字段沿用库里当前的值，不是清零。
func (h *SettingHandler) UpdateSupplyOnboardingSettings(c *gin.Context) {
	var req UpdateSupplyOnboardingSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	ctx := c.Request.Context()
	current := h.settingService.GetSupplyOnboardingSettings(ctx)
	// 从 current 整体拷贝而不是逐字段抄：早先这里只抄了两个字段，
	// 于是失效熔断那两项（max_incidents_per_user / incident_window_hours）
	// 在每一次保存接入上限时被**静默清零**——闸关了，面板上看不出任何异常。
	// 整体拷贝让「结构体加了字段、这里忘了抄」这类事故从根上不可能。
	settings := &service.SupplyOnboardingSettings{}
	if current != nil {
		*settings = *current
	}
	if req.RelayEnabled != nil {
		settings.RelayEnabled = *req.RelayEnabled
	}
	if req.MaxAccountsPerUser != nil {
		if *req.MaxAccountsPerUser < 0 {
			response.BadRequest(c, "max_accounts_per_user cannot be negative")
			return
		}
		settings.MaxAccountsPerUser = *req.MaxAccountsPerUser
	}
	if req.MaxAccountsPerIP != nil {
		if *req.MaxAccountsPerIP < 0 {
			response.BadRequest(c, "max_accounts_per_ip cannot be negative")
			return
		}
		settings.MaxAccountsPerIP = *req.MaxAccountsPerIP
	}
	if err := h.settingService.SetSupplyOnboardingSettings(ctx, settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	response.Success(c, newSupplyOnboardingSettingsResponse(
		h.settingService.GetSupplyOnboardingSettings(ctx)))
}

// AbuseDetectionSettingsResponse 是异常使用检测配置的对外形态。
type AbuseDetectionSettingsResponse struct {
	Enabled           bool    `json:"enabled"`
	WindowMinutes     int     `json:"window_minutes"`
	MinRequests       int64   `json:"min_requests"`
	MaxCacheHitRatio  float64 `json:"max_cache_hit_ratio"`
	MinAvgInputTokens float64 `json:"min_avg_input_tokens"`
	ThrottleRPM       int     `json:"throttle_rpm"`
	AutoThrottle      bool    `json:"auto_throttle"`
}

func newAbuseDetectionSettingsResponse(s *service.AbuseDetectionSettings) AbuseDetectionSettingsResponse {
	if s == nil {
		return AbuseDetectionSettingsResponse{}
	}
	return AbuseDetectionSettingsResponse{
		Enabled:           s.Enabled,
		WindowMinutes:     s.WindowMinutes,
		MinRequests:       s.MinRequests,
		MaxCacheHitRatio:  s.MaxCacheHitRatio,
		MinAvgInputTokens: s.MinAvgInputTokens,
		ThrottleRPM:       s.ThrottleRPM,
		AutoThrottle:      s.AutoThrottle,
	}
}

// GetAbuseDetectionSettings 读异常使用检测配置
// GET /api/v1/admin/settings/abuse-detection
func (h *SettingHandler) GetAbuseDetectionSettings(c *gin.Context) {
	response.Success(c, newAbuseDetectionSettingsResponse(
		h.settingService.GetAbuseDetectionSettings(c.Request.Context())))
}

// UpdateAbuseDetectionSettings 写异常使用检测配置
// PUT /api/v1/admin/settings/abuse-detection
//
// 越界的阈值由 service 层夹回合法区间而不是报错——一个填过头的数字应当被收敛成
// 最接近的合法值，而不是让整次保存失败。所以这里回读写入后的真实配置，
// 而不是回显请求体：回显会让面板显示一个数据库里并不存在的值。
func (h *SettingHandler) UpdateAbuseDetectionSettings(c *gin.Context) {
	var req AbuseDetectionSettingsResponse
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	ctx := c.Request.Context()
	settings := &service.AbuseDetectionSettings{
		Enabled:           req.Enabled,
		WindowMinutes:     req.WindowMinutes,
		MinRequests:       req.MinRequests,
		MaxCacheHitRatio:  req.MaxCacheHitRatio,
		MinAvgInputTokens: req.MinAvgInputTokens,
		ThrottleRPM:       req.ThrottleRPM,
		AutoThrottle:      req.AutoThrottle,
	}
	if err := h.settingService.SetAbuseDetectionSettings(ctx, settings); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, newAbuseDetectionSettingsResponse(
		h.settingService.GetAbuseDetectionSettings(ctx)))
}
