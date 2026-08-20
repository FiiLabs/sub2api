// APEXONE-EXT: 双边市场——提现服务。
//
// 供给侧（申请、撤回、看自己的单）与管理侧（看全部、打款、拒绝）装在同一个服务里，
// 但**入口是两组方法**：供给侧的每一个方法都强制带 userID 并把它作为查询条件，
// 管理侧的方法不接受 userID 过滤器之外的归属参数。分成两个服务反而更糟——
// 那样两边会各自持有一份「一张单子能怎么流转」的理解，而状态机只该有一份。
//
// 钱的动作全部在仓储的事务里（见 supplier_withdrawal_repo.go）。本层只做三件事：
// 读配置、校验入参、把「拒绝/撤回要退款，打款不退」这条规则翻译成 Refund 布尔。
package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	supplierWithdrawalDefaultPageSize = 20
	supplierWithdrawalMaxPageSize     = 100
)

// supplierWithdrawalSettingsReader 只读提现那一组配置。
//
// 窄接口：这个服务不该有能力读到分成比例或观察期参数——它们与提现无关，
// 而一个能读到全部设置的依赖迟早会被用来在这里做一次不属于它的判断。
type supplierWithdrawalSettingsReader interface {
	GetSupplyWithdrawalSettings(ctx context.Context) *SupplyWithdrawalSettings
}

// supplierWithdrawalWalletReader 只读钱包余额。
type supplierWithdrawalWalletReader interface {
	EnsureWallet(ctx context.Context, userID int64) (*SupplierCreditSummary, error)
}

// supplierWithdrawalNotifierPort 是通知出口。做成接口而不是直接持有
// *SupplierWithdrawalNotifier，是为了让测试能在不碰 SMTP 的前提下断言
// 「哪一步该发信、哪一步不该发」——尤其是撤回**不该**发信这一条，只有
// 能观察到"没调用"才钉得住。
type supplierWithdrawalNotifierPort interface {
	NotifyRequested(w *SupplierWithdrawal)
	NotifyResolved(w *SupplierWithdrawal)
}

// SupplierWithdrawalService 是提现的应用服务。
type SupplierWithdrawalService struct {
	repo     SupplierWithdrawalRepository
	wallet   supplierWithdrawalWalletReader
	settings supplierWithdrawalSettingsReader
	notifier supplierWithdrawalNotifierPort
}

// NewSupplierWithdrawalService 构造提现服务。
//
// notifier 允许为 nil：通知不可用绝不能让提现不可用。调用点因此一律走
// s.notify*，那几个方法自己做 nil 判断。
func NewSupplierWithdrawalService(
	repo SupplierWithdrawalRepository,
	creditRepo SupplierCreditRepository,
	settingService *SettingService,
	notifier *SupplierWithdrawalNotifier,
) *SupplierWithdrawalService {
	s := &SupplierWithdrawalService{repo: repo, wallet: creditRepo, settings: settingService}
	// 显式判 nil 再赋值：一个装着 nil 指针的非 nil 接口会让下面的 nil 判断失效，
	// 于是"通知没配"变成一次空指针 panic，而 panic 的位置在提现主路径上。
	if notifier != nil {
		s.notifier = notifier
	}
	return s
}

func (s *SupplierWithdrawalService) notifyRequested(w *SupplierWithdrawal) {
	if s == nil || s.notifier == nil || w == nil {
		return
	}
	s.notifier.NotifyRequested(w)
}

func (s *SupplierWithdrawalService) notifyResolved(w *SupplierWithdrawal) {
	if s == nil || s.notifier == nil || w == nil {
		return
	}
	s.notifier.NotifyResolved(w)
}

func (s *SupplierWithdrawalService) ready() bool {
	return s != nil && s.repo != nil && s.settings != nil
}

func (s *SupplierWithdrawalService) unavailable() error {
	return infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "supplier withdrawal service unavailable")
}

// SupplierWithdrawalOptions 是申请表单需要的一切：能不能提、最少提多少、
// 打到哪些渠道、此刻可用多少、还挂着几张单。
//
// 做成一次请求返回全部，而不是让前端拼三个接口的结果：这几个数必须来自同一个
// 时刻，否则会出现「余额显示 100、渠道列表是空的、按钮却亮着」这种自相矛盾的界面。
type SupplierWithdrawalOptions struct {
	// Available 提现此刻真的可用（开关开着且配了渠道）。前端只看这一个布尔。
	Available bool `json:"available"`
	// Enabled 总开关。与 Available 分开报，是为了让「开了但没配渠道」在管理端
	// 之外也能被看见——供给者看到"暂未开放"和运营看到"你还没配渠道"是同一件事。
	Enabled    bool     `json:"enabled"`
	MinAmount  float64  `json:"min_amount"`
	MaxPending int      `json:"max_pending"`
	Channels   []string `json:"channels"`
	Notice     string   `json:"notice"`
	// AvailableCredit 此刻可提的余额（钱包的可用区）。
	AvailableCredit float64 `json:"available_credit"`
	// PendingCount 已挂着的未决单数。
	PendingCount int64 `json:"pending_count"`
}

