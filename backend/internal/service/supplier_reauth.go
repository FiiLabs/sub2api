// APEXONE-EXT: 双边市场——供给者就地重新授权一个已有的号。
//
// # 这条路径为什么必须存在
//
// 供给者的上游凭证会失效（订阅到期、他在 claude.ai 撤销了授权、token 过了刷不回来）。
// 在这条路径之前，平台对此的全部回应是：观察期任务每 15 分钟探测一次、每次拿到 401、
// 永远失败下去，而供给者的仪表盘上只有一行红字。他唯一能做的自助动作是**解绑**
// （不可逆、抹凭证、软删号）**再重新接入**——换一个新的 account id，观察期从头跑，
// 他自己设的每日上限丢失，而且如果他已经挂到人均上限，还必须先解绑才能重挂，
// 一个先后顺序搞反就把号弄丢了。
//
// 更要紧的是 supplier_lifecycle_service.go 里 shouldProbe 那句注释：
// 「供给者重新授权后 Status 会回到 active，探测自然恢复」——它描述的路径此前
// 并不存在。整个观察期状态机建立在一个假设之上，而那个假设没有实现。
//
// # 它与「重新接入」的唯一区别，也是它全部的安全性所在
//
// 换进来的必须是**同一份上游订阅**（requireSameSubscription）。没有这一道，
// 一个已经跑完观察期、攒了信誉和每日上限的 account id，就成了一个可以被任意
// 一份新订阅顶包的壳子——而它跳过了整个观察期。这一条与「已 promote 的号重新
// 授权后保持 active 而不掉回观察期」是绑死的：削弱前者就必须同时改掉后者。
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
)

// CompleteReauthInput 兑换一次重新授权。
type CompleteReauthInput struct {
	UserID    int64
	AccountID int64
	SessionID string
	Code      string
	// ClientIP 只进日志，**不判任何闸**。理由见 CompleteReauth 里那段
	// 「不适用的闸」——这条路径不建号，按来源 IP 数号的那道闸与它无关。
	ClientIP string
}

// StartReauth 为 userID 名下的 accountID 发起一次就地重新授权。
//
// 与 StartOAuth 的三处刻意不同：
//
//  1. **先过 getOwnedAccount**。这条路径的一切都是关于「那一个号」的，
//     归属没确认之前不该往会话表里写任何东西。
//  2. **不调 supplyGroupID / IsEnabled**。运营关闭新接入时，已经在池子里的号
//     仍然必须修得好——把「修复」挂在「是否还收新供给」上是错误耦合：那会让一次
//     运营侧的开关变动，静默地把一批在线供给号变成不可修复。
//  3. **不调 requireCapacity**。见 CompleteReauth。
//
// 协议门禁保留：那是「这个人还能不能供货」，与他是在挂新号还是在修旧号无关。
func (s *SupplierOnboardingService) StartReauth(ctx context.Context, userID, accountID int64) (*SupplierAuthorization, error) {
	if s == nil || s.repo == nil || s.oauth == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	account, err := s.getOwnedAccount(ctx, userID, accountID)
	if err != nil {
		return nil, err
	}
	if err := reauthEligible(account); err != nil {
		return nil, err
	}
	if err := s.requireAgreement(ctx, userID); err != nil {
		return nil, err
	}

	pending, err := s.repo.CountPendingSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	if pending >= supplierMaxPendingSessions {
		return nil, ErrSupplierOAuthTooManyPending
	}

	// scope 与接入路径**一字不差**。一次重新授权不是重新协商权限的时机——
	// 供给者当初同意的是「平台替我转发推理请求」，换一份 token 不改变这一点。
	auth, err := s.oauth.NewSupplierAuthorization(oauth.ScopeInference)
	if err != nil {
		return nil, err
	}

	session := &SupplierOAuthSession{
		SessionID:    auth.SessionID,
		UserID:       userID,
		Platform:     PlatformAnthropic,
		State:        auth.State,
		CodeVerifier: auth.CodeVerifier,
		Scope:        auth.Scope,
		ExpiresAt:    time.Now().Add(supplierOAuthSessionTTL),
		// 这一条会话从此只能用来修这一个号。
		AccountID: account.ID,
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		return nil, err
	}

	s.cleanupExpiredSessions()
	return auth, nil
}

