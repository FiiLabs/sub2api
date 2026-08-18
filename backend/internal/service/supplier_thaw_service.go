// APEXONE-EXT: 双边市场——冻结额到期释放。
//
// 为什么需要一个后台任务，而不是照抄 affiliate 的「读仪表盘时顺手解冻」：
// affiliate 的返利额只有一个出口——在面板上看、然后花掉，用户不打开面板就不会用到它，
// 懒解冻够用。供给者的 credit 不一样，它的主出口是**抵扣自己发起的 API 请求**，
// 那条路径上没有人打开过任何页面。只做懒解冻的话，一个从不登面板、只用 API 的供给者
// 的钱会永远躺在冻结区，每次请求照扣 users.balance——功能在他身上等于不存在。
//
// 所以两条都做：这里是兜底的周期扫描，仪表盘读取时另有一次即时解冻（低延迟体感）。
// 两者都幂等，重复跑不会多搬一分钱（到期流水被 UPDATE ... WHERE frozen_until <= NOW()
// 一次性摘掉，第二次扫不到）。
package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// supplierThawLeaderLockKey 多实例下只让一个实例跑扫描。
	// 解冻本身幂等，加锁是为了不让 N 个实例同时对同一批钱包行抢锁互相阻塞。
	supplierThawLeaderLockKey = "supplier:credit:thaw:leader"
	// supplierThawLeaderLockTTL 必须大于单轮扫描的超时，否则锁会在跑到一半时过期。
	supplierThawLeaderLockTTL = 5 * time.Minute
	// supplierThawRunTimeout 单轮扫描超时。
	supplierThawRunTimeout = 2 * time.Minute
	// SupplierThawDefaultInterval 默认扫描间隔。
	//
	// 冻结窗以天计，解冻晚几分钟毫无影响，所以刻意取一个稀疏的值：
	// 这个任务在没有到期流水时也会扫一次索引，跑太勤只是白费数据库。
	SupplierThawDefaultInterval = 10 * time.Minute
	// supplierThawBatchLimit 单轮最多处理多少个供给者。
	// 剩下的下一轮继续——补偿型任务不需要一次做完，需要的是永远不会把库压垮。
	supplierThawBatchLimit = 500
)

// SupplierThawService 周期性把到期的冻结 credit 搬进可用区。
type SupplierThawService struct {
	repo     SupplierCreditRepository
	interval time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
}

func NewSupplierThawService(repo SupplierCreditRepository, interval time.Duration) *SupplierThawService {
	if interval <= 0 {
		interval = SupplierThawDefaultInterval
	}
	return &SupplierThawService{
		repo:       repo,
		interval:   interval,
		stopCh:     make(chan struct{}),
		instanceID: uuid.NewString(),
	}
}

// SetLeaderLock 注入选主用的缓存与数据库。两者都为 nil 时任务不选主直接跑
// （单实例部署与测试的行为）。
func (s *SupplierThawService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	if s == nil {
		return
	}
	s.lockCache = lockCache
	s.db = db
}

func (s *SupplierThawService) Start() {
	if s == nil || s.repo == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		// 启动即跑一次：进程重启后不必等满一个 interval 才补上停机期间到期的额度。
		s.runOnce()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *SupplierThawService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *SupplierThawService) runOnce() {
	if s == nil || s.repo == nil {
		return
	}

	lockCtx, lockCancel := context.WithTimeout(context.Background(), 2*time.Second)
	release, ok := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, supplierThawLeaderLockKey, s.instanceID, supplierThawLeaderLockTTL)
	lockCancel()
	if !ok {
		return
	}
	defer release()

	// 独立于任何请求 context：这是后台任务，不该被别人的取消牵连。
	runCtx, cancel := context.WithTimeout(context.Background(), supplierThawRunTimeout)
	defer cancel()

	users, total, err := s.repo.ThawAllMaturedUsers(runCtx, supplierThawBatchLimit)
	if err != nil {
		// 只记不重试：下一轮 ticker 自然会重来，且解冻幂等。
		slog.Error("[SupplierThaw] failed to release matured supplier credit", "error", err,
			"thawed_users", users, "thawed_amount", total)
		return
	}
	if users > 0 {
		slog.Info("[SupplierThaw] released matured supplier credit", "users", users, "amount", total)
	}
}
