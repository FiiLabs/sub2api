//go:build integration

// APEXONE-EXT: 双边市场——两个后台任务的多实例选主真库验证。
//
// # 为什么这个文件在 repository 而不在 service
//
// 选主的判定逻辑（tryAcquireSingletonLeaderLock）住在 service，锁的实现
// （leaderLockCache，SETNX + compare-and-delete 的 Lua）住在 repository。
// 真正跑在生产上的是这两件东西**组装起来**的样子，而能同时看见它们的包只有
// repository——service 不能反向 import repository。所以组装态的验证只能在这里做。
//
// # 在这之前已经有什么、还差什么
//
// 已有的是两组单进程测试：service/leader_lock_test.go 用一个内存假锁验证
// 「拿到/被挡住/释放后再拿到」的分支走法，repository/leader_lock_cache_test.go 用
// miniredis 验证 SETNX 与 compare-and-delete 的语义。两者都没能回答部署时真正关心的
// 那个问题——**N 个实例同时起来，到底有几个跑了这一轮**。
//
// 差的是三件只有真后端能答的事：
//
//  1. 真 Redis 下的互斥。miniredis 是 Go 重写的一份实现，SETNX 的 NX 语义与
//     EVAL 的原子性都是它自己的近似；生产上是真 redis-server。
//  2. Postgres advisory lock 这条兜底路径。它一次都没被执行过——假锁测试里
//     db 恒为 nil，「Redis 报错就退到数据库」那个分支只验证了「不 panic」。
//     而这条路径有一个真库才能暴露的前提：pg_try_advisory_lock 是**会话级**的，
//     实现必须把锁钉在一条独占连接上（db.Conn），否则连接被还回池子后
//     另一个实例可能拿到同一条会话，advisory lock 在同会话内可重入 —— 互斥直接消失。
//  3. 两个任务的锁键确实不同。同键会让解冻和生命周期互相饿死，而这种错误
//     （复制常量时忘了改字符串）在单元测试里长得和正常完全一样。
//
// # 为什么用真的服务而不是直接调那个 helper
//
// 直接测 helper 只能证明 helper 对。这里从 SupplierThawService.Start() 打进去，
// 走的是生产完全一样的路径：同一个 instanceID、同一个锁键、同一个 defer release()。
// 任务体本身用 gate 挡住（那是被验证对象之外的东西），仓储用一个只实现被调到的那个
// 方法的桩，其余方法交给内嵌的 nil 接口——被调到就 panic，正好说明桩的假设过期了。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"hash/fnv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	redisclient "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// 生产上真正写进 Redis 的键。刻意在测试里再抄一遍字面量而不是引用常量：
// 这里要钉的就是「键名有没有被改过」。滚动发布时新旧两版实例共存，键名一变
// 两边各自选出一个 leader，观察期探测会把供给者的额度烧两份——而两边的日志
// 都显示自己是 leader，没有任何一处会报错。
const (
	thawLeaderRedisKey      = "leader:lock:supplier:credit:thaw:leader"
	lifecycleLeaderLockKey  = "supplier:lifecycle:leader"
	leaderLockTestTimeout   = 5 * time.Second
	leaderLockTestFleetSize = 8
)

// ============================================================================
// 桩与夹具
// ============================================================================

// gatedThawRepo 是解冻任务的仓储桩：记录进入次数与并发峰值，并可以把任务体
// 卡在里面，模拟「一轮扫描还没跑完」。
//
// 内嵌接口而不是实现全部 8 个方法：这一轮只会调 ThawAllMaturedUsers，别的方法
// 被调到就会打在 nil 接口上直接 panic——那是想要的结果，说明任务体变了而这个桩
// 的假设已经过期，比静默返回零值好。
type gatedThawRepo struct {
	service.SupplierCreditRepository

	entered chan struct{}
	hold    chan struct{}

	mu          sync.Mutex
	runs        int
	inFlight    int
	maxInFlight int
}

func newGatedThawRepo() *gatedThawRepo {
	return &gatedThawRepo{
		entered: make(chan struct{}, leaderLockTestFleetSize),
		hold:    make(chan struct{}),
	}
}