// CompleteReauth 兑换授权码并把新凭证原地换进那个号。
//
// # 顺序
//
//	归属 → 资格 → 协议 → 领会话 → 换 token → 身份闸 → 写凭证 → 清错误态 → 恢复调度
//
// 每一步的位置都是一次判断：
//
//   - **协议门禁在领会话之前**。领取是一次性消费，在它之后被拒的人会白白丢掉手上
//     那个授权码，而"你还没同意协议"在第一行就判得出来。同 CompleteOAuth。
//   - **身份闸在任何写之前**。它是这条路径唯一的安全边界，写完再检查等于没检查。
//   - **写凭证严格早于清错误态**。反过来会出现「status=active + 一份死 token」
//     被派真实流量的窗口。而按这个顺序，中间失败留下的是「新凭证已就位、号仍是
//     错误态」——不接单、不被探测（shouldProbe 跳过非 active 的号）、事件不关，
//     供给者重试一次就好。与 DetachAccount 里 scrub 早于 delete 是同一条不对称性。
//   - **恢复调度放最后且有条件**。它是唯一一步会把号重新放到付费流量前面的操作。
//
// # 为什么最后那一步不能省
//
// SetError 会把 schedulable 一并置 false，而 ClearError **不会**把它还回来
// （repository/account_repo.go）。观察期任务只扫 pending_review 的号，所以一个
// 已经 promote 进池的供给号一旦被置错（真实流量上的 401 走 RateLimitService，
// 或本次改动之后的探测 401），在这条路径出现之前是**永久**不可调度的——
// 代码里没有任何一条路能把它放回去。这一步就是那条路。
//
// pending_review 的号不做这一步：它本来就该是 schedulable=false，
// 把它放回池子是观察期任务的职责，不是这里的。
//
// # 不适用的闸
//
//   - requireCapacity（人均 / 单 IP 号数上限）：这条路径**不建号**。数「你还能挂几个」
//     是在回答一个没人问的问题，而且会把一个已经挂满的供给者锁死在他的坏号上——
//     他修不了，也腾不出位置。
//   - GuardOnboarding（近期失效熔断）：熔断器数的是这个人最近坏掉的号，而重新授权
//     正是**关闭**那些事件的动作。套上去会形成闭环：号坏 → 事件积累 → 熔断 →
//     修不了 → 事件不关 → 永久熔断，唯一出口是解绑重挂，而那条路是 onboarding，
//     同样被熔断拦着。熔断器的本意（见 GuardOnboarding）是拦住"反复往平台塞坏号
//     的人"，而重新授权一个号也没多塞。
//
// # 一个已知的、可接受的抖动
//
// 如果供给者授权成功但那份订阅本身已经死了：ClearError → 下一次 Sweep 关掉事件 →
// 下一次探测 401 → SetError → 开一条**新**事件 → 再发一封信。它由人工点击次数
// 天然限流（每次都是一次深思熟虑的操作），且 notified_at 保证每条事件只发一封。
// 写在这里是为了它日后被人当成 bug 时，能读到这是权衡过的。
func (s *SupplierOnboardingService) CompleteReauth(ctx context.Context, input *CompleteReauthInput) (*SupplierAccountView, error) {
	if s == nil || s.repo == nil || s.oauth == nil || s.accountRepo == nil {
		return nil, ErrSupplierOnboardingDisabled
	}
	if input == nil {
		return nil, ErrSupplierAccountNotFound
	}
	if strings.TrimSpace(input.Code) == "" || strings.TrimSpace(input.SessionID) == "" {
		return nil, ErrSupplierOAuthSessionInvalid
	}

	account, err := s.getOwnedAccount(ctx, input.UserID, input.AccountID)
	if err != nil {
		return nil, err
	}
	if err := reauthEligible(account); err != nil {
		return nil, err
	}
	if err := s.requireAgreement(ctx, input.UserID); err != nil {
		return nil, err
	}

	// 原子领取，并且**绑在这个号上**：一条接入会话（account_id 为 NULL）在这里
	// 领不到，一条为别的号发起的会话也领不到。见 ClaimSession。
	session, err := s.repo.ClaimSession(ctx, strings.TrimSpace(input.SessionID), input.UserID, account.ID)
	if err != nil {
		return nil, err
	}

	// token 交换本身就是「这份凭证是活的」的证明——与中转路径当场探测同一个标准
	// （见 supplier_relay.go 里那句「OAuth 路径有 token 交换兜真伪」）。
	// 不再额外打一次探测：那只会多烧供给者一次额度去证明同一件事。
	tokenInfo, err := s.oauth.ExchangeSupplierCode(ctx, strings.TrimSpace(input.Code), &SupplierAuthorization{
		State:        session.State,
		CodeVerifier: session.CodeVerifier,
		Scope:        session.Scope,
	})
	if err != nil {
		return nil, fmt.Errorf("exchange authorization code: %w", err)
	}

	if err := s.requireSameSubscription(ctx, account, tokenInfo); err != nil {
		return nil, err
	}

	state := supplyStateOf(account)
	extra := map[string]any{
		// 这三个键都属于**上一份凭证**：它失败过几次、上次什么时候探的、
		// 因为什么失败。换了凭证之后它们一个字都不再成立。
		SupplyProbePassesExtraKey: 0,
		SupplyProbeErrorExtraKey:  "",
		// 清空而不是写 now：shouldProbe 在读不到这个键时直接返回 true，
		// 于是下一轮生命周期任务立刻探一次，而不是再等一个 ProbeInterval（≥5 分钟）。
		// 供给者刚点完「重新授权」，这几分钟里他正盯着页面看。
		SupplyProbeAtExtraKey: "",
	}
	if state == SupplyStatePendingReview {
		// 观察窗从这一刻重新计时：之前那段观察的是一份已经失效的凭证，
		// 拿它的时长给新凭证记账，等于让新凭证白得一段没被观察过的时间。
		// 同 ResumeAccount 从 retired 挂回时的处理。
		//
		// 已经 active 的号**不**重置——它不在观察期里，写这个键只会让界面上
		// 冒出一段莫名其妙的进度。
		extra[SupplyProbationSinceExtraKey] = time.Now().Format(time.RFC3339)
	}

	if err := s.repo.ApplyReauthCredentials(
		ctx, account.ID, input.UserID, buildSupplierClaudeCredentials(tokenInfo), extra,
	); err != nil {
		return nil, err
	}

	if err := s.accountRepo.ClearError(ctx, account.ID); err != nil {
		// 凭证已经换好了。这一步失败留下的是「新凭证 + 错误态」——安全的那一半：
		// 号不接单、不被探测，供给者重试一次即可。所以照常报错，不吞。
		slog.Error("[SupplierReauth] credentials applied but error state not cleared",
			"account_id", account.ID, "user_id", input.UserID, "error", err)
		return nil, err
	}

	if state == SupplyStateActive {
		if err := s.accountRepo.SetSchedulable(ctx, account.ID, true); err != nil {
			slog.Error("[SupplierReauth] account repaired but not returned to the pool",
				"account_id", account.ID, "user_id", input.UserID, "error", err)
			return nil, err
		}
	}

	slog.Info("[SupplierReauth] supply account re-authorized",
		"account_id", account.ID, "user_id", input.UserID, "supply_state", state)

	// 重读而不是就地改内存里那份：上面三次写分别落在三个不同的地方
	// （raw SQL、ClearError、SetSchedulable），手工拼一份视图迟早与库里不一致。
	updated, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || updated == nil {
		return nil, ErrSupplierAccountNotFound
	}
	return newSupplierAccountView(updated, s.probationSettings(ctx)), nil
}

