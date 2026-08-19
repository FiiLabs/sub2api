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
      unknown: '其他'
    },

    error: {
      loadFailed: '加载失败',
      startFailed: '发起授权失败',
      completeFailed: '接入失败',
      pauseFailed: '下线失败',
      resumeFailed: '挂回失败',
      codeRequired: '请先填写授权码'
    }
  },

  supplyAdmin: {
    navLabel: '双边市场',
    title: '双边市场',
    description: '配置供给者分成与供给池路由。两组配置分开保存。',

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

    error: {
      loadFailed: '加载配置失败',
      saveFailed: '保存失败'
    }
  }
}