func (r *gatedThawRepo) ThawAllMaturedUsers(_ context.Context, _ int) (int, float64, error) {
	r.mu.Lock()
	r.runs++
	r.inFlight++
	if r.inFlight > r.maxInFlight {
		r.maxInFlight = r.inFlight
	}
	r.mu.Unlock()

	// 非阻塞投递：缓冲满了也不能把任务体卡死在这里，那会让「谁进来了」这个
	// 观测手段反过来影响被观测的行为。
	select {
	case r.entered <- struct{}{}:
	default:
	}

	<-r.hold

	r.mu.Lock()
	r.inFlight--
	r.mu.Unlock()
	return 0, 0, nil
}

func (r *gatedThawRepo) stats() (runs, maxInFlight int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs, r.maxInFlight
}

// waitEntered 等到有一个实例真的进了任务体（也就是它此刻正握着锁）。
func (r *gatedThawRepo) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-r.entered:
	case <-time.After(leaderLockTestTimeout):
		t.Fatal("没有任何实例进入任务体：选主把所有人都挡住了")
	}
}

// countingLeaderLockCache 包在**真** Redis 实现外面，只数调用次数。
// 数的是「尝试」而不是「胜出」——测试需要知道全部实例都已经撞过锁了，
// 才能断言「只有一个跑了」是互斥的结果，而不是别人还没来得及跑。
type countingLeaderLockCache struct {
	inner service.LeaderLockCache

	mu       sync.Mutex
	attempts int
	wins     int
}

func (c *countingLeaderLockCache) TryAcquireLeaderLock(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	ok, err := c.inner.TryAcquireLeaderLock(ctx, key, owner, ttl)
	c.mu.Lock()
	c.attempts++
	if ok {
		c.wins++
	}
	c.mu.Unlock()
	return ok, err
}

func (c *countingLeaderLockCache) ReleaseLeaderLock(ctx context.Context, key, owner string) error {
	return c.inner.ReleaseLeaderLock(ctx, key, owner)
}

func (c *countingLeaderLockCache) counts() (attempts, wins int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts, c.wins
}

// erroringLeaderLockCache 模拟 Redis 挂了/超时。它必须返回 error 而不是
// (false, nil)：后者的语义是「锁被别人拿着」，那是要跳过这一轮；只有 error
// 才会触发退到数据库 advisory lock 的那条分支。
type erroringLeaderLockCache struct {
	mu       sync.Mutex
	attempts int
}

func (c *erroringLeaderLockCache) TryAcquireLeaderLock(_ context.Context, _, _ string, _ time.Duration) (bool, error) {
	c.mu.Lock()
	c.attempts++
	c.mu.Unlock()
	return false, errors.New("redis unreachable")
}

func (c *erroringLeaderLockCache) ReleaseLeaderLock(_ context.Context, _, _ string) error {
	return errors.New("redis unreachable")
}

func (c *erroringLeaderLockCache) attemptCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attempts
}

// thawFleet 是一组共用同一份仓储与同一个锁后端的解冻任务实例，模拟 N 个进程。
type thawFleet struct {
	repo      *gatedThawRepo
	instances []*service.SupplierThawService
	release   sync.Once
}

// newThawFleet 构造 n 个实例但还不启动。
//
// interval 取一小时：这些测试只关心「启动即跑」的那一轮，让 ticker 在测试中途
// 再敲一次只会把断言变成时间的函数。
func newThawFleet(t *testing.T, n int, cache service.LeaderLockCache, db *sql.DB) *thawFleet {
	t.Helper()

	fleet := &thawFleet{repo: newGatedThawRepo()}
	for i := 0; i < n; i++ {
		svc := service.NewSupplierThawService(fleet.repo, time.Hour)
		svc.SetLeaderLock(cache, db)
		fleet.instances = append(fleet.instances, svc)
	}

	// 收尾顺序是死的：先放行任务体，再 Stop。反过来 Stop 会一直等那个卡在
	// gate 上的 goroutine，测试挂死在 cleanup 里。
	t.Cleanup(func() {
		fleet.openGate()
		fleet.stopAll()
	})
	return fleet
}

func (f *thawFleet) startAll() {
	for _, svc := range f.instances {
		svc.Start()
	}
}