// reauthEligible 判断一个号能不能走 OAuth 重新授权。
//
// 两条拒绝各有理由：
//
//   - **中转（API key）账号**没有 OAuth 身份，走不了这条路。它的补救是重新提交
//     那把 key，是另一件事、另一个（目前还不存在的）端点。让它进来只会在
//     ExchangeSupplierCode 之后才失败——那时供给者已经白跑了一趟上游授权。
//   - **已下线（retired）的号**：那是他自己按的按钮，对应的动作叫「重新挂回」
//     （ResumeAccount）。在同一行上再放一个长得也像「修好它」的按钮，比只有一个
//     按钮更糟——两个都试一遍才知道该按哪个。
//
// draining（排空中）**放行**：凭证坏了就是坏了，修它与「他正在下线这个号」是两件
// 独立的事。重新授权不取消排空——那是 ResumeAccount 的职责，这里不替他改主意。
func reauthEligible(account *Account) error {
	if account == nil {
		return ErrSupplierAccountNotFound
	}
	if !account.IsOAuth() {
		return ErrSupplierReauthUnsupported
	}
	if supplyStateOf(account) == SupplyStateRetired {
		return ErrSupplierAccountRetired
	}
	return nil
}

// requireSameSubscription 挡住「把另一份订阅塞进一个已有账号的壳子里」。
//
// 两道，缺一不可：
//
//  1. **身份必须与这个号原本的身份一致。** 这是重新授权与重新接入的全部区别所在。
//     没有这一道，一个已经 promote 进池、跑完观察期、攒了每日上限和收益的
//     account id，就成了一个可以被任意一份新订阅顶包的壳——而它跳过了整个观察期。
//  2. **命中的那一行必须就是这个号自己。** 第 1 道理论上已经蕴含了它，但两者的
//     数据源不同（一个读本行 credentials，一个查全表），而这道闸的作用恰恰是在
//     两者不一致时（历史脏数据、并发改动）拒绝而不是放行。
//
// # 为什么只比最强的那一个键，而不是「所有共有的键都必须相等」
//
// 上游是允许改邮箱的。account_uuid 没变就是同一份订阅，此时邮箱不同是一个
// 合法的事实。要求每个共有键都相等，会把一次正常的改邮箱判成盗用——而供给者
// 对此束手无策（他改不回去，也不知道平台在比什么）。
//
// # 为什么"一个可比的键都没有"是拒绝而不是放行
//
// 同 ErrSupplierAccountIdentityUnavailable 那条：没有身份键就判不了「是不是同一份
// 订阅」，而放行一个判不了的重绑，正好是第 1 道要挡的那件事。
//
// **这个函数与 CompleteReauth 里「已 active 的号保持 active」那个分支是绑死的。**
// 之所以敢不让它掉回观察期，唯一的依据就是这里保证了换进来的是同一份订阅。
// 一旦削弱这道闸，必须同时把那个分支改成 pending_review + schedulable=false +
// 重置观察期，否则 account id 就成了未审订阅的洗白通道。
func (s *SupplierOnboardingService) requireSameSubscription(ctx context.Context, account *Account, tokenInfo *TokenInfo) error {
	incoming := supplierIdentityValues(tokenInfo)

	for _, key := range SupplierIdentityKeys {
		newValue, ok := incoming[key]
		if !ok {
			continue
		}
		storedValue := strings.TrimSpace(account.GetCredential(string(key)))
		if storedValue == "" {
			continue
		}

		// 两边都有值的最强键。只比这一个，然后收工——见上面「为什么只比最强的」。
		//
		// 大小写不敏感地比：对齐查重语句里那个 LOWER(...)（见
		// supplierAccountFindByEmailSQL）。按字节比会让上游把 Foo@x.com 显示成
		// foo@x.com 这种无害变化被判成换了订阅。account_uuid 是十六进制串，
		// 对它做大小写折叠不会引入歧义。
		if !strings.EqualFold(storedValue, newValue) {
			slog.Warn("[SupplierReauth] identity mismatch, refusing to rebind",
				"account_id", account.ID, "identity_key", string(key))
			return ErrSupplierReauthIdentityMismatch
		}

		// 第二道：这个身份在库里对应的行必须就是本号。
		existingID, err := s.repo.FindAccountIDByUpstreamIdentity(ctx, account.Platform, key, newValue)
		if err != nil {
			// 查询失败往上抛。同 rejectDuplicateSubscription：这道闸的开关不能
			// 建立在「数据库这一刻是否健康」之上。
			return err
		}
		if existingID > 0 && existingID != account.ID {
			slog.Warn("[SupplierReauth] identity belongs to another account, refusing to rebind",
				"account_id", account.ID, "existing_account_id", existingID, "identity_key", string(key))
			return ErrSupplierAccountAlreadyBound
		}
		return nil
	}

	// 一个两边都有值的键都没有。可能是号上没存身份（历史脏数据），也可能是这次
	// 上游没吐——两种都判不了「是不是同一份订阅」，都拒绝。
	slog.Warn("[SupplierReauth] no comparable identity key, refusing to rebind",
		"account_id", account.ID)
	return ErrSupplierReauthUnsupported
}

