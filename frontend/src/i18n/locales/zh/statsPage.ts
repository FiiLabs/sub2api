// /stats 公开数据看板页文案。数字为真实聚合 + 运营配的基数偏移；增长曲线为展示性质的合成示意。
export default {
  statsPage: {
    eyebrow: '// 平台数据',
    title: 'ApexOne 用数据说话',
    subtitle: '可验证的 AI 网络，正在增长。',
    kpi: {
      sharedAccounts: '共享账号',
      activeUsers: '活跃用户',
      totalRequests: '累计请求',
      contributorEarnings: '已付贡献者 (USDT)'
    },
    growth: {
      title: '请求增长（近 30 天）',
      requests: '请求',
      users: '活跃用户'
    },
    supply: {
      title: '供给分布',
      subtitle: '按平台的共享账号占比',
      claude: 'Claude',
      openai: 'OpenAI',
      other: '其它'
    },
    empty: {
      title: '平台数据尚未公开',
      desc: '运营尚未开启公开数据展示，稍后再来看看。'
    },
    cta: {
      use: '开始使用 →',
      share: '共享订阅赚钱 →',
      home: '返回首页'
    }
  }
}
