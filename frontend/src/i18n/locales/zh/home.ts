// 旧版(tee-control)营销首页/页头/页脚的 home.* 文案,整体覆盖 upstream landing 里的 home 块
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
    // 新增：面向用户的价值主张
    heroSubtitle: '一个密钥，畅用多个 AI 模型',
    heroDescription: '无需管理多个订阅账号，一站式接入 Claude、GPT、Gemini 等主流 AI 服务',
    tags: {
      subscriptionToApi: '订阅转 API',
      stickySession: '会话保持',
      realtimeBilling: '按量计费'
    },
    // 用户痛点区块
    painPoints: {
      title: '你是否也遇到这些问题？',
      items: {
        expensive: {
          title: '订阅费用高',
          desc: '每个 AI 服务都要单独订阅，每月支出越来越多'
        },
        complex: {
          title: '多账号难管理',
          desc: '不同平台的账号、密钥分散各处，管理起来很麻烦'
        },
        unstable: {
          title: '服务不稳定',
          desc: '单一账号容易触发限制，影响正常使用'
        },
        noControl: {
          title: '用量无法控制',
          desc: '不知道钱花在哪了，也无法限制团队成员的使用'
        }
      }
    },
    // 解决方案区块
    solutions: {
      title: '我们帮你解决',
      subtitle: '简单三步，开始省心使用 AI'
    },
    features: {
      unifiedGateway: '一键接入',
      unifiedGatewayDesc: '获取一个 API 密钥，即可调用所有已接入的 AI 模型，无需分别申请。',
      multiAccount: '稳定可靠',
      multiAccountDesc: '智能调度多个上游账号，自动切换和负载均衡，告别频繁报错。',
      balanceQuota: '用多少付多少',
      balanceQuotaDesc: '按实际使用量计费，支持设置配额上限，团队用量一目了然。'
    },
    // 优势对比
    comparison: {
      title: '为什么选择我们？',
      headers: {
        feature: '对比项',
        official: '官方订阅',
        us: '本平台'
      },
      items: {
        pricing: {
          feature: '付费方式',
          official: '固定月费，用不完也付',
          us: '按量付费，用多少付多少'
        },
        models: {
          feature: '模型选择',
          official: '单一服务商',
          us: '多模型随意切换'
        },
        management: {
          feature: '账号管理',
          official: '每个服务单独管理',
          us: '统一密钥，一站管理'
        },
        stability: {
          feature: '服务稳定性',
          official: '单账号易触发限制',
          us: '多账号池，自动切换'
        },
        control: {
          feature: '用量控制',
          official: '无法限制',
          us: '可设配额、查明细'
        }
      }
    },
    providers: {
      title: '已支持的 AI 模型',
      description: '一个 API，多种选择',
      supported: '已支持',
      soon: '即将推出',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: '更多'
    },
    // CTA 区块
    cta: {
      title: '准备好开始了吗？',
      description: '注册即可获得免费试用额度，体验一站式 AI 服务',
      button: '免费注册'
    },
    landing: {
      hero: {
        badge: 'Claude Fable 5 今日上线',
        title: '隐私不靠承诺，靠验证。TEE 加密路由',
        subtitle:
          '通过 TEE 加密网关调用 Claude 等前沿模型——数据全程私密，每次调用可验证，价格仅为官方 API 的一半。',
        verifyPrivacy: '验证隐私',
        stats: {
          discount: '官方 API 价格的一半',
          attested: 'TEE 隐私保护',
          claude: '今日上线',
          hermes: '客户端支持'
        }
      },
      routing: {
        eyebrow: '// 机密路由架构',
        title: '我们不卖你的数据——因为我们根本拿不到。',
        subtitle:
          '不同于不透明网关，ApexOne 的每一次请求都经过可验证的 TEE 路由，明文从不经过我们的手。',
        caption: '明文只在已认证的 TEE 内解密——ApexOne 运营方、日志和基础设施都读不到。'
      },
      personal: {
        eyebrow: '// ApexOne',
        title: '价格砍半，窥探为零。',
        subtitle: '价格、隐私、可靠性，一张表看明白。',
        compare: {
          note: '基于官方 API 计费价格，ApexOne 按其 50% 计费。',
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
              apexone: '✅ 已认证，今日可用'
            },
            failover: {
              label: '自动故障切换',
              opaque: '❌ 无',
              official: '❌ 无',
              apexone: '✅ 99.99% 可用性'
            },
            price: {
              label: '价格',
              opaque: '便宜，但说不清为什么',
              official: '官方原价',
              apexone: '官方价格五折'
            }
          }
        },
        uptime: {
          title: '不间断路由',
          desc: '多路由架构，路由一旦劣化立即切换流量。',
          window: '可用性设计目标',
          points: {
            failover: '路由劣化时自动切换',
            monitoring: '实时路由健康监测'
          }
        },
        privacy: {
          title: 'TEE 密封路由',
          desc: '提示词只在 Intel TDX 机密虚拟机内解密——这条边界你可以在浏览器里亲自验证。',
          points: {
            tdx: '仅在 Intel TDX 内、交给服务方之前解密',
            noRead: 'ApexOne 运营方和日志读不到提示词',
            attestation: '远程认证可验证网关边界'
          }
        }
      },
      pricing: {
        eyebrow: '// 价格',
        title: '官方价格一半，透明公开。',
        subtitle: '按 token 计费，价格为官方 API 的 50%——没有订阅，没有最低消费。',
        personal: {
          name: 'ApexOne',
          tagline: '按量付费',
          priceLine: '按官方 API 计费价格的 50% 逐 token 计费。',
          desc: '面向需要 Claude 接入、可验证私密路由和透明账单的开发者。',
          cta: '立即开始 →',
          features: {
            f1: 'Claude Fable 5 今日可用；GPT 和 Gemini 即将上线',
            f2: '官方 API 计费价格五折',
            f3: 'TEE 认证网关 + 远程认证',
            f4: '控制面只接触元数据——提示词从不落日志',
            f5: '自动故障切换 & 实时路由监测',
            f6: '兼容 OpenAI API——Hermes 等客户端开箱即用'
          }
        }
      },
      video: {
        eyebrow: '// 为什么是 ApexOne',
        title: '价格减半，隐私不打折',
        subtitle: '便宜和隐私，不用二选一。'
      },
      cta: {
        title: '用得起，也信得过的 AI。',
        description: 'Claude Fable 5，官方半价，TEE 全程密封。',
        primary: '立即开始 →',
        secondary: '验证隐私 →',
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