// openGate 放行所有卡在任务体里的实例。
func (f *thawFleet) openGate() {
	f.release.Do(func() { close(f.repo.hold) })
}

func (f *thawFleet) stopAll() {
	var wg sync.WaitGroup
	for _, svc := range f.instances {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.Stop()
		}()
	}
	wg.Wait()
}

// waitAttempts 等到锁后端被撞过 want 次。
func waitAttempts(t *testing.T, count func() int, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return count() >= want }, leaderLockTestTimeout, 5*time.Millisecond,
		"有实例始终没有尝试选主，期望 %d 次尝试", want)
}

// ============================================================================
// 真 Redis
// ============================================================================

// 一个实例正握着锁时，后来的实例必须一个都进不来；它释放之后，新的实例又能进来。
//
// 顺序是刻意串起来的（先确认握锁、再启动后来者），所以断言是确定的，不依赖任何
// sleep 或时序运气：后来者启动的那一刻，锁**确实**在 Redis 里被别人持有。
func TestSupplierThawLeaderLock_RealRedisLocksOutPeersUntilRelease(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := &countingLeaderLockCache{inner: NewLeaderLockCache(rdb)}

	holder := newThawFleet(t, 1, cache, nil)
	holder.startAll()
	holder.repo.waitEntered(t)

	// 锁此刻真的躺在 Redis 里，而且键名就是生产上那个。
	owner, err := rdb.Get(ctx, thawLeaderRedisKey).Result()
	require.NoError(t, err, "持锁期间 Redis 里没有这个键：键名被改过，或者根本没走缓存路径")
	require.NotEmpty(t, owner, "锁的值应当是持有者的 instance id")

	ttl, err := rdb.TTL(ctx, thawLeaderRedisKey).Result()
	require.NoError(t, err)
	// TTL 是崩溃兜底，必须真的设上——没有 TTL 的锁在实例被 kill -9 后永久饿死后来者。
	assertTTLWithin(t, ttl, time.Minute, 5*time.Minute)

	// 后来者：全部启动、全部跑完这一轮，一个都不该进任务体。
	// 先放行它们自己的 gate——万一互斥失效，表现应当是一条断言失败，
	// 而不是一个卡在 gate 上、只能等 go test 超时的死锁。
	peers := newThawFleet(t, 3, cache, nil)
	peers.openGate()
	peers.startAll()
	peers.stopAll()

	peerRuns, _ := peers.repo.stats()
	require.Zero(t, peerRuns, "锁被别人持有时仍有实例跑了这一轮")

	// 放行并等持有者退出，release() 在 runOnce 的 defer 里，Stop 返回时一定已经执行。
	holder.openGate()
	holder.stopAll()

	_, err = rdb.Get(ctx, thawLeaderRedisKey).Result()
	require.ErrorIs(t, err, redisclient.Nil, "任务跑完后锁没有被释放")

	// 释放之后不能有永久饿死：新起的实例必须能拿到。
	late := newThawFleet(t, 1, cache, nil)
	late.openGate() // 不必再卡住，直接跑完
	late.startAll()
	late.stopAll()

	lateRuns, _ := late.repo.stats()
	require.Equal(t, 1, lateRuns, "锁释放后新实例仍然进不来")
}

// N 个实例同时起来（生产上的滚动发布就是这个样子）时不能有踩踏。
//
// 断言 maxInFlight==1 而不只是 runs==1：前者是真正的不变量——即使时序恰好让第二个
// 实例在第一个释放之后才拿到锁，「同时有两个在跑」也仍然必须为假。
func TestSupplierThawLeaderLock_RealRedisNoStampedeOnSimultaneousStart(t *testing.T) {
	cache := &countingLeaderLockCache{inner: NewLeaderLockCache(testRedis(t))}

	fleet := newThawFleet(t, leaderLockTestFleetSize, cache, nil)
	fleet.startAll()

	// 等到八个实例都撞过锁，这时「只有一个跑了」才是互斥的结论而不是抢跑的假象。
	waitAttempts(t, func() int { attempts, _ := cache.counts(); return attempts }, leaderLockTestFleetSize)

	attempts, wins := cache.counts()
	require.Equal(t, leaderLockTestFleetSize, attempts)
	require.Equal(t, 1, wins, "真 Redis 下选出了 %d 个 leader", wins)

	runs, maxInFlight := fleet.repo.stats()
	require.Equal(t, 1, runs, "%d 个实例里跑了 %d 轮", leaderLockTestFleetSize, runs)
	require.Equal(t, 1, maxInFlight, "同一时刻有 %d 个实例在跑同一个任务", maxInFlight)
}

