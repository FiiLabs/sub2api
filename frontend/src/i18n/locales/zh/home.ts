// 旧版(tee-control)营销首页/页头/页脚的 home.* 文案,整体覆盖 upstream landing 里的 home 块
//
// ## 硬规则：价格对比只与「官方 API」比
//
// 本文件（以及整个仓库、所有会进 GitHub 的文档）**不得出现任何第三方服务的名字**，
// 也不得出现"比某某网关便宜""市面上最低"这类指向具体对手的暗示。对比列里那个
// 「不透明网关」是**类别**不是某一家，可以留。
//
// 这不是文案偏好：点名对手会把我们的定价决策暴露成对某一家的应激反应，
// 也给了对方一个现成的比较框架。竞品分析放在仓库之外的运营文档里，不进代码。
export default {
  home: {
    viewOnGithub: '在 GitHub 上查看',
    viewDocs: '查看文档',
    docs: '文档',
    switchToLight: '切换到浅色模式',
    switchToDark: '切换到深色模式',
    dashboard: '控制台',
    login: '登录',
    getStarted: '立即开始',
    goToDashboard: '进入控制台',
    nav: {
      menu: '菜单',
      architecture: '架构',
      features: '特性',
      price: '价格',
      proof: '证明',
      document: '文档'
    },
    landing: {
      hero: {
        // 不写"今日/本周"这类带时效的词:首页文案改一次要走一次发版,
        // 而"今日上线"过了当天就是假的。
        badge: 'Claude Fable 5 已上线',
        title: '隐私不靠承诺，靠验证。TEE 加密路由',
        subtitle:
          '通过 TEE 加密网关调用 Claude 等前沿模型——数据全程私密，每次调用可验证，价格低至官方 API 的 2.2 折。',
        verifyPrivacy: '验证隐私',
        // 消费侧入口卡的标题。与下面的供给卡是一对,措辞必须同构（都是「我要/我有」
        // 开头的第一人称短句）：两张卡回答的是同一个问题的两个答案,句式不一致
        // 会让人以为它们不是并列关系。
        useAI: '我要用 AI',
        // 卡片副文案是 subtitle 的精简版：卡片里放不下一整句带三个从句的话,
        // 但价格和隐私这两个点必须都在——少一个就变成另一个产品的介绍。
        useAIDesc: '通过 TEE 加密网关调用 Claude 等前沿模型，价格低至官方 API 的 2.2 折，数据全程私密且每次调用可验证。',
        // 供给侧入口:锚点到本页的 #supply,不直接跳 /supply——那条路要登录,
        // 未登录用户点过去只会看到登录页,不知道自己错过了什么。
        shareSubscription: '我有闲置订阅',
        // 三件事按供给者真正会问的顺序说：能赚吗、怎么算、钱怎么拿到手。
        shareSubscriptionDesc: '把闲置额度变成收入：别人用掉你的订阅额度时按用量分成，收益可提现到链上。',
        shareSubscriptionCta: '了解如何赚取',
        stats: {
          // 与数值卡片拼成「22% / 官方 API 价格」。标签里不要再写一遍「2.2 折」——
          // 那会让同一张卡上出现两种单位，读者得先换算才知道说的是一件事。
          discount: '官方 API 价格',
          attested: 'TEE 隐私保护',
          claude: '已上线',
          hermes: '客户端支持'
        }
      },
      routing: {
        eyebrow: '// 机密路由架构',
        title: '我们不卖你的数据——因为我们根本拿不到。',
        subtitle:
          '不同于不透明网关，ApexOne 的每一次请求都经过可验证的 TEE 路由，明文从不经过我们的手。',
        // 三种供货通道里有一种(供给者自填 URL 的 API 中转)不在 TEE 密封内,
        // 所以这句必须带通道限定,不能写成全称判断。
        caption:
          '平台自营与共享订阅通道的明文只在已认证的 TEE 内解密——ApexOne 运营方、日志和基础设施都读不到；供给者自填的 API 中转通道会把请求转发到该供给者的服务端点，不在这层密封之内。'
      },
      personal: {
        eyebrow: '// ApexOne',
        title: '价格 2.2 折，窥探为零。',
        subtitle: '价格、隐私、可靠性，一张表看明白。',
        compare: {
          note: '基于官方 API 计费价格，ApexOne 按其 22% 计费。',
          cols: {
            opaque: '不透明网关',
            official: '官方 API',
            apexone: 'ApexOne'
          },
          rows: {
            attestation: {
              label: '可验证隐私（远程认证）',
              opaque: '❌ 无',
              official: '❌ 无',
              apexone: '✅ 浏览器中自证'
            },
            dataAccess: {
              label: '提示词对运营方密封',
              opaque: '❌ 可读取，可能转卖',
              official: '⚠️ 仅靠政策承诺',
              apexone: '✅ TEE 密封，可证明'
            },
            fable: {
              label: '真实模型，完整精度',
              opaque: '⚠️ 无法验证',
              official: '✅ 是',
              apexone: '✅ 已认证，现已可用'
            },
            failover: {
              // 不承诺具体可用性数字:SLA 一旦写进首页就是对外承诺,
              // 而我们并没有对应的赔付条款。只讲我们确实做了的机制。
              label: '自动故障切换',
              opaque: '❌ 无',
              official: '❌ 无',
              apexone: '✅ 多路由自动切换'
            },
            price: {
              label: '价格',
              opaque: '便宜，但说不清为什么',
              official: '官方原价',
              apexone: '官方价格 2.2 折'
            }
          }
        },
        uptime: {
          title: '不间断路由',
          desc: '多路由架构，路由一旦劣化立即切换流量。',
          headline: '多路由自动切换',
          window: '可用性设计目标：劣化即切换',
          points: {
            failover: '路由劣化时自动切换',
            monitoring: '实时路由健康监测'
          }
        },
        privacy: {
          title: 'TEE 密封路由',
          desc: '不同供货通道的数据流向不一样，这里按通道说清楚——TEE 边界本身，你可以在浏览器里亲自验证。',
          // 按通道分开写,不是措辞讲究而是准确性:自营和共享订阅两条通道请求确实
          // 不出 TEE,但 API 中转通道会走到供给者自己的服务器上。合成一句"我们拿不到
          // 你的数据"对第三条就是假的,所以三条各写各的流向。
          points: {
            owned: '平台自营账号：请求在 Intel TDX 机密虚拟机内路由，运营方与日志都读不到明文',
            shared:
              '共享订阅通道：供给者只提供订阅额度，请求内容经 TEE 直达 Anthropic，不经过供给者的任何设备',
            relay: 'API 中转通道：请求会转发到供给者提供的服务端点，这一通道不在 TEE 密封范围内'
          }
        }
      },
      pricing: {
        eyebrow: '// 价格',
        title: '官方价格 2.2 折，透明公开。',
        subtitle: '按 token 计费，价格为官方 API 的 22%——没有订阅，没有最低消费。',
        personal: {
          name: 'ApexOne',
          tagline: '按量付费',
          priceLine: '按官方 API 计费价格的 22% 逐 token 计费。',
          desc: '面向需要 Claude 接入、可验证私密路由和透明账单的开发者。',
          cta: '立即开始 →',
          features: {
            f1: 'Claude Fable 5 现已可用；GPT 和 Gemini 即将上线',
            f2: '官方 API 计费价格 2.2 折',
            f3: 'TEE 认证网关 + 远程认证',
            f4: '控制面只接触元数据——提示词从不落日志',
            f5: '自动故障切换 & 实时路由监测',
            f6: '兼容 OpenAI API——Hermes 等客户端开箱即用'
          }
        }
      },
      // 供给侧区块。刻意只承诺我们控制得了的东西(分成比例、提现方式),
      // 用量和收入取决于真实需求,写成数字就会变成信任事故。
      supply: {
        eyebrow: '// 共享订阅',
        title: '把闲置的订阅额度，变成收入。',
        subtitle: '你已经在为 Claude 订阅付费。别人用掉你没用完的那部分时，你按比例分成。',
        howItWorks: {
          title: '怎么运作',
          s1: {
            title: '接入你的订阅',
            desc: '授权连接，或提供一个兼容的 API 端点。随时可以下线或解绑。'
          },
          s2: {
            title: '闲置额度被使用',
            desc: '平台把请求调度到你的账号上，只用你没用完的额度。'
          },
          // 分成基数必须写明是「平台向用户收取的金额」而不是「官方计价」。
          // 页面另一处写着"按官方价的 22% 计费"，两个数字摆在同一页上，
          // 含糊的基数会被直接算成"收 22 付 80"——那是一句说不通的话，
          // 而认真算过账的供给者恰恰是我们最想留住的那批人。
          s3: {
            title: '按用量分成',
            desc: '平台向用户收取的每一笔费用，你拿其中的 80%。'
          }
        },
        earnings: {
          title: '收益怎么算',
          formula: '你的收益 = 用户为这些用量付的钱 × 分成比例',
          ratio: '当前分成比例：80%',
          // 全区块最重要的一句:平台还在早期,供给者按"月入多少"的预期进来
          // 一定会失望,这句必须留在页面上,不能被后来的转化优化砍掉。
          disclaimer:
            '平台承诺的是分成比例，不是具体收入金额。实际能赚多少取决于平台的真实需求量——而平台仍在早期，需求还在积累中，早期收益可能很有限。任何收益测算都只是估算，不构成承诺。'
        },
        payout: {
          title: '收益怎么拿',
          chain: '链上 USDT 直接打到你的钱包地址',
          fee: '提现零手续费，网络 gas 由平台承担',
          freeze: '收益有 72 小时冻结期，之后可随时提取'
        },
        privacy: {
          title: '你不经手任何用户数据',
          desc: '共享订阅通道下，用户的请求内容经平台的 TEE 网关直达 Anthropic，不会流经你的任何设备。你提供的是额度，不是服务器。'
        },
        cta: '了解如何共享订阅'
      },
      video: {
        eyebrow: '// 为什么是 ApexOne',
        title: '价格 2.2 折，隐私不打折',
        subtitle: '便宜和隐私，不用二选一。'
      },
      cta: {
        title: '用得起，也信得过的 AI。',
        description: 'Claude Fable 5，官方 2.2 折，TEE 全程密封。',
        primary: '立即开始 →',
        secondary: '验证隐私 →',
        supply: '共享订阅赚钱 →',
        pills: {
          noTraining: '🚫 不用你的数据训练',
          audit: '📋 仅元数据审计日志',
          encrypted: '🔒 传输全程加密'
        }
      }
    },
    footer: {
      contactUs: '联系我们',
      stayConnected: '保持联系',
      allRightsReserved: '保留所有权利。',
      trademarkNotice: '*Claude 是 Anthropic, PBC 的商标。ApexOne 是独立服务，与 Anthropic 无关联。'
    }
  },
}
