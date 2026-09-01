// 旧版(tee-control)营销首页/页头/页脚的 home.* 文案,整体覆盖 upstream landing 里的 home 块
//
// ## 硬规则：价格对比只与「官方 API」比
//
// 本文件（以及整个仓库、所有会进 GitHub 的文档）**不得出现任何第三方服务的名字**，
// 也不得出现"比某某网关便宜""市面上最低"这类指向具体对手的暗示。对比列里那个
// 「不透明网关 / Opaque Gateway」是**类别**不是某一家，可以留。
//
// 这不是文案偏好：点名对手会把我们的定价决策暴露成对某一家的应激反应，
// 也给了对方一个现成的比较框架。竞品分析放在仓库之外的运营文档里，不进代码。
export default {
  home: {
    viewOnGithub: 'View on GitHub',
    viewDocs: 'View Documentation',
    docs: 'Docs',
    switchToLight: 'Switch to Light Mode',
    switchToDark: 'Switch to Dark Mode',
    dashboard: 'Dashboard',
    login: 'Login',
    getStarted: 'Get Started',
    goToDashboard: 'Go to Dashboard',
    nav: {
      menu: 'Menu',
      architecture: 'Architecture',
      features: 'Features',
      price: 'Price',
      proof: 'Proof',
      document: 'Docs'
    },
    landing: {
      hero: {
        // 不写"今日/本周"这类带时效的词:首页文案改一次要走一次发版,
        // 而"今日上线"过了当天就是假的。
        badge: 'Claude Fable 5 Is Live',
        title: 'Data privacy you can verify. Powered by TEE',
        subtitle:
          'Access frontier models like Claude through a TEE-protected gateway — your data stays private, every call is verifiable, all at 14% of official API pricing.',
        verifyPrivacy: 'Verify Privacy',
        // 消费侧入口卡的标题。与下面的供给卡是一对,措辞必须同构（都是「我要/我有」
        // 开头的第一人称短句）：两张卡回答的是同一个问题的两个答案,句式不一致
        // 会让人以为它们不是并列关系。
        useAI: 'I want to use AI',
        // 卡片副文案是 subtitle 的精简版：卡片里放不下一整句带三个从句的话,
        // 但价格和隐私这两个点必须都在——少一个就变成另一个产品的介绍。
        useAIDesc:
          'Call frontier models like Claude through a TEE-protected gateway at 14% of official API pricing — your data stays private and every call is verifiable.',
        // 供给侧入口:锚点到本页的 #supply,不直接跳 /supply——那条路要登录,
        // 未登录用户点过去只会看到登录页,不知道自己错过了什么。
        shareSubscription: 'I have spare quota',
        // 三件事按供给者真正会问的顺序说：能赚吗、怎么算、钱怎么拿到手。
        shareSubscriptionDesc:
          'Turn idle subscription quota into income: earn a revenue share whenever someone uses it, and withdraw your earnings on-chain.',
        shareSubscriptionCta: 'See how earning works',
        stats: {
          discount: 'Of Official API Price',
          attested: 'TEE Privacy Protection',
          claude: 'Live Now',
          hermes: 'Client Support'
        }
      },
      routing: {
        eyebrow: '// Confidential Routing Architecture',
        title: "We can't sell your data. We can't even read it.",
        subtitle:
          'Unlike opaque gateways, ApexOne routes every request through a verifiable TEE — plaintext never touches our hands.',
        // 三种供货通道里有一种(供给者自填 URL 的 API 中转)不在 TEE 密封内,
        // 所以这句必须带通道限定,不能写成全称判断。
        caption:
          "On platform-owned and shared-subscription routes, plaintext is decrypted only inside the attested TEE — never readable by ApexOne operators, logs, or infrastructure. The provider-supplied API relay route forwards requests to that provider's own endpoint, which sits outside this seal."
      },
      personal: {
        eyebrow: '// ApexOne',
        title: '14% of the Price. None of the Snooping.',
        subtitle: 'One gateway for price, privacy, and reliability — compare for yourself.',
        compare: {
          note: 'Based on official API billing price. ApexOne bills 14% of that price.',
          cols: {
            opaque: 'Opaque Gateway',
            official: 'Official API',
            apexone: 'ApexOne'
          },
          rows: {
            attestation: {
              label: 'Verifiable Privacy (Remote Attestation)',
              opaque: '❌ None',
              official: '❌ None',
              apexone: '✅ Verify in your browser'
            },
            dataAccess: {
              label: 'Prompts Sealed From Operator',
              opaque: '❌ Can read, may resell',
              official: '⚠️ Policy only',
              apexone: '✅ TEE-sealed, provable'
            },
            fable: {
              label: 'Real Model, Full Precision',
              opaque: '⚠️ Unverifiable',
              official: '✅ Yes',
              apexone: '✅ Attested, available now'
            },
            failover: {
              // 不承诺具体可用性数字:SLA 一旦写进首页就是对外承诺,
              // 而我们并没有对应的赔付条款。只讲我们确实做了的机制。
              label: 'Automatic Failover',
              opaque: '❌ No',
              official: '❌ No',
              apexone: '✅ Multi-route auto failover'
            },
            price: {
              label: 'Price',
              opaque: 'Cheap, unclear how',
              official: '1x Official Price',
              apexone: '14% of Official Price'
            }
          }
        },
        uptime: {
          title: 'Always-On Routing',
          desc: 'Multi-route architecture that reroutes traffic the moment a route degrades.',
          headline: 'Multi-Route Auto Failover',
          window: 'Availability design goal: reroute on degradation',
          points: {
            failover: 'Automatic failover when a route degrades',
            monitoring: 'Real-time route health monitoring'
          }
        },
        privacy: {
          title: 'TEE-Sealed Routing',
          desc: 'Data flows differ by supply route, so we spell each one out — and the TEE boundary itself is something you can verify from your browser.',
          // 按通道分开写,不是措辞讲究而是准确性:自营和共享订阅两条通道请求确实
          // 不出 TEE,但 API 中转通道会走到供给者自己的服务器上。合成一句"我们拿不到
          // 你的数据"对第三条就是假的,所以三条各写各的流向。
          points: {
            owned:
              'Platform-owned accounts: requests are routed inside an Intel TDX confidential VM, where neither operators nor logs can read plaintext',
            shared:
              'Shared subscription route: providers contribute quota only — requests travel from the TEE straight to Anthropic, never through any provider device',
            relay:
              'API relay route: requests are forwarded to the endpoint the provider supplied, which is outside the TEE seal'
          }
        }
      },
      pricing: {
        eyebrow: '// Pricing',
        title: '14% of official pricing. Nothing hidden.',
        subtitle: 'Billed per token at 14% of official API pricing — no subscriptions, no minimums.',
        personal: {
          name: 'ApexOne',
          tagline: 'Pay As You Go',
          priceLine: '14% of official API billing price, per token.',
          desc: 'For developers who want Claude access with verifiable private routing and transparent billing.',
          cta: 'Get Started →',
          features: {
            f1: 'Claude Fable 5 Available Now; GPT and Gemini Coming Soon',
            f2: '14% of Official API Billing Price',
            f3: 'TEE-Attested Gateway + Remote Attestation',
            f4: 'Metadata-Only Control Plane — Prompts Never Logged',
            f5: 'Automatic Failover & Real-Time Route Monitoring',
            f6: 'OpenAI-Compatible API — Works with Hermes and Other Clients'
          }
        }
      },
      // 供给侧区块。刻意只承诺我们控制得了的东西(分成比例、提现方式),
      // 用量和收入取决于真实需求,写成数字就会变成信任事故。
      supply: {
        eyebrow: '// Shared Subscriptions',
        title: 'Turn idle subscription quota into income.',
        subtitle:
          "You're already paying for a Claude subscription. When someone uses the part you didn't, you take a share.",
        howItWorks: {
          title: 'How It Works',
          s1: {
            title: 'Connect your subscription',
            desc: 'Authorize a connection, or supply a compatible API endpoint. You can go offline or unbind at any time.'
          },
          s2: {
            title: 'Idle quota gets used',
            desc: 'The platform routes requests to your account, drawing only on quota you have not used.'
          },
          // The share base must say "what the platform charges the user", not
          // "official pricing": the same page states we bill at 14% of official
          // pricing, so a vague base reads as "collect 14, pay out 50".
          s3: {
            title: 'Share the usage revenue',
            desc: 'You keep 50% of every fee the platform charges the user.'
          }
        },
        earnings: {
          title: 'How Earnings Are Calculated',
          formula: 'Your earnings = what users paid for that usage × revenue share',
          ratio: 'Current revenue share: 50%',
          // 全区块最重要的一句:平台还在早期,供给者按"月入多少"的预期进来
          // 一定会失望,这句必须留在页面上,不能被后来的转化优化砍掉。
          disclaimer:
            'What we commit to is the revenue share, not any particular amount of income. What you actually earn depends on real demand on the platform — and the platform is still early, with demand still building, so early earnings may be quite limited. Any earnings estimate is only an estimate, not a promise.'
        },
        payout: {
          title: 'How You Get Paid',
          chain: 'USDT paid on-chain, straight to your wallet address',
          fee: 'No withdrawal fee — network gas is covered by the platform',
          freeze: 'Earnings carry a 72-hour hold, and can be withdrawn any time after that'
        },
        privacy: {
          title: 'You never handle user data',
          desc: "On the shared subscription route, user requests travel from the platform's TEE gateway straight to Anthropic — they never pass through any device of yours. What you provide is quota, not servers.",
        },
        cta: 'See how sharing works'
      },
      video: {
        eyebrow: '// Why ApexOne',
        title: '14% of the price, none of the trade-off.',
        subtitle: "Why you don't have to trade privacy for cost."
      },
      cta: {
        title: 'Build on AI You Can Verify.',
        description: 'Claude Fable 5, at 14% of official pricing, sealed inside a TEE.',
        primary: 'Get Started →',
        secondary: 'Verify Privacy →',
        supply: 'Earn by Sharing →',
        pills: {
          noTraining: '🚫 No Training on Your Data',
          audit: '📋 Metadata-Only Audit Logs',
          encrypted: '🔒 Encrypted in Transit'
        }
      }
    },
    footer: {
      contactUs: 'Contact Us',
      stayConnected: 'Stay Connected',
      allRightsReserved: 'All rights reserved.',
      trademarkNotice:
        '*Claude is a trademark of Anthropic, PBC. ApexOne is an independent service and is not affiliated with Anthropic.'
    }
  },
}
