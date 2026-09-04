// APEXONE-EXT: 双边市场——供给侧文案。
// 注意：vue-i18n 把 "@" 和 "|" 当特殊语法，字面 @ 要写成 {'@'}。
export default {
  supply: {
    // navLabel 单独一条而不是复用 title：侧边栏放不下一句话。
    // 也刻意不进 common.ts 的 nav 命名空间——那是上游文件，本模块整体是纯新增。
    navLabel: '共享订阅',
    // 分区标题，与菜单项标签分开：标题要说清这一整块是干什么的（赚钱），
    // 菜单项说的是具体去哪（共享订阅）。两者同名的话分区标题就成了噪音。
    navSection: '共享赚钱',
    title: '共享我的订阅',
    description: '把你的 AI 订阅接进来，闲置额度被别人用掉时你按比例分成。',

    // 控制台共享模式。这一组只在 /dashboard 上用，与 /supply 那一页的
    // wallet.* 分开写：同一个数在两处的**语境**不同——在专门的收益页可以叫
    // 「累计收益」，在一屏混着趋势图的概览上必须带上"这是共享侧的"这层信息。
    console: {
      modeLabel: '控制台',
      usageMode: '使用模式',
      sharingMode: '共享模式',
      // 与 supply.wallet.available 同名同义：同一笔钱在两个页面上必须叫同一个名字，
      // 否则用户会以为是两笔。
      available: '待提现收益',
      availableHint: '已过冻结期，可提现',
      // 刻意不叫「本月产出」：后端没有按月聚合的供给侧接口，标签说的必须是
      // 这个数真正的口径，否则它会被拿去算月收入。
      history: '累计收益',
      frozenHint: '另有 {amount} 在冻结期内',
      accounts: '在池账号数',
      schedulableHint: '{count} 个正在接单',
      actionsTitle: '快捷操作',
      connect: '接入订阅',
      connectDesc: '把闲置订阅挂进来开始赚分成',
      withdraw: '提现',
      withdrawDesc: '把待提现收益提到链上'
    },

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
      // 刻意**不叫**「可用余额」——那是消费侧充值余额的名字（common.availableBalance）。
      // 同时供给又消费的用户会在同一个应用里看到两个「可用余额」，而它们的规则
      // 完全不同：一笔是充值来的、只能花；这一笔是分成赚的、能提到链上。
      // 同名不同物是这套双边界面里最贵的一处误解，命名上必须先分开。
      available: '待提现收益',
      availableHint: '已过冻结期，可提现或抵扣消费',
      frozen: '冻结中',
      frozenHint: '过了冻结期会自动转入可用余额',
      history: '累计收益',
      spent: '已消费',
      refresh: '刷新'
    },

    // 新手引导。写给一个刚点进来、还不知道自己要干什么的人。
    //
    // 三条自我约束：
    //   1. 不出现任何金额、不出现"月入"「预计收益」——我们能承诺的只有分成比例
    //      和提现方式。写一个数字进去，它就会被当成承诺，而兑现不了的那天
    //      赔掉的是整个供给侧的信任。after3 就是这条约束的正面表达。
    //   2. 每一步都说清"为什么"和"要等多久"，尤其是观察期：新号不接单是设计，
    //      不解释的话它读起来像"我挂上去了但没反应"。
    //   3. 不与任何第三方服务作比较。
    guide: {
      title: '三步开始赚钱',
      subtitle: '你已经在为 Claude 订阅付费。把没用完的额度共享出来，别人用掉时你按比例分成。',
      step1: {
        title: '同意供给者协议',
        desc: '一次性动作。协议里写明了数据如何处理、以及你作为供给者的义务。'
      },
      step2: {
        title: '接入你的订阅',
        desc: '推荐用授权连接（OAuth）——请求经平台的加密网关直达 Anthropic，不会流经你的任何设备。也可以填一个兼容的 API 端点。'
      },
      step3: {
        title: '等待核验通过',
        desc: '新接入的账号会先进入观察期：平台定期探测，连续通过后才开始接单。这期间不会有收益，是正常的。'
      },
      afterTitle: '开始接单之后',
      after1: '每一笔用量按平台向用户收取的金额分成，比例见下方接入卡。',
      after2: '收益有冻结期，过后可提现到你的链上地址，零手续费。',
      // 这一句是底线，不能删也不能弱化。它回答的是"我到底能赚多少"——
      // 而那个问题如果不由我们如实回答，用户就会自己脑补一个数。
      after3: '实际能赚多少取决于平台的真实需求量。平台仍在早期，早期收益可能很有限——我们承诺的是分成比例，不是具体金额。',
      docsCta: '查看完整说明',
      docsHref: 'https://docs.apex1.us/earn/share-subscription/',
      // 控制台精简版上的按钮。那一页没有接入表单，唯一有意义的动作是把人送过去。
      cta: '去接入'
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
      // 比例由后端下发，不写死在文案里：它是运营随时可改的设置，
      // 写死的那个数会在运营改配置的那一刻变成一句谎话。
      shareRatio: '当前分成比例：平台向用户收取的每一笔费用，你拿其中的 {ratio}。',
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
      // 按钮上要写的是「可不可逆」，不是「什么时候」。「立即」只说了时间，
      // 而真正的差别是能不能反悔——且代价不对称：选错不可逆的那个要重走
      // 15–30 分钟观察期，选错另一个只是多等 10 分钟。有供给者来问这两个
      // 有什么区别，那就是标签没把差别传达出来的信号。
      pause: '下线',
      pausing: '正在下线…',
      pauseNow: '立即下线（不可撤销）',
      resume: '重新挂回',
      resuming: '正在挂回…',
      cancelPause: '取消下线',
      // 悬停提示。每条一句话，说后果而不是机制——把鼠标放上去的人想知道的是
      // 「我该点哪个」。
      // 开头就说「不会自己恢复」。有供给者来问「排空窗过了会不会自动回来」——
      // 不会，而旧文案把这件事留给他自己推断。这个推断错的方向是不对称的：
      // 以为会自动恢复的人，下线之后就走开了，然后白白少赚好几天。
      // 那 10 分钟是**可取消**窗口，不是「到点自动回来」的计时器。
      pauseTitle: '停止接新单。这个号会一直保持下线，直到你手动挂回——不会自己恢复。10 分钟内可以取消；过了之后再挂回要重走一遍观察期。已在传输中的请求正常跑完。',
      pauseNowTitle: '立刻停止接新单，且不可撤销——没有可取消的窗口。这个号会一直保持下线，直到你手动挂回，而挂回要重走一遍观察期（15–30 分钟）。已在传输中的请求正常跑完。',
      resumeTitle: '把号重新挂回池子。会先进入观察期，通过后才重新开始赚钱。',
      cancelPauseTitle: '让这个号继续接单——撤销待生效的下线，立刻回到之前的状态。',
      detachTitle: '停止接单，并把平台保存的这个号的凭证彻底删除。不可撤销。',
      pauseHint: '两个选项都会让这个号停下来，并且一直停着，直到你手动挂回——都不会自己恢复。「下线」给你 10 分钟的反悔窗口；「立即下线」跳过那个窗口且不可撤销。挂回都要重走一遍观察期。两者都停不掉已经在传输中的请求——那部分请求会正常跑完。',
      // 确认框是点下去之前最后一道提示，所以它要回答用户真正的疑问
      // ——「它会自己回来吗」——而不是只描述排空期。
      pauseConfirm: '这个号会立即停止接新单，并且一直保持下线，直到你自己把它挂回来——不会自动恢复。接下来 10 分钟内你还可以取消。已产生的收益不受影响。确定要下线吗？',
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
      drainUntil: '排空期至 {time}',

      // ---- 每日共享上限 ----
      dailyCap: '每日上限',
      dailyCapUnlimited: '不限',
      dailyCapEdit: '设置',
      dailyCapCancel: '取消',
      dailyCapSave: '保存',
      dailyCapSaving: '保存中…',
      dailyCapSaved: '已保存。次日 UTC 零点自动重置。',
      dailyCapCostLabel: '每日金额上限（美元，留空=不限）',
      dailyCapTokenLabel: '每日 token 上限（留空=不限）',
      dailyCapTokensUnit: 'token',
      // 这一句不能删：分母是官方牌价，不是他的收益（两者差一个倍率）。
      // 少了它，供给者会拿这个数去对自己的分成、得出"数字不对"的结论。
      dailyCapBasisHint: '按官方定价计，不是你的收益',
      dailyCapResetAt: '{time} 重置',
      // 触顶时账号在库里仍是"可调度"，所以这句话是供给者唯一能知道
      // "为什么现在没在赚钱"的地方。
      dailyCapReached: '今日额度已用完，零点后自动恢复接单',
      dailyCapHint: '两个上限先到先生效。用完当天不再接单，次日 UTC 零点自动恢复；已产生的收益不受影响。'
    },

    // 中转接入（M7）。trustNotice 是这一组唯一不能删的句子：平台会把消费者
    // 请求转发到供给者填的服务器上，这必须在他交出端点**之前**说清。
    relay: {
      title: '或：接入 API 中转',
      description: '填一个 Anthropic 兼容的 /v1/messages 端点和它的 API Key，平台把请求转发过去、按用量给你分成。',
      trustNotice: '注意：接入后，平台会把用户的请求内容转发到你填的服务器。你有义务妥善处理这些数据——这也写在供给者协议里。',
      baseUrlLabel: '端点地址（Base URL）',
      baseUrlHint: '必须是 https。不含路径后缀，平台会自动拼 /v1/messages。',
      apiKeyLabel: 'API Key',
      nameLabel: '给这个号起个名字（可选）',
      namePlaceholder: '留空则用端点域名',
      submit: '提交并验证',
      submitting: '正在验证端点…',
      probeHint: '提交时会向端点发送一条最小的真实请求（1 token，与观察期探测同一模型，默认 claude-sonnet-4-5）验证连通性与 Key。',
      submitted: '接入成功，已进入观察期。',
      failed: '接入失败',
      fieldsRequired: '端点地址与 API Key 都要填'
    },

    // 就地重新授权。凭证失效时把一份新 token 换进**同一个**号。
    //
    // 这一组里有两句是承重的，改之前请读理由：
    //   - hint：供给者此前被教育的修复方式是「解绑再重新接入」（换新 id、
    //     观察期重跑、每日上限丢失）。不明确说「这不是重新接入」，他不会相信。
    //   - mismatch：泛化成「授权失败」会把人直接推回解绑重挂，正是要消灭的动作。
    reauth: {
      badge: '需要重新授权',
      cta: '重新授权',
      starting: '正在生成授权链接…',
      title: '重新授权这个号',
      hint: '用同一个 Anthropic 账号重新走一遍授权。账号 id、每日上限和已产生的收益都保留，这不是重新接入。',
      sameAccountHint: '请确认你登录的是 {email}。',
      submit: '完成重新授权',
      submitting: '正在换回凭证…',
      cancel: '取消',
      success: '已重新授权，这个号恢复接单。',
      successPending: '已重新授权，将重新进入观察期。',
      failed: '重新授权失败',
      mismatch: '你授权的是另一个 Anthropic 账号。请用 {email} 再走一遍。',
      // 替代那串原始的上游报错：它可能带 token 片段、内部主机名或整个 JSON 响应体，
      // 而供给者需要的是「我该做什么」。非重新授权类的错误仍显示原文——
      // 那是他仅有的诊断信息。
      statusHint: '平台已经用不了这个号的凭证了。点右边的「重新授权」换一份新的。'
    },

    // account.status 的翻译。此前这一栏直接把 'error' 这种内部值显示给供给者。
    accountStatus: {
      active: '正常',
      error: '异常',
      disabled: '已被平台停用',
      unknown: '未知'
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
      // 只有一个渠道时不画下拉：让人"选择"一个没有选择的下拉是噪音。
      singleChannel: '{channel} · 自动打款到你绑定的地址',
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

      // 链上收款地址。这一组文案的重量与它守的东西成正比：链上转账不可逆，
      // 且没有任何下游能兜住一个填错的地址——交易会成功，浏览器上写着「已转账」，
      // 钱在一个谁也不认识的地址里。所以 hint 必须说清后果，而不是写「请核对」。
      wallet: {
        autoNotice: '{channel} 由系统自动打款到你绑定的链上地址，不需要手填收款账号。',
        empty: '你还没有绑定 {network} 收款地址。绑定之后才能用这个渠道提现。',
        label: '{network} 收款地址（{token}）',
        placeholder: '从钱包复制的 0x 开头地址',
        hint: '请从钱包里复制粘贴，不要手打、也不要凭记忆改动其中任何一位。链上转账不可逆：地址错了，钱会成功转到别人手里，平台无法追回。',
        bind: '绑定',
        binding: '绑定中…',
        rebind: '换绑',
        cancelEdit: '取消',
        rebindNotice: '换绑不影响已经提交的单子——那些单子记的是提交那一刻的地址。',
        unbind: '解绑',
        unbinding: '解绑中…',
        unbindConfirm: '确定解绑 {network} 上的收款地址吗？解绑后这个渠道就提不了现，已经提交的单子不受影响。',
        bindSuccess: '收款地址已绑定。',
        unbindSuccess: '收款地址已解绑。'
      },

      // 手续费。免手续费改版后新单不再收 gas（金库承担），
      // line 只用于历史单子（fee_amount > 0 的旧记录）的展示。
      fee: {
        line: '手续费 {fee} · 到账 {net}',
        auto: '自动打款 · {network}'
      },

      state: {
        pending: '待处理',
        // 链上单的两个中间态（M4）。failed 的措辞刻意不说"失败"——
        // 它的意思是"自动打款没走通、有人在跟进"，钱还在单子上；
        // 说"失败"供给者会立刻来问退款，而正确答案可能是核实后照常打款。
        processing: '打款中',
        failed: '打款异常（人工跟进中）',
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
      // 没有 Fable 额度：这个号免费或没订阅付费方案，接进来接不了单，后端已当场
      // 拒绝并清掉。措辞给出下一步（订阅→重试），不糊成一句'失败'。
      noFableQuota: '这个账号没有 Claude Fable 额度（可能是免费账户或未订阅付费方案），无法共享。请先订阅 Claude 付费方案，再重新接入。',
      pauseFailed: '下线失败',
      resumeFailed: '挂回失败',
      detachFailed: '解绑失败',
      acceptFailed: '同意协议失败',
      codeRequired: '请先填写授权码',
      withdrawalAmountInvalid: '请填写一个大于 0 的提现金额',
      withdrawalChannelRequired: '请选择收款渠道',
      withdrawalFailed: '提交提现申请失败',
      withdrawalCancelFailed: '撤回提现申请失败',
      payoutAddressRequired: '请填写收款地址',
      // 与 withdrawalAccountRequired 分开：链上渠道的收款账号不是「填」出来的，
      // 是绑定出来的。让他去表单里找一个不存在的输入框，是一次白跑。
      payoutWalletRequired: '请先绑定这个渠道的链上收款地址',
      payoutBindFailed: '绑定收款地址失败',
      payoutUnbindFailed: '解绑收款地址失败',
      dailyCapInvalid: '上限要填一个不小于 0 的数字，留空表示不限',
      dailyCapFailed: '保存每日上限失败'
    }
  },

  supplyAdmin: {
    navLabel: '双边市场',
    title: '双边市场',
    description: '配置供给者分成、供给池路由、观察期、协议与提现。每组分开保存。',

    // 定价与供给健康度（只读）。放在几张配置卡之前是有意的：下面每一个参数配得对不对，
    // 只能由这张卡上「从钱反推出来」的读数回答，而不是由输入框里写着什么回答。
    health: {
      title: '定价与供给健康度',
      description: '倍率、分成、兜底这几个参数配得对不对，只能从跑出来的钱反推。这张卡全部只读。',
      window: '统计窗口',
      windowDays: '最近 {days} 天',
      loadFailed: '加载健康度读数失败',
      retry: '重新加载',
      listValue: '近 {days} 天流水（牌价等值）',
      listValueHint: '平台营收（消费者实付）{revenue}',
      grossMargin: '平台毛利',
      grossMarginHint: '营收 {revenue} − 供给者分成 {payout}。不含兜底订阅费与服务器等固定成本，判断能不能覆盖它们要拿这个数去比月度固定支出。',
      medianOutput: '供给账号折月产出中位数',
      medianOutputHint: '{suppliers} 位供给者 · {accounts} 个号有产出。用中位数不用平均值：一两个重度账号会把平均值抬起来，把「多数供给者其实赚不到钱」盖住，而那正是供给流失的前兆。',
      overflowShare: '兜底承接比例',
      overflowShareHint: '平台自有账号承接了 {value} 牌价等值。这个数偏高只说明共享供给不够、该去拉供给者，不代表兜底账号不够用。',

      selfCheck: {
        title: '配置自检',
        multiplier: '倍率',
        share: '分成',
        effective: '实际生效 {value}',
        configured: '配置 {value}',
        aligned: '一致',
        drift: '相差 {drift}',
        noPool: '未配供给池分组，无从对照',
        noShare: '分成配成 0，无从对照',
        // 这段话的排序是按成因的常见程度来的：调过价之后这个高亮会持续亮满一个窗口，
        // 如果只说「密钥绑错分组」，运营每调一次价就会去查一次并不存在的事故。
        mismatch:
          '实际值与配置值不一致。常见成因有两个：① 统计窗口跨越了一次参数调整，窗口内新旧计费混在一起——调价之后属正常，会随时间收敛；② 有消费者的密钥绑在别的分组上，那个分组用的是它自己的倍率。切到 7 天窗口看是否收敛，可以区分这两者。'
      },

      exhausted: {
        title: '今天有 {count} 次请求撞上「供给池和兜底池同时是空的」。',
        body: '这些请求的消费者当场拿到的是「无可用账号」。这是唯一该据以增加兜底账号的信号——兜底承接比例高不算，那只说明兜底在被用。'
      },

      accounts: {
        title: '供给账号产出榜',
        description: '按窗口内产出降序。折月产出低于 $1500 的号落在定价方案的最低一档：那一档的动作是把倍率提上去，否则供给者留不住。',
        name: '账号',
        monthlyOutput: '折月产出',
        earned: '供给者收益',
        requests: '请求数',
        low: '低于预期',
        empty: '窗口内没有任何供给账号产生产出。'
      },

      empty: {
        title: '这个窗口内没有流水。',
        body: '没有用量就没有可反推的倍率与分成，四格与自检都不会显示。换一个更长的窗口，或等第一批请求跑过之后再看。'
      }
    },

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
      advancedTucked:
        '工程参数（探测间隔、达标次数、排空窗、探测模型）已从面板收起：默认值即推荐值，settings API 仍可手工调整。',
      clampNotice: '这一组参数越界时后端会夹回区间并保存（不会报错），保存后表单显示的是库里真正存下的值。',
      save: '保存观察期参数',
      saved: '观察期参数已保存'
    },

    // APEXONE-EXT: 接入上限。两道闸的文案分开写，因为它们挡的不是同一件事，
    // 而运营需要在配之前就知道第二道的误伤面有多大。
    onboarding: {
      relayEnabled: '开放「URL + API Key」中转接入',
      relayEnabledHint: '开启后，供给者可以自助提交 Anthropic 兼容中转端点，与订阅号走同一套观察期与结算。',
      relayTrustWarning: '开启即接受：消费者的请求内容会被转发到供给者控制的服务器。请确认供给者协议已写明数据处理义务。',
      title: '接入上限',
      description: '一个人、一个出口网络最多能往平台挂几个供给账号。两道闸都是 0 = 不限。',
      maxPerUser: '每人最多账号数',
      maxPerUserHint: '数的是当下未解绑的号，解绑一个就腾出一个位置。这道闸只是礼貌性护栏——再注册一个账号就能绕过它。填 0 = 不限。上限 {max}。',
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
      noNotifyNotice: '提现开着，但打款异常时没有任何人会收到告警——链上打款失败的单子会带着扣掉的钱等人工裁决，而后台不会自己弹出来。请在下面填上收件人。',
      enabled: '开启提现',
      enabledHint: '关闭后余额照常累积，只是不能申请提现。已提交的单子不受影响。',
      minAmount: '起提金额',
      minAmountHint: '低于这个数的申请会被**拒绝**，不会被夹到起提额。上限 {max}。',
      notifyEmails: '打款异常告警收件人',
      // vue-i18n 把裸 @ 当 linked-message 语法，必须用 {'@'} 转义，否则整个
      // locale 编译失败（localesMessageCompile.spec.ts 会拦下来）。
      notifyEmailsPlaceholder: "finance{'@'}example.com\nops{'@'}example.com",
      notifyEmailsHint: '一行一个，最多 {max} 个、每个不超过 {len} 字。留空 = 链上打款失败时没有任何人被叫来，那笔钱会卡在一张没人知道的单子上。与配额告警的收件人是两份配置：收打款异常的是财务，收告警的是运维。格式填错会**保存失败**并告诉你是哪一个，不会被悄悄丢掉。',
      notice: '给供给者的说明',
      noticePlaceholder: '到账时效、手续费、需要提供的信息……',
      noticeHint: '显示在供给者的提现表单上，纯文本，最多 {max} 字。',
      rejectNotice: '越界的参数会被直接拒绝而不是夹回：起提额被悄悄夹到上限，结果是所有人都提不了钱，而面板上看不出任何异常。',
      channelsRetired: '收款渠道已改由下方「链上金库」的配置派生：金库能结算什么币，供给者就能提什么。旧的渠道白名单不再被读取。',
      save: '保存提现参数',
      saved: '提现参数已保存'
    },

    // 链上金库（M6）。私钥只进不出：这张卡上任何地方都不会回显私钥，
    // 保存后能看到的只有从它推导出的金库地址。
    payoutChain: {
      title: '链上金库',
      description: '提现由这里配置的金库自动上链打款。保存即生效，无需重启。',
      statusLive: '打款客户端已装配（LIVE），链 ID 已与节点核对。',
      statusUnverified: '客户端已装配，但链 ID 没核上或有告警——看下面的错误信息。',
      statusOff: '链上打款未启用。供给者的提现入口整体不可用。',
      enabled: '启用链上打款',
      enabledHint: '开启并配好金库后，提现单由 worker 自动上链结算。关闭时供给者无法发起提现。',
      rpcUrl: '节点 RPC 地址',
      chainId: '链 ID',
      chainIdHint: 'BSC 主网 56，测试网 97。保存时会向节点核对。',
      tokenAddress: '稳定币合约地址',
      tokenAddressHint: '换这个地址等于换一种币：老单子上钉的是建单那一刻的合约，worker 会拒绝为它们发新配置的币（走 failed → 人工裁决）。',
      disperseAddress: '批量合约地址（可选）',
      disperseHint: '留空则逐笔转账。配上后同一轮的同币种单子合成一笔 disperseToken，省 gas。',
      signerKey: '金库私钥',
      signerKeyPlaceholder: '0x 开头的 64 位十六进制私钥',
      signerKeyKeep: '已配置。留空保存 = 保持不变',
      signerKeyHint: '私钥加密落库（AES-256-GCM），保存后不再回显；页面上能看到的只有推导出的金库地址。',
      treasury: '金库地址：{address}',
      verify: '测试连接',
      verifying: '正在核对…',
      save: '保存并生效',
      saved: '金库配置已保存并生效'
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

    // 失效事件。文案上刻意区分「窗口内发生过」与「现在还坏着」两种口径——
    // 那是这一段唯一容易被读错的地方。
    incidents: {
      title: '供给号失效',
      description: '一次失效 = 一行。上面那张账号表答的是「此刻怎么样」，这里答的是「这段时间发生过什么」——号恢复之后，前者就再也说不出他这个月坏过几次。',
      openOnly: '只看还没恢复的',
      userFilter: '只看 #{id}',
      opened: '新发生',
      resolved: '已恢复',
      open: '当前还坏着',
      openNoWindow: '不受窗口限制：坏了三个月的号也算在内',
      suppliers: '被波及的供给者',
      inWindow: '最近 {days} 天',
      ofAccounts: '全站共 {count} 个供给号',
      supplier: '供给者',
      accountsCol: '在册号数',
      incidentsCol: '窗口内失效次数',
      openCol: '当前未恢复',
      rateCol: '失效率',
      lastDetectedAt: '最近一次',
      detectedAt: '发现时间',
      account: '账号',
      reason: '上游状态',
      state: '状态',
      closed: '已恢复',
      stillOpen: '还坏着',
      notNotified: '尚未通知到供给者'
    },

    export: {
      button: '导出近 {days} 天 CSV',
      running: '导出中…',
      done: '导出完成（{note}）',
      truncated: '导出已截断：{note}。请收窄时间窗后重新导出。',
      incomplete: '这份文件不完整，不能用于打款：服务端在写入过程中出错，文件已存为 {name}。请重新导出。'
    },

    error: {
      overviewFailed: '加载看板失败',
      exportFailed: '导出失败',
      rosterFailed: '加载名册失败',
      accountsFailed: '加载账号失败',
      ledgerFailed: '加载流水失败',
      withdrawalsFailed: '加载提现单失败',
      incidentSummaryFailed: '加载失效报表失败',
      incidentsFailed: '加载失效事件失败',
      withdrawalResolveFailed: '处理提现单失败',
      rejectNoteRequired: '拒绝必须填写理由'
    }
  }
}
