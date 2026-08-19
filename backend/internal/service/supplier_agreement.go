// APEXONE-EXT: 双边市场——供给者协议的同意与门禁。
//
// 平台在建出那个握着别人上游凭证的 accounts 行之前，先要求他明确同意一份带版本号的
// 协议，并把这次同意存成证据（supplier_agreement_acceptances，见 migrations/228）。
//
// # 为什么门禁挂在 StartOAuth 和 CompleteOAuth 两处
//
// CompleteOAuth 是**必须**的那一道：它是账号真正建出来、凭证真正落库的那一刻，
// 门禁漏在这里就等于没有门禁。StartOAuth 那一道纯粹是体验——不拦的话，供给者会
// 跑完一整遍上游授权（登录、选账号、复制授权码）之后才在最后一步被告知"你还没
// 同意协议"，而那时他已经在 Anthropic 那边生成了一个 setup token。
//
// 两处都调同一个 requireAgreement，不各写一份判断：这是一道法律门禁，两份实现
// 迟早会漂移，而漂移的那一边一定是漏放行的那一边。
//
// # 版本变了，已经挂着的号不受影响
//
// 门禁只在「接入新号」这条路径上。协议改版不会把存量供给号停掉，也不会追溯地
// 要求谁重新同意——那意味着运营改一个错别字就能把全网供给停掉。代价是存量号在
// 一段时间里跑在旧版协议下，这是真实存在的、也是可接受的：他当时同意的那一版
// 被完整地记在了同意记录里，按哪一版算是查得清的。
package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	// ErrSupplierAgreementNotConfigured 平台还没发布供给者协议。
	//
	// 这是一条**拒绝**而不是"跳过同意"：见 setting_supply_agreement.go 头部。
	// 错误码与 REQUIRED 分开，因为两者要做的事完全不同——这一个只有管理员能修，
	// 供给者点什么都没用，前端得把话说成「平台尚未发布协议」而不是「请先同意」。
	ErrSupplierAgreementNotConfigured = infraerrors.BadRequest(
		"SUPPLIER_AGREEMENT_NOT_CONFIGURED", "the supplier agreement has not been published yet")
	// ErrSupplierAgreementRequired 供给者还没同意当前版本的协议。
	ErrSupplierAgreementRequired = infraerrors.BadRequest(
		"SUPPLIER_AGREEMENT_REQUIRED", "you must accept the current supplier agreement first")
	// ErrSupplierAgreementVersionMismatch 提交的版本不是当前生效的版本。
	//
	// 典型场景不是攻击，是一个开了很久的页面：运营在这期间发布了新版本，而他点的
	// 「同意」按的还是旧版正文。照旧版记一条同意记录，会让证据指向一份他其实没读过
	// 的文本——那比让他刷新一次页面糟得多。
	ErrSupplierAgreementVersionMismatch = infraerrors.BadRequest(
		"SUPPLIER_AGREEMENT_VERSION_MISMATCH", "the agreement has been updated, please review it again")
)

// supplierAgreementUserAgentMaxLen UA 存进库前截断到列宽。
//
// 超长就截断而不是拒绝：UA 是取证用的旁证，不是同意本身。因为一个畸形的 UA 头
// 让同意失败，等于把一个纯粹的记录问题变成了功能故障。
const supplierAgreementUserAgentMaxLen = 512

// supplierAgreementIPMaxLen 同上。
const supplierAgreementIPMaxLen = 64

// SupplierAgreementAcceptance 是一条同意记录。
//
// 只增不改：一行代表一次真实发生过的点击。
type SupplierAgreementAcceptance struct {
	UserID     int64
	Version    string
	AcceptedAt time.Time
	IP         string
	UserAgent  string
}

// SupplierAgreementView 是供给者接入页看到的协议状态。
//
// 把「当前协议」和「我同意到哪一版了」放在同一个响应里，是因为页面要回答的是一个
// 合成的问题：这个按钮现在能不能点、不能点是因为什么。分成两个接口的话，前端得
// 自己把两个状态拼成一句话，而那句话有四种情况（未发布 / 没同意过 / 同意的是旧版 /
// 已同意）。
type SupplierAgreementView struct {
	// Version 当前生效的协议版本。空 = 平台尚未发布。
	Version string `json:"version"`
	// Published 是否已发布。与 Version 非空等价，单独给一个布尔是为了让前端的
	// 判断不必依赖"空字符串"这种约定。
	Published bool `json:"published"`
	// URL 协议全文链接，可空。
	URL string `json:"url,omitempty"`
	// Body 协议正文，可空。**纯文本**，前端不得当 HTML/markdown 渲染。
	Body string `json:"body,omitempty"`

	// Accepted 当前用户是否已经同意了 Version 这一版。这一个布尔就是门禁的判据。
	Accepted bool `json:"accepted"`
	// AcceptedAt 同意当前版本的时刻，仅 Accepted 时有值。
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	// AcceptedVersion 这个人最近一次同意的版本号。
	//
	// 与 Version 不同 = 协议在他同意之后改过。前端据此把话说成「协议已更新，
	// 需要重新确认」而不是「请先同意」——后者会让一个明明点过同意的人以为系统坏了。
	AcceptedVersion string `json:"accepted_version,omitempty"`
}

