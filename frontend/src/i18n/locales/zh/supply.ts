// APEXONE-EXT: 双边市场——供给侧文案。
// 注意：vue-i18n 把 "@" 和 "|" 当特殊语法，字面 @ 要写成 {'@'}。
export default {
  supply: {
    // navLabel 单独一条而不是复用 title：侧边栏放不下一句话。
    // 也刻意不进 common.ts 的 nav 命名空间——那是上游文件，本模块整体是纯新增。
    navLabel: '共享订阅',
    title: '共享我的订阅',
    description: '把你的 AI 订阅接进来，闲置额度被别人用掉时你按比例分成。',

    disabled: {
      title: '供给市场尚未开放',
      body: '这个站点还没有配置供给池。等管理员配好之后，你就能在这里接入自己的订阅。'
    },

    settlementOff: {
      title: '当前未在计费',
      body: '结算开关处于关闭状态：现在挂上来的号可以正常接入，但产生的用量暂时不会入账。已经攒下的余额不受影响。'
    },

    wallet: {
      title: '我的收益',
      available: '可用余额',
      availableHint: '已过冻结期，可用于抵扣消费',
      frozen: '冻结中',
      frozenHint: '过了冻结期会自动转入可用余额',
      history: '累计收益',
      spent: '已消费',
      refresh: '刷新'
    },

    // 协议同意。四种状态四句话：未发布 / 没同意过 / 同意的是旧版 / 已同意。
    // 对一个明明点过同意的人说"请先同意"，他会以为系统坏了，所以 title 和
    // updatedTitle 必须分开。
    agreement: {
      title: '接入前请先阅读并同意供给者协议',
      updatedTitle: '供给者协议已更新，请重新确认',
      version: '当前版本：{version}',
      openFullText: '查看协议全文',
      checkbox: '我已阅读并同意 {version} 版《供给者协议》',
      accept: '同意并继续',
      accepting: '正在提交…',
      acceptedToast: '已记录你的同意',
      acceptedAt: '你已于 {time} 同意 {version} 版协议。',
      unpublishedTitle: '平台尚未发布供给者协议',
      unpublishedBody: '在协议发布之前无法接入订阅。这一步只有管理员能完成，请联系站点管理员。'
    },

    connect: {
      title: '接入一个新订阅',
      description: '整个过程分两步：先去授权，再把拿到的授权码贴回来。',
      start: '连接我的订阅',
      starting: '正在生成授权链接…',
      step1: '第 1 步：在新标签页里完成授权',
      step1Hint: '点下面的链接会打开官方授权页。授权完成后页面会给你一段授权码。',
      openAuthUrl: '打开授权页面',
      copyAuthUrl: '复制链接',
      authUrlCopied: '授权链接已复制',
      step2: '第 2 步：把授权码贴回来',
      codeLabel: '授权码',
      codePlaceholder: '粘贴授权页面给你的那段码',
      nameLabel: '给这个号起个名字（可选）',
      namePlaceholder: '留空则用上游账号邮箱',
      submit: '完成接入',
      submitting: '正在接入…',
      cancel: '取消',
      success: '接入成功，这个号已进入观察期',
      expiryHint: '授权链接 15 分钟内有效，过期请重新发起。',
      pendingHint: '接入后账号会先进入观察期，暂不接受调度；平台核验通过后才会开始接单。'
    },

    accounts: {
      title: '我接入的订阅',
      empty: '你还没有接入任何订阅。',
      name: '名称',
      platform: '平台',
      state: '接入状态',
      status: '账号状态',
      createdAt: '接入时间',
      lastUsedAt: '最近使用',
      actions: '操作',
      never: '从未使用',
      pause: '下线',
      pausing: '正在下线…',
      pauseNow: '立即下线',
      resume: '重新挂回',
      resuming: '正在挂回…',
      cancelPause: '取消下线',
      pauseHint: '「下线」会先进入排空期，期间不再接新单，你随时可以反悔；「立即下线」直接进终态，要再上线得重走观察期。两者都停不掉已经在传输中的请求——那部分请求会正常跑完。',
      pauseConfirm: '下线后这个号立即停止接新单，进入排空期，期间你还可以取消。已产生的收益不受影响。确定要下线吗？',
      pauseNowConfirm: '立即下线会直接进入终态，不能取消；要再上线需要重新走一遍观察期。已经在传输中的请求仍会跑完。确定吗？',
      paused: '已下线',
      detach: '解绑',
      detaching: '正在解绑…',
      // 确认文案的全部工作是把「下线」和「解绑」的差别摆出来：前者凭证还在平台手里，
      // 后者才是把它删掉。含糊过去会让想彻底退出的人以为点了下线就够了。
      detachConfirm:
        '解绑「{name}」会立即停止接单，并把平台保存的这个号的授权凭证彻底删除，之后平台再也无法使用它。此操作不可撤销。已产生的收益不受影响，仍可正常提取。确定要解绑吗？',
      detached: '已解绑，平台不再持有这个号的凭证',
      // 上游那边的授权记录还在——Anthropic 没有可供平台调用的撤销接口。
      // 不说这句话，用户会以为解绑等于上游也一并撤销了。
      detachUpstreamHint:
        '平台这边的凭证已经删除。如需在 Anthropic 一侧一并撤销授权，请前往 claude.ai 的账号设置里移除对应的 Claude Code 授权。',
      draining: '已停止接新单，进入排空期',
      pauseCancelled: '已取消下线，这个号继续接单',
      resumed: '已重新挂回，将重新进入观察期',
      schedulable: '接单中',
      notSchedulable: '未接单',
      probePasses: '已连续通过 {passes} 次探测',
      eligibleAt: '不早于 {time} 满足观察时长',
      probeError: '探测失败：{reason}。多半需要你重新授权这个号。',
      drainUntil: '排空期至 {time}'
    },

    state: {
      pending_review: '观察期',
      active: '已上线',
      draining: '排空中',
      retired: '已下线',
      unknown: '未知'
    },

    stateHint: {
      pending_review: '平台核验中，暂不接单',
      active: '正常接单中',
      draining: '已停止接新单，排空期结束后转为已下线',
      retired: '你主动下线了这个号'
    },

    ledger: {
      title: '收益流水',
      empty: '还没有任何流水。',
      time: '时间',
      action: '类型',
      amount: '金额',
      basis: '计费基数',
      ratio: '分成比例',
      frozenUntil: '解冻时间',
      remark: '备注',
      prev: '上一页',
      next: '下一页',
      pageInfo: '第 {page} / {pages} 页，共 {total} 条'
    },

    action: {
      accrue: '入账',
      spend: '消费',
      thaw: '解冻',
      clawback: '追回',
      withdraw: '提现',
      // 与 withdraw 分开说而不是合成一句"提现"：一笔退回在账单上必须
      // 一眼看得出是退回来的钱，否则它读起来像一笔来路不明的收入。
      withdraw_revert: '提现退回',
      unknown: '其他'
    },

    // 提现。三句话必须写在用户看得见的地方，因为它们都是会被误解的：
    //   1. 申请的那一刻钱就扣了，不是审批时才扣；
    //   2. 低于起提额会被拒绝，不会被夹到起提额；
    //   3.「开着但没配渠道」是平台的问题，不是他填错了。
    withdrawal: {
      title: '提现',
      closedTitle: '提现暂未开放',
      closedBody: '平台还没有开放提现。余额不会消失，等开放后可以一次性申请。',
      channelsMissingTitle: '提现渠道维护中',
      channelsMissingBody: '提现已开启，但平台还没有配好收款渠道。这是平台侧的配置问题，请联系管理员，不用重复尝试。',
      deductHint: '提交申请时金额会立刻从可用余额中扣除，而不是等审核通过才扣。审核未通过或你自己撤回时，这笔钱会原路退回可用余额。',
      pendingCount: '待处理 {count} / {max} 张',
      amountLabel: '提现金额（起提 {min}）',
      useAll: '全部可用余额（{amount}）',
      channelLabel: '收款渠道',
      channelPlaceholder: '请选择收款渠道',
      accountLabel: '收款账号',
      accountPlaceholder: '填写对应渠道的收款账号',
      accountHint: '请仔细核对：打款按这里填的账号进行，填错导致的损失无法追回。',
      noteLabel: '备注（选填）',
      notePlaceholder: '需要让运营知道的信息',
      submit: '提交提现申请',
      submitting: '提交中…',
      submitted: '申请已提交，金额已从可用余额中扣除。',
      empty: '还没有提现记录。',
      createdAt: '申请时间',
      amount: '金额',
      channel: '收款方式',
      status: '状态',
      reviewNote: '处理说明',
      actions: '操作',
      externalRef: '打款凭证：{ref}',
      cancel: '撤回',
      cancelling: '撤回中…',
      cancelConfirm: '确定要撤回这张 {amount} 的提现申请吗？撤回后金额会退回可用余额。',
      cancelled: '已撤回，金额已退回可用余额。',

      state: {
        pending: '待处理',
        paid: '已打款',
        rejected: '已拒绝',
        canceled: '已撤回',
        unknown: '未知'
      }
    },

    error: {
      loadFailed: '加载失败',
      startFailed: '发起授权失败',
      completeFailed: '接入失败',
      pauseFailed: '下线失败',
      resumeFailed: '挂回失败',
      detachFailed: '解绑失败',
      acceptFailed: '同意协议失败',
      codeRequired: '请先填写授权码',
      withdrawalAmountInvalid: '请填写一个大于 0 的提现金额',
      withdrawalChannelRequired: '请选择收款渠道',
      withdrawalAccountRequired: '请填写收款账号',
      withdrawalFailed: '提交提现申请失败',
      withdrawalCancelFailed: '撤回提现申请失败'
    }
  },

  supplyAdmin: {
    navLabel: '双边市场',
    title: '双边市场',
    description: '配置供给者分成、供给池路由、观察期、协议与提现。每组分开保存。',

    settlement: {
      title: '结算参数',
      description: '决定供给者能拿到多少钱、什么时候能动。',
      enabled: '开启结算',
      enabledHint: '关闭时供给账号退化为普通自营账号：照常接单，但不产生任何分成。',
      shareRatio: '分成比例',
      shareRatioHint: '基数是消费者实付金额（不是官方价）。0.7 表示供给者拿 70%。上限 {max}。',
      freezeHours: '冻结小时数',
      freezeHoursHint: '入账后需冻结多久才可动用。必须不小于支付通道的拒付窗口，否则冻结期过后发生拒付，平台只能自吃。上限 {max} 小时。',
      spendFromWalletFirst: '优先从赚取钱包扣费',
      spendFromWalletFirstHint: '开启后，用户消费时先扣赚取余额、再扣充值余额。可以先只开入账、等钱包侧稳了再打开这个出口。',
      save: '保存结算参数',
      saved: '结算参数已保存'
    },

    pool: {
      title: '供给池路由',
      description: '供给池里没有可用账号时，把请求兜到自营池。',
      enabled: '开启溢出',
      enabledHint: '关闭时调度行为与上游原逻辑完全一致。',
      supplyGroupId: '供给池分组 ID',
      supplyGroupIdHint: '只有解析后落在这个分组上的请求才会溢出。这个门开得窄是有意的：如果任何空分组都能往自营池溢，一个配错的分组会静默地拿平台自有账号供货。',
      overflowGroupId: '兜底分组 ID（自营池）',
      overflowGroupIdHint: '必须与供给池分组不同。这里不校验分组是否存在——分组可能在配置之后被删掉，真正的兜底在调度侧。',
      dailyOverflowLimit: '每日溢出上限（次）',
      dailyOverflowLimitHint:
        '0 = 不限量（仍然计数）。配额用完后，供给池耗尽的请求拿回它原本就会拿到的「无可用账号」，不会新增故障面。计数不可用时按不溢出处理——花平台的钱的决定不能建立在「不知道今天花了多少」之上。',
      overflowUsage: '今日（{day}）已溢出 {used} 次，因配额拦下 {denied} 次。',
      costWarning: '每一次溢出，平台都在按自营成本供货却按供给池价收费。溢出率是要盯着的经营指标（服务端日志 [SupplyPool] supply pool exhausted）。',
      save: '保存池配置',
      saved: '池配置已保存'
    },

    probation: {
      title: '观察期与下线',
      description: '新挂上来的号在什么条件下才放进供给池，以及优雅下线等多久。',
      enabled: '开启自动入池',
      enabledHint: '关闭时观察期照常探测、照常记录，但不会自动放行——需要人工把号切成可调度。默认是关的：先看几天数据再打开。',
      minObservation: '最短观察时长（分钟）',
      minObservationHint: '从接入那一刻算起。与"连续成功次数"是并且的关系：探测再顺利也得等满这段时间。上限 {max} 分钟。',
      requiredSuccesses: '需要连续成功次数',
      requiredSuccessesHint: '中间失败一次计数清零，并把失败原因回显给供给者。上限 {max} 次。',
      probeInterval: '探测间隔（分钟）',
      probeIntervalHint: '每次探测都是一个真实的上游请求，花的是供给者自己的额度——间隔太小等于拿人家的额度当探针耗材。区间 {min}–{max} 分钟。',
      probeModel: '探测模型 ID',
      probeModelPlaceholder: '留空则用平台默认测试模型',
      probeModelHint: '填一个便宜的小模型。这个值只影响探测，不影响真实调度。',
      drainWindow: '排空窗（分钟）',
      drainWindowHint: '供给者选"下线"后，等多久才转入终态。这不是硬排空——平台打断不了已经在流的请求，这段时间同时也是供给者反悔的窗口。填 0 则"下线"退化成立即终态。上限 {max} 分钟。',
      clampNotice: '这一组参数越界时后端会夹回区间并保存（不会报错），保存后表单显示的是库里真正存下的值。',
      save: '保存观察期参数',
      saved: '观察期参数已保存'
    },

    // APEXONE-EXT: 接入上限。两道闸的文案分开写，因为它们挡的不是同一件事，
    // 而运营需要在配之前就知道第二道的误伤面有多大。
    onboarding: {
      title: '接入上限',
      description: '一个人、一个出口网络最多能往平台挂几个供给账号。两道闸都是 0 = 不限。',
      maxPerUser: '每人最多账号数',
      maxPerUserHint: '数的是当下未解绑的号，解绑一个就腾出一个位置。这道闸只是礼貌性护栏——再注册一个账号就能绕过它。填 0 = 不限。上限 {max}。',
      maxPerIp: '每个来源 IP 最多账号数',
      maxPerIpHint: '按接入时的出口 IP 精确匹配，跨用户合计。这才是有阻力的那道闸：注册账号免费，换出口网络不免费。默认 0（不限）。上限 {max}。',
      ipWarning: '这道闸开着。运营商级 NAT、校园网、公司出口后面站着的是成百上千个互不相干的真实用户，被它挡下的人只会看到"挂不上号"，不会来报障。请先看过真实的 IP 分布，再配一个远大于"一户人家"的数。',
      clampNotice: '这一组参数越界时后端会夹回区间并保存（不会报错），保存后表单显示的是库里真正存下的值。',
      save: '保存接入上限',
      saved: '接入上限已保存'
    },

    // APEXONE-EXT: 供给者协议。这一组文案的分寸要拿准：协议是法律文本，
    // 后台这一页只负责"发布哪一版"，不负责替运营解释协议写了什么。
    agreement: {
      title: '供给者协议',
      description: '供给者第一次接入前必须点头同意的那份文本。没发布协议，自助接入整个是关着的。',
      publishedNotice: '协议已发布，自助接入的协议门禁生效中：没同意过这一版的人发不起授权。',
      unpublishedNotice: '尚未发布协议——当前任何人都无法自助接入（后端直接拒绝发起授权）。填好版本号并保存即可开放。',
      version: '版本号',
      versionPlaceholder: '例如 v1、2026-08-01',
      versionHint: '清空版本号 = 撤下协议、关闭自助接入。改成一个新值 = 所有已同意的人都要重新同意一次——改错别字也算，请谨慎。',
      url: '协议全文链接',
      urlHint: '可留空。只接受 http/https 的绝对地址，填了会在同意页上多一个"查看全文"的外链。',
      body: '协议正文',
      bodyPlaceholder: '直接粘贴纯文本。前端按纯文本渲染，不解析 HTML 或 Markdown。',
      bodyHint: '可留空（那就只剩外链）。最长 {max} 字符，按字符数算不是字节数。',
      rejectNotice: '这一组字段越界时后端会报错拒绝保存，不会像观察期参数那样悄悄夹回来——协议文本被截断一半是没法接受的。',
      save: '保存协议',
      saved: '协议已保存'
    },

    // 提现参数。这一组的每一句提示都在描述"配错了会怎样"，因为它的失效方式
    // 是静默的：开着但没配渠道，面板一切正常，供给者点提现被硬拒。
    withdrawal: {
      title: '提现',
      description: '决定供给者能不能把余额取走、最少取多少、打到哪些渠道。',
      openNotice: '提现已开放。供给者提交申请时金额立刻从可用余额扣除，等待你在「供给运营」页处理。',
      closedNotice: '提现关闭中。供给者的余额照常累积，只是取不走。',
      noChannelNotice: '提现开着，但一个收款渠道都没配——供给者会看到一个点不动的入口。要么补上渠道，要么直接关掉开关。',
      noNotifyNotice: '提现开着，渠道也配了，但没有任何人会收到新申请的通知——后台不会自己弹出来，而供给者的钱在提交那一刻就已经扣了。请在下面填上收件人。',
      enabled: '开启提现',
      enabledHint: '关闭后余额照常累积，只是不能申请提现。已提交的单子不受影响。',
      minAmount: '起提金额',
      minAmountHint: '低于这个数的申请会被**拒绝**，不会被夹到起提额。上限 {max}。',
      maxPending: '每人未决单上限',
      maxPendingHint: '同一个人同时挂着的待处理单子数。上限 {max}。',
      channels: '收款渠道',
      channelsPlaceholder: '一行一个，例如：\nUSDT-TRC20\n支付宝\n银行卡',
      channelsHint: '一行一个，最多 {max} 个、每个不超过 {len} 字。供给者提交时按**完全相等**匹配（只忽略首尾空格），所以 USDT 和 usdt 是两个不同的渠道，改名等于把老渠道下线。',
      notifyEmails: '新申请通知收件人',
      // vue-i18n 把裸 @ 当 linked-message 语法，必须用 {'@'} 转义，否则整个
      // locale 编译失败（localesMessageCompile.spec.ts 会拦下来）。
      notifyEmailsPlaceholder: "finance{'@'}example.com\nops{'@'}example.com",
      notifyEmailsHint: '一行一个，最多 {max} 个、每个不超过 {len} 字。留空 = 没有任何人知道有单要处理。与配额告警的收件人是两份配置：收钱的是财务，收告警的是运维。格式填错会**保存失败**并告诉你是哪一个，不会被悄悄丢掉。',
      notice: '给供给者的说明',
      noticePlaceholder: '到账时效、手续费、需要提供的信息……',
      noticeHint: '显示在供给者的提现表单上，纯文本，最多 {max} 字。',
      rejectNotice: '越界的参数会被直接拒绝而不是夹回：起提额被悄悄夹到上限，结果是所有人都提不了钱，而面板上看不出任何异常。',
      save: '保存提现参数',
      saved: '提现参数已保存'
    },

    error: {
      loadFailed: '加载配置失败',
      saveFailed: '保存失败'
    }
  },

  // APEXONE-EXT: 运营视图（只读）。与 supplyAdmin（配置页）分开：那一页改参数，
  // 这一页只看数，两组文案的语气也不同——这里全是陈述，没有一句"将会生效"。
  supplyOps: {
    navLabel: '供给运营',
    title: '供给运营',
    description: '看供给侧此刻的状态：要付多少、谁在接单、谁的号坏了、谁卡在观察期。整页只读。',
    search: '查询',
    loading: '加载中…',
    empty: '没有符合条件的数据。',

    window: {
      label: '统计窗口',
      days: '最近 {days} 天'
    },

    overview: {
      owed: '待付负债',
      owedBreakdown: '可用 {available} · 冻结 {frozen}',
      suppliers: '供给者',
      wallets: '其中 {count} 人有钱包记录',
      accounts: '供给账号',
      accountsBreakdown: '已上线 {active} · 正在接单 {schedulable}',
      accrued: '近 {days} 天入账',
      windowBreakdown: '追回 {clawed} · 供给者消费 {spent}',
      unhealthy: '账号异常',
      thawHint: '本窗口解冻搬运 {thawed}，提现申请 {withdrawn}，退回 {reverted}。解冻是钱包内部把冻结额挪进可用额，不要和入账相加——那是同一笔钱数两遍。提现是**申请时**扣的，所以 {withdrawn} 是申请额不是已打款额；退回来的（拒绝/撤回）单独记在 {reverted}，两个数不要相减看净额——退回的笔数本身就是渠道配错或审核标准有问题的信号。'
    },

    roster: {
      title: '供给者名册',
      description: '名下有过供给账号、或钱包里有过余额的人。已注销但仍欠着钱的人也在这里——把他藏起来只会让这笔钱在对账时凭空冒出来。',
      keywordPlaceholder: '邮箱 / 用户名',
      supplier: '供给者',
      accounts: '账号数',
      accountsHint: '在线 {active} · 观察期 {pending} · 异常 {unhealthy}',
      owed: '待付',
      history: '累计入账',
      lastAccrual: '最后入账',
      neverAccrued: '从未入账',
      actions: '操作',
      viewAccounts: '看他的号',
      viewLedger: '看他的账',
      sort: {
        owed: '按待付排序',
        history: '按累计入账排序',
        accounts: '按账号数排序',
        recent: '按最后入账排序'
      }
    },

    accounts: {
      title: '供给账号',
      description: '只含有归属人的号，自营账号不在这里。',
      account: '账号',
      owner: '归属人',
      state: '接入状态',
      health: '上游健康',
      lastUsedAt: '最近使用',
      never: '从未使用',
      anyState: '全部状态',
      anyHealth: '全部健康度',
      healthy: '正常',
      unhealthy: '异常',
      schedulable: '正在接单',
      notSchedulable: '不接单',
      probationSince: '观察期起点 {time}',
      probePasses: '已连续通过 {passes} 次探测',
      probeError: '探测失败：{reason}',
      drainUntil: '排空至 {time}',
      ownerFilter: '只看 #{id} 的号'
    },

    ledger: {
      title: '全站流水',
      description: '所有供给者的钱包流水。对账时手上通常只有一个 request_id，可以直接精确检索。',
      time: '时间',
      user: '收款人',
      action: '类型',
      amount: '金额',
      basis: '计费基数',
      requestId: '请求 ID',
      anyAction: '全部类型',
      requestIdPlaceholder: '按 request_id 精确匹配',
      userFilter: '只看 #{id}'
    },

    // 提现审批：这一页唯一的写路径。文案的语气与其余几块不同——
    // 那几块是陈述，这里每一句都在描述一个不可撤销的动作。
    withdrawals: {
      title: '提现审批',
      description: '钱在供给者提交申请时就已经从可用余额扣走了，这里只推进单子的状态。打款后不可撤销；拒绝会把钱退回他的可用余额。',
      anyStatus: '全部状态',
      userFilter: '只看 #{id}',
      requestedAt: '申请时间',
      user: '供给者',
      amount: '金额',
      payout: '收款方式',
      status: '状态',
      actions: '操作',
      markPaid: '标记已打款',
      markPaidConfirm: '确认已经向 {account} 打款 {amount}？这一步没有撤销，标记后金额不会退回。',
      externalRefPrompt: '填写打款凭证 / 交易号（没有可留空，但纠纷时这是双方唯一的共同凭据）',
      markedPaid: '已标记为已打款。',
      reject: '拒绝',
      rejectPrompt: '拒绝这张 {amount} 的申请。请填写理由——它会原样显示给供给者，是他唯一能拿到的解释：',
      rejected: '已拒绝，金额已退回供给者的可用余额。'
    },

    error: {
      overviewFailed: '加载看板失败',
      rosterFailed: '加载名册失败',
      accountsFailed: '加载账号失败',
      ledgerFailed: '加载流水失败',
      withdrawalsFailed: '加载提现单失败',
      withdrawalResolveFailed: '处理提现单失败',
      rejectNoteRequired: '拒绝必须填写理由'
    }
  }
}
