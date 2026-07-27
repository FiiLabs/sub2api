// /proof 页文案。注意:vue-i18n 会把 "@" 和 "|" 当特殊语法 —— digest、邮箱、
// 命令一律不进文案,由组件直接渲染。
export default {
  proof: {
    title: '验证这个服务',
    subtitle:
      '本页在你的浏览器里证明:该服务运行在真实的 Intel TDX 机密计算硬件中,且运行的是未经篡改的开源代码。下面的密码学校验全部在本地进行,信任根是 Intel 与公开源码仓库,而不是服务器自己。',
    overall: {
      running: '正在验证实时证明…',
      pass: '全部校验通过 —— 真实硬件,运行已发布的开源构建。',
      fail: '验证失败 —— 有校验项不匹配,详见下方。',
      error: '无法完成验证。',
    },
    actions: {
      rerun: '重新验证',
      download: '下载证据 (JSON)',
    },
    hardware: {
      title: '真实硬件,当下新鲜',
      desc: '浏览器生成随机 nonce 并绑定进实时 Intel TDX quote,quote 签名链由 Intel 官方 collateral 验证 —— 证明是真实机密硬件在"现在"应答,而非重放。',
      quoteGenuine: 'TDX quote 签名链(Intel DCAP)',
      tcbStatus: '硬件 TCB 状态',
      nonceBinding: '新鲜性:quote 绑定本次访问的 nonce',
    },
    measure: {
      title: '运行的正是已发布的代码',
      desc: '重放硬件度量事件日志,并与发布的参考值比对:compose 文件(以 digest 锁定每个容器镜像)、操作系统镜像、应用身份。',
      rtmrReplay: '事件日志真实(RTMR3 重放与寄存器一致)',
      appId: '应用身份(app-id)',
      composeHash: 'compose 度量(事件日志 + mr_config_id 寄存器双路)',
      osImageHash: '操作系统镜像度量',
    },
    source: {
      title: '开源可查,构建可证',
      desc: '被度量的 compose 所锁定的镜像,由公开 CI 从公开仓库构建,附 keyless cosign 签名与 SLSA 构建溯源。任何人都可用下方命令复核 digest 与 commit 的绑定。',
      repo: '源码仓库',
      commit: '锁定的 commit',
      images: '锁定的镜像',
      verifyCmd: '第三方复核命令',
    },
    disclosure: {
      title: '本页不证明什么(诚实边界)',
      body1: '传输:你的连接经 TLS 终止在 enclave 前的 Phala 网关;本页未在其上叠加端到端加密。',
      body2: '上游:服务转发给模型提供商的请求,对提供商本身是明文可见的,受其条款约束。',
    },
    reference: {
      source: '参考值来源',
      repo: '已发布参考值(从公开仓库实时拉取,变更历史可在 git 审计)',
      bakedIn: '内置回退值(仓库副本拉取失败 —— 数值可能过期)',
    },
    auditors: {
      title: '审计视角:逐项校验与原始证据',
      checks: '逐项校验',
      raw: 'attestor 原始响应',
      refValues: '已发布参考值',
      hint: '每行是一次独立比对;信息性条目会明确标注,绝不伪装成绿色通过。',
      endpoint: 'attestor 端点',
      pccs: 'Intel collateral(PCCS)',
    },
    errors: {
      fetchFailed: '无法访问 attestor 端点',
    },
  },
}