// supplyNeedsReauth 判定「这个号现在需要主人重新授权一次」。
//
// 界面上那个徽章和那个按钮的唯一依据。放在这个文件里而不是 view 构造函数旁边，
// 是为了让它紧挨着它所守护的那个端点——判据变了，端点的前提也就变了。
//
// 两支为真：
//
//  1. status 已经是 error。凭证失效的**终态**：真实流量上的 401
//     （RateLimitService.handleAuthError）、探测的 401（见
//     supplier_lifecycle_service.go 的 probeOnce）、token 刷新拿到 invalid_grant，
//     三条路都汇到这里。
//  2. 探测已经看见了 401、但状态还没被翻过来的那一段。这一支覆盖两种存量：
//     本次改动上线之前就卡在观察期里 401 循环的号（它们的 status 还是 active），
//     以及一次 401 与紧随其后的 SetError 之间的窗口。
//     没有它，那些号要等到下一次探测才显示出按钮——而它们已经坏了很多天了。
//
// 两支为假是刻意的：
//
//   - **中转号**：它走不了 OAuth 重新授权（见 reauthEligible）。给它挂这个徽章
//     只会把人骗进一个必然被拒的流程。
//   - **已下线的号**：他自己按的按钮，不是坏了。
//   - **status=disabled**：那是管理员停用的，让他去重新授权是让他原地转圈。
//     这种号仍然会在「账号状态」那一栏里如实显示。
func supplyNeedsReauth(account *Account) bool {
	if account == nil || !account.IsOAuth() {
		return false
	}
	if supplyStateOf(account) == SupplyStateRetired {
		return false
	}
	if account.Status == StatusError {
		return true
	}
	// 与 probeOnce 升级状态用的是**同一个**判据函数——徽章与状态机因此不可能各说各话。
	return supplyProbeAuthFailure(supplyExtraString(account, SupplyProbeErrorExtraKey))
}