// GetAgreement 返回当前协议与该用户的同意状态。
func (s *SupplierOnboardingService) GetAgreement(ctx context.Context, userID int64) (*SupplierAgreementView, error) {
	if s == nil || s.repo == nil || s.settings == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	if userID <= 0 {
		return nil, ErrSupplierOnboardingDisabled
	}

	settings := s.settings.GetSupplyAgreementSettings(ctx)
	view := &SupplierAgreementView{}
	if settings != nil {
		view.Version = strings.TrimSpace(settings.Version)
		view.URL = settings.URL
		view.Body = settings.Body
	}
	view.Published = view.Version != ""

	// 没发布就没有"同意状态"可言——拿一个空版本去查同意记录只会查到空。
	if !view.Published {
		return view, nil
	}

	accepted, err := s.repo.FindAgreementAcceptance(ctx, userID, view.Version)
	if err != nil {
		return nil, err
	}
	if accepted != nil {
		view.Accepted = true
		view.AcceptedVersion = accepted.Version
		at := accepted.AcceptedAt
		view.AcceptedAt = &at
		return view, nil
	}

	// 只在"没同意当前版本"这条分支上多查一次：要区分「从没同意过」和「同意的是旧版」，
	// 而这两句话在界面上是不同的话。已经同意的人不必付这次查询。
	latest, err := s.repo.LatestAgreementAcceptance(ctx, userID)
	if err != nil {
		return nil, err
	}
	if latest != nil {
		view.AcceptedVersion = latest.Version
	}
	return view, nil
}

// AcceptAgreement 记下一次同意。
//
// version 由调用方回传并**必须**与当前生效版本一致，不是服务端自己取当前版本了事：
// 前者能证明"他看到的就是这一版"，后者只能证明"他点了一下按钮"。这两件事在争议里
// 差别很大——页面开了两天没刷新的人，点的是旧版正文。
func (s *SupplierOnboardingService) AcceptAgreement(ctx context.Context, userID int64, version, ip, userAgent string) (*SupplierAgreementView, error) {
	if s == nil || s.repo == nil || s.settings == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	if userID <= 0 {
		return nil, ErrSupplierOnboardingDisabled
	}

	settings := s.settings.GetSupplyAgreementSettings(ctx)
	if !settings.Published() {
		return nil, ErrSupplierAgreementNotConfigured
	}
	current := strings.TrimSpace(settings.Version)
	if strings.TrimSpace(version) != current {
		return nil, ErrSupplierAgreementVersionMismatch
	}

	acceptance := &SupplierAgreementAcceptance{
		UserID:     userID,
		Version:    current,
		AcceptedAt: time.Now(),
		IP:         truncateRunes(strings.TrimSpace(ip), supplierAgreementIPMaxLen),
		UserAgent:  truncateRunes(strings.TrimSpace(userAgent), supplierAgreementUserAgentMaxLen),
	}
	if err := s.repo.RecordAgreementAcceptance(ctx, acceptance); err != nil {
		return nil, err
	}

	// 回读而不是把刚写的那份拼回去：重复点同意时库里保留的是**最早**那一行
	// （ON CONFLICT DO NOTHING），拼回去会让页面显示一个比事实晚的同意时刻。
	return s.GetAgreement(ctx, userID)
}

// requireAgreement 是接入路径上的门禁。
//
// 两种拒绝分开返回，理由见 ErrSupplierAgreementNotConfigured 的注释：一个只有
// 管理员能解，一个只有供给者能解，合并成一个错误码会让两边都收到一句无从下手的话。
func (s *SupplierOnboardingService) requireAgreement(ctx context.Context, userID int64) error {
	if s == nil || s.repo == nil || s.settings == nil {
		return ErrSupplierOnboardingDisabled
	}

	settings := s.settings.GetSupplyAgreementSettings(ctx)
	if !settings.Published() {
		return ErrSupplierAgreementNotConfigured
	}

	accepted, err := s.repo.FindAgreementAcceptance(ctx, userID, strings.TrimSpace(settings.Version))
	if err != nil {
		// 查不到不等于没同意——查询失败时放行会让一次数据库抖动变成"没有协议也能挂号"。
		// 这条路径上 fail-closed 的代价只是重试一次接入。
		return err
	}
	if accepted == nil {
		return ErrSupplierAgreementRequired
	}
	return nil
}

// truncateRunes 按字符（而不是字节）截断，避免把一个多字节字符切成半个。
func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return ""
	}
	if len(s) <= max {
		return s
	}
	runes := []rune(s)
	// 列宽是按字节算的，所以先按字节裁一刀再按字符收边，两个口径都不越界。
	for len(string(runes)) > max {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