// GetOptions 读申请表单需要的全部信息。
//
// 读不到钱包/未决单数时不报错，回零：这个接口的用途是画一个表单，
// 让它因为一个统计数字失败而整页报错，供给者连"提现开着没"都看不到。
// 真正会拦住一笔错误申请的是 Request 里的那几道校验，它们都走数据库。
func (s *SupplierWithdrawalService) GetOptions(ctx context.Context, userID int64) (*SupplierWithdrawalOptions, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	settings := s.settings.GetSupplyWithdrawalSettings(ctx)
	out := &SupplierWithdrawalOptions{
		Available:  settings.Available(),
		Enabled:    settings.Enabled,
		MinAmount:  settings.MinAmount,
		MaxPending: settings.MaxPending,
		Channels:   append([]string(nil), settings.Channels...),
		Notice:     settings.Notice,
	}
	if s.wallet != nil {
		if wallet, err := s.wallet.EnsureWallet(ctx, userID); err == nil && wallet != nil {
			out.AvailableCredit = wallet.AvailableCredit
		}
	}
	if pending, err := s.repo.CountPending(ctx, userID); err == nil {
		out.PendingCount = pending
	}
	return out, nil
}

// SupplierWithdrawalRequest 是供给者提交的申请。
type SupplierWithdrawalRequest struct {
	Amount        float64
	PayoutChannel string
	PayoutAccount string
	UserNote      string
}

// Request 提交一笔提现申请。
//
// 校验顺序是刻意的：先答"这个功能开着吗"，再答"你填的对吗"，最后才碰钱。
// 反过来的话，功能关着的时候供给者会先收到一串字段校验错误，改完了才发现根本没开。
func (s *SupplierWithdrawalService) Request(ctx context.Context, userID int64, req SupplierWithdrawalRequest) (*SupplierWithdrawal, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}

	settings := s.settings.GetSupplyWithdrawalSettings(ctx)
	if !settings.Enabled {
		return nil, ErrSupplierWithdrawalDisabled
	}
	if len(settings.Channels) == 0 {
		return nil, ErrSupplierWithdrawalNotConfigured
	}
	if !settings.HasChannel(req.PayoutChannel) {
		return nil, ErrSupplierWithdrawalChannelInvalid
	}

	account := strings.TrimSpace(req.PayoutAccount)
	if account == "" {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_ACCOUNT_REQUIRED", "payout account is required")
	}
	if len([]rune(account)) > SupplierPayoutAccountMaxLen {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_ACCOUNT_TOO_LONG", "payout account is too long")
	}
	note := strings.TrimSpace(req.UserNote)
	if len([]rune(note)) > SupplierWithdrawalNoteMaxLen {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_NOTE_TOO_LONG", "note is too long")
	}

	if req.Amount <= 0 {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_INVALID_AMOUNT", "amount must be positive")
	}
	// 低于起提额是拒绝，不是夹到起提额：替供给者把 5 块的申请改成 50 块，
	// 等于替他决定提多少钱。
	if settings.MinAmount > 0 && req.Amount < settings.MinAmount {
		return nil, ErrSupplierWithdrawalBelowMinimum
	}

	created, err := s.repo.Create(ctx, SupplierWithdrawalCreateParams{
		UserID:        userID,
		Amount:        req.Amount,
		PayoutChannel: strings.TrimSpace(req.PayoutChannel),
		PayoutAccount: account,
		UserNote:      note,
		MaxPending:    settings.MaxPending,
	})
	if err != nil {
		return nil, err
	}
	// 通知在落库**之后**：钱已经扣了、单子已经在了，这封信才是真的。
	// 反过来（先发信后落库）会在 Create 撞上未决单上限时发出一封关于不存在的
	// 单子的邮件。
	s.notifyRequested(created)
	return created, nil
}

// Cancel 供给者撤回自己的未决单，钱退回可用区。
//
// **刻意不检查总开关**：开关是"还收不收新单"，不是"已经扣下来的钱还能不能拿回去"。
// 运营关掉提现的那一刻，所有挂着的单子如果连撤回都不让，那笔钱就被锁死在一张
// 谁也不会处理的单子上了。
func (s *SupplierWithdrawalService) Cancel(ctx context.Context, userID, id int64) (*SupplierWithdrawal, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	return s.repo.Resolve(ctx, SupplierWithdrawalResolveParams{
		ID:     id,
		UserID: userID,
		Status: SupplierWithdrawalStatusCanceled,
		Refund: true,
	})
}

