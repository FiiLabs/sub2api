// 旧版(tee-control)营销首页/页头/页脚的 home.* 文案,整体覆盖 upstream landing 里的 home 块
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
    // User-focused value proposition
    heroSubtitle: 'One Key, All AI Models',
    heroDescription: 'No need to manage multiple subscriptions. Access Claude, GPT, Gemini and more with a single API key',
    tags: {
      subscriptionToApi: 'Subscription to API',
      stickySession: 'Session Persistence',
      realtimeBilling: 'Pay As You Go'
    },
    // Pain points section
    painPoints: {
      title: 'Sound Familiar?',
      items: {
        expensive: {
          title: 'High Subscription Costs',
          desc: 'Paying for multiple AI subscriptions that add up every month'
        },
        complex: {
          title: 'Account Chaos',
          desc: 'Managing scattered accounts and API keys across different platforms'
        },
        unstable: {
          title: 'Service Interruptions',
          desc: 'Single accounts hitting rate limits and disrupting your workflow'
        },
        noControl: {
          title: 'No Usage Control',
          desc: "Can't track where your money goes or limit team member usage"
        }
      }
    },
    // Solutions section
    solutions: {
      title: 'We Solve These Problems',
      subtitle: 'Three simple steps to stress-free AI access'
    },
    features: {
      unifiedGateway: 'One-Click Access',
      unifiedGatewayDesc: 'Get a single API key to call all connected AI models. No separate applications needed.',
      multiAccount: 'Always Reliable',
      multiAccountDesc: 'Smart routing across multiple upstream accounts with automatic failover. Say goodbye to errors.',
      balanceQuota: 'Pay What You Use',
      balanceQuotaDesc: 'Usage-based billing with quota limits. Full visibility into team consumption.'
    },
    // Comparison section
    comparison: {
      title: 'Why Choose Us?',
      headers: {
        feature: 'Comparison',
        official: 'Official Subscriptions',
        us: 'Our Platform'
      },
      items: {
        pricing: {
          feature: 'Pricing',
          official: 'Fixed monthly fee, pay even if unused',
          us: 'Pay only for what you use'
        },
        models: {
          feature: 'Model Selection',
          official: 'Single provider only',
          us: 'Switch between models freely'
        },
        management: {
          feature: 'Account Management',
          official: 'Manage each service separately',
          us: 'Unified key, one dashboard'
        },
        stability: {
          feature: 'Stability',
          official: 'Single account rate limits',
          us: 'Multi-account pool, auto-failover'
        },
        control: {
          feature: 'Usage Control',
          official: 'Not available',
          us: 'Quotas & detailed analytics'
        }
      }
    },
    providers: {
      title: 'Supported AI Models',
      description: 'One API, Multiple Choices',
      supported: 'Supported',
      soon: 'Soon',
      claude: 'Claude',
      gemini: 'Gemini',
      antigravity: 'Antigravity',
      more: 'More'
    },
    // CTA section
    cta: {
      title: 'Ready to Get Started?',
      description: 'Sign up now and get free trial credits to experience seamless AI access',
      button: 'Sign Up Free'
    },
    landing: {
      hero: {
        badge: 'Claude Fable 5 Live Today',
        title: 'Data privacy you can verify. Powered by TEE',
        subtitle:
          'Access frontier models like Claude through a TEE-protected gateway — your data stays private, every call is verifiable, all at 0.5x official API price.',
        verifyPrivacy: 'Verify Privacy',
        stats: {
          discount: 'Of Official API Price',
          attested: 'TEE Privacy Protection',
          claude: 'Live Today',
          hermes: 'Client Support'
        }
      },
      routing: {
        eyebrow: '// Confidential Routing Architecture',
        title: "We can't sell your data. We can't even read it.",
        subtitle:
          'Unlike opaque gateways, ApexOne routes every request through a verifiable TEE — plaintext never touches our hands.',
        caption:
          'Decrypted only inside the attested TEE — never readable by ApexOne operators, logs, or infrastructure.'
      },
      personal: {
        eyebrow: '// ApexOne',
        title: 'Half the Price. None of the Snooping.',
        subtitle: 'One gateway for price, privacy, and reliability — compare for yourself.',
        compare: {
          note: 'Based on official API billing price. ApexOne bills 50% of that price.',
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
              apexone: '✅ Attested, live today'
            },
            failover: {
              label: 'Automatic Failover',
              opaque: '❌ No',
              official: '❌ No',
              apexone: '✅ 99.99% uptime'
            },
            price: {
              label: 'Price',
              opaque: 'Cheap, unclear how',
              official: '1x Official Price',
              apexone: '0.5x Official Price'
            }
          }
        },
        uptime: {
          title: 'Always-On Routing',
          desc: 'Multi-route architecture that reroutes traffic the moment a route degrades.',
          window: 'Availability Design Target',
          points: {
            failover: 'Automatic failover when a route degrades',
            monitoring: 'Real-time route health monitoring'
          }
        },
        privacy: {
          title: 'TEE-Sealed Routing',
          desc: 'Prompts are decrypted only inside an Intel TDX confidential VM — a boundary you can verify from your browser.',
          points: {
            tdx: 'Decrypted only inside Intel TDX before provider handoff',
            noRead: 'ApexOne operators and logs cannot read prompts',
            attestation: 'Remote attestation verifies the gateway boundary'
          }
        }
      },
      pricing: {
        eyebrow: '// Pricing',
        title: '50% of official pricing. Nothing hidden.',
        subtitle: 'Billed per token at 50% of official API pricing — no subscriptions, no minimums.',
        personal: {
          name: 'ApexOne',
          tagline: 'Pay As You Go',
          priceLine: '50% of official API billing price, per token.',
          desc: 'For developers who want Claude access with verifiable private routing and transparent billing.',
          cta: 'Get Started →',
          features: {
            f1: 'Claude Fable 5 Live Today; GPT and Gemini Coming Soon',
            f2: '50% of Official API Billing Price',
            f3: 'TEE-Attested Gateway + Remote Attestation',
            f4: 'Metadata-Only Control Plane — Prompts Never Logged',
            f5: 'Automatic Failover & Real-Time Route Monitoring',
            f6: 'OpenAI-Compatible API — Works with Hermes and Other Clients'
          }
        }
      },
      cta: {
        title: 'Build on AI You Can Verify.',
        description: 'Claude Fable 5, half the official price, sealed inside a TEE.',
        primary: 'Get Started →',
        secondary: 'Verify Privacy →',
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