// ============================================================================
// Postgres advisory lock 兜底
// ============================================================================

// advisoryLockID 复算一遍 service 侧的 fnv64a，用来在 pg_locks 里认出这把锁。
//
// 这是刻意的独立重算而不是引用常量：它同时钉住「锁 id 由哪个字符串算出来」。
// 退到数据库这条路径如果算错了键，表现是两个任务共用一把锁——互相饿死，
// 但两边都不会报任何错。
func advisoryLockID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64())
}

// countGrantedAdvisoryLock 数这把 advisory lock 此刻被授予了几次。
//
// pg_locks 把 bigint 形式的锁 id 拆成 classid（高 32 位）与 objid（低 32 位），
// objsubid=1 表示它是单参数 bigint 形式（两参数 int/int 形式是 2）。
func countGrantedAdvisoryLock(t *testing.T, key string) int {
	t.Helper()
	id := uint64(advisoryLockID(key))

	var n int
	err := integrationDB.QueryRowContext(context.Background(), `
		SELECT count(*) FROM pg_locks
		WHERE locktype = 'advisory'
		  AND granted
		  AND objsubid = 1
		  AND classid::bigint = $1
		  AND objid::bigint = $2`,
		int64(id>>32), int64(id&0xFFFFFFFF)).Scan(&n)
	require.NoError(t, err, "查 pg_locks")
	return n
}

// Redis 报错时必须退到 Postgres advisory lock，而不是所有实例一起跑。
//
// 这是最值钱的一条：Redis 抖动恰恰是最容易同时打到所有实例的故障，如果这一步
// 退化成「大家都跑」，一次 Redis 抖动就会让观察期探测按实例数倍烧供给者的额度。
//
// 它同时是唯一钉住「持锁必须独占一条连接」的测试。pg_try_advisory_lock 是会话级、
// 且在同一会话内可重入的：把实现里的 db.Conn 换成 db.QueryRow 之后，持锁那条连接
// 会在语句结束时被还回池子，后来者从池子里拿到同一条会话就能重入成功——互斥静默消失。
// 本例的后来者是在持有者进入任务体**之后**才启动的，池子里那条空闲连接正握着锁，
// 于是这个错误会稳定地在这里现形（已用改坏实现的方式验证过）。
func TestSupplierThawLeaderLock_FallsBackToPostgresAdvisoryLockWhenRedisErrors(t *testing.T) {
	cache := &erroringLeaderLockCache{}

	holder := newThawFleet(t, 1, cache, integrationDB)
	holder.startAll()
	holder.repo.waitEntered(t)

	// 走的确实是数据库这条路：锁真的在 pg_locks 里，而且 id 由那个键算出来。
	require.Equal(t, 1, countGrantedAdvisoryLock(t, "supplier:credit:thaw:leader"),
		"Redis 报错后没有在 Postgres 上真正持锁——这一轮是裸跑的")

	peers := newThawFleet(t, 3, cache, integrationDB)
	peers.openGate()
	peers.startAll()
	peers.stopAll()

	peerRuns, _ := peers.repo.stats()
	require.Zero(t, peerRuns, "advisory lock 没有挡住后来者")
	require.GreaterOrEqual(t, cache.attemptCount(), 4, "后来者没有先尝试 Redis")

	holder.openGate()
	holder.stopAll()

	// 释放要连会话一起收掉：连接不还池、锁不解开，两者都会让下一轮永远进不来。
	require.Zero(t, countGrantedAdvisoryLock(t, "supplier:credit:thaw:leader"),
		"任务跑完后 advisory lock 仍被持有")

	late := newThawFleet(t, 1, cache, integrationDB)
	late.openGate()
	late.startAll()
	late.stopAll()

	lateRuns, _ := late.repo.stats()
	require.Equal(t, 1, lateRuns, "advisory lock 释放后新实例仍然进不来")
}