// List 读供给者自己的提现记录。userID 由调用方从会话里取。
func (s *SupplierWithdrawalService) List(ctx context.Context, userID int64, page, pageSize int) ([]SupplierWithdrawal, int64, error) {
	if !s.ready() {
		return nil, 0, s.unavailable()
	}
	if userID <= 0 {
		return nil, 0, infraerrors.BadRequest("INVALID_USER", "invalid user")
	}
	page, pageSize = clampSupplierWithdrawalPage(page, pageSize)
	return s.repo.List(ctx, SupplierWithdrawalFilter{UserID: userID, Page: page, PageSize: pageSize})
}

// ============================================================================
// 管理侧
// ============================================================================

// AdminList 读全站提现单。status 空 = 不限。
func (s *SupplierWithdrawalService) AdminList(ctx context.Context, filter SupplierWithdrawalFilter) ([]SupplierWithdrawal, int64, error) {
	if !s.ready() {
		return nil, 0, s.unavailable()
	}
	if filter.UserID < 0 {
		filter.UserID = 0
	}
	// 不认识的状态直接当"不筛"会给出一个看起来正常、实际是全量的列表——
	// 运营以为自己在看待办队列，其实在看全部。
	if filter.Status != "" && !isKnownWithdrawalStatus(filter.Status) {
		return nil, 0, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_INVALID_STATUS", "unsupported withdrawal status")
	}
	filter.Page, filter.PageSize = clampSupplierWithdrawalPage(filter.Page, filter.PageSize)
	return s.repo.List(ctx, filter)
}

// MarkPaid 标记已打款。**不退款**——钱已经出去了。
//
// externalRef 是打款凭证（交易号/工单号），平台不解析它，只存下来：出纠纷时
// 「平台说打了、供给者说没收到」这个僵局，唯一的破解方式就是有一个双方都能拿去
// 对账的字符串。它不是必填的，因为不是每种渠道都有单号；但界面上要提示填。
func (s *SupplierWithdrawalService) MarkPaid(ctx context.Context, id int64, reviewerID *int64, externalRef, note string) (*SupplierWithdrawal, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	if len([]rune(strings.TrimSpace(externalRef))) > SupplierWithdrawalExternalRefMaxLen {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_REF_TOO_LONG", "external reference is too long")
	}
	if len([]rune(strings.TrimSpace(note))) > SupplierWithdrawalNoteMaxLen {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_NOTE_TOO_LONG", "note is too long")
	}
	resolved, err := s.repo.Resolve(ctx, SupplierWithdrawalResolveParams{
		ID:          id,
		Status:      SupplierWithdrawalStatusPaid,
		ReviewerID:  reviewerID,
		Refund:      false,
		ReviewNote:  note,
		ExternalRef: externalRef,
	})
	if err != nil {
		return nil, err
	}
	s.notifyResolved(resolved)
	return resolved, nil
}

// Reject 拒绝一张单子，钱退回可用区。
//
// note 是必填的：一笔被拒的提现是供给者最需要一个解释的时刻，而"无理由拒绝"
// 在一个双边市场里等于随时可以扣住别人的钱。
func (s *SupplierWithdrawalService) Reject(ctx context.Context, id int64, reviewerID *int64, note string) (*SupplierWithdrawal, error) {
	if !s.ready() {
		return nil, s.unavailable()
	}
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_REASON_REQUIRED", "a reason is required to reject a withdrawal")
	}
	if len([]rune(trimmed)) > SupplierWithdrawalNoteMaxLen {
		return nil, infraerrors.BadRequest("SUPPLIER_WITHDRAWAL_NOTE_TOO_LONG", "note is too long")
	}
	resolved, err := s.repo.Resolve(ctx, SupplierWithdrawalResolveParams{
		ID:         id,
		Status:     SupplierWithdrawalStatusRejected,
		ReviewerID: reviewerID,
		Refund:     true,
		ReviewNote: trimmed,
	})
	if err != nil {
		return nil, err
	}
	s.notifyResolved(resolved)
	return resolved, nil
}

// isKnownWithdrawalStatus 状态白名单。
func isKnownWithdrawalStatus(status string) bool {
	switch status {
	case SupplierWithdrawalStatusPending,
		SupplierWithdrawalStatusPaid,
		SupplierWithdrawalStatusRejected,
		SupplierWithdrawalStatusCanceled:
		return true
	}
	return false
}

func clampSupplierWithdrawalPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = supplierWithdrawalDefaultPageSize
	}
	if pageSize > supplierWithdrawalMaxPageSize {
		pageSize = supplierWithdrawalMaxPageSize
	}
	return page, pageSize
}