// 同样地，advisory lock 这条路径下 N 个实例同时起来也不能踩踏。
//
// 与 Redis 那条的区别在于争抢发生在数据库连接层：N 个实例同时向同一个连接池要连接，
// 每条连接是一个独立会话，锁在会话之间才有意义。这一例证明的是「同时起跑」这个
// 时序下互斥依然成立；「连接被还回池子导致重入」那个更阴的故障由上一例负责。
func TestSupplierThawLeaderLock_AdvisoryLockNoStampedeOnSimultaneousStart(t *testing.T) {
	cache := &erroringLeaderLockCache{}

	fleet := newThawFleet(t, leaderLockTestFleetSize, cache, integrationDB)
	fleet.startAll()

	waitAttempts(t, cache.attemptCount, leaderLockTestFleetSize)

	runs, maxInFlight := fleet.repo.stats()
	require.Equal(t, 1, runs, "%d 个实例里跑了 %d 轮", leaderLockTestFleetSize, runs)
	require.Equal(t, 1, maxInFlight, "同一时刻有 %d 个实例在跑同一个任务", maxInFlight)
	require.Equal(t, 1, countGrantedAdvisoryLock(t, "supplier:credit:thaw:leader"))
}

// ============================================================================
// 两个任务的锁互不干扰
// ============================================================================

// lifecycleScanRepo 只实现生命周期任务这一轮会调到的那个方法，并且永远返回空列表——
// 这个测试要证明的是「它跑了」，不是它扫出了什么。
type lifecycleScanRepo struct {
	service.SupplierOnboardingRepository

	mu    sync.Mutex
	scans int
}

func (r *lifecycleScanRepo) ListAccountIDsBySupplyState(_ context.Context, state string, _ int) ([]int64, error) {
	if state == service.SupplyStateDraining {
		r.mu.Lock()
		r.scans++
		r.mu.Unlock()
	}
	return nil, nil
}

func (r *lifecycleScanRepo) scanCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scans
}

// lifecycleAccountRepoStub 只是为了让 Start() 的非空判断过关；列表恒为空，
// 它一个方法都不会被调到。
type lifecycleAccountRepoStub struct {
	service.AccountRepository
}

// 解冻与生命周期必须用两把不同的锁。
//
// 同键的后果是互相饿死：解冻每 10 分钟跑一次并且很快，生命周期每 5 分钟跑一次
// 且要发真实探测（可能几分钟），谁先起来谁就长期把对方按住。而两边的日志都只会
// 显示「这一轮不是我当 leader」，看起来完全正常。
func TestSupplierLeaderLocks_ThawAndLifecycleUseDistinctKeys(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewLeaderLockCache(rdb)

	// 解冻实例进任务体并一直握着自己的锁。
	holder := newThawFleet(t, 1, cache, nil)
	holder.startAll()
	holder.repo.waitEntered(t)
	require.NoError(t, rdb.Get(ctx, thawLeaderRedisKey).Err(), "解冻锁应当被持有")

	// 此时生命周期任务必须照常跑完一轮。
	scanRepo := &lifecycleScanRepo{}
	lifecycle := service.NewSupplierLifecycleService(scanRepo, &lifecycleAccountRepoStub{}, nil, nil, time.Hour)
	lifecycle.SetLeaderLock(cache, nil)
	lifecycle.Start()
	lifecycle.Stop()

	require.Equal(t, 1, scanRepo.scanCount(),
		"解冻任务持锁期间生命周期任务被挡住了：两者共用了同一个锁键")

	// 生命周期跑完也会释放自己那把锁，而解冻那把还在。
	require.NoError(t, rdb.Get(ctx, thawLeaderRedisKey).Err(), "生命周期任务释放锁时误删了解冻的锁")
	require.ErrorIs(t, rdb.Get(ctx, "leader:lock:"+lifecycleLeaderLockKey).Err(), redisclient.Nil,
		"生命周期任务跑完后没有释放自己的锁")
}
