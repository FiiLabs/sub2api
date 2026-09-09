// /stats public metrics dashboard copy. Numbers are real aggregates + operator-configured
// baseline offsets; the growth curve is an illustrative synthesized shape.
export default {
  statsPage: {
    eyebrow: '// Platform metrics',
    title: 'ApexOne by the numbers',
    subtitle: 'A verifiable AI network, and it is growing.',
    hero: {
      live: 'Live'
    },
    models: {
      title: 'Supported models',
      subtitle: 'One API, multiple frontier models',
      live: 'Live',
      soon: 'Soon'
    },
    verify: {
      eyebrow: '// Verifiable',
      title: 'Every call is verifiable',
      desc: 'Unlike ordinary gateways, every inference on ApexOne is routed inside a TEE and can be verified from your browser — proof that the exact model you paid for served your request.',
      cta: 'See the privacy proof →'
    },
    highlights: {
      discount: { value: '14%', label: 'of official API price' },
      verifiable: { value: 'TEE', label: 'confidential routing' },
      models: { value: '2+', label: 'frontier model families' },
      failover: { value: 'Multi', label: 'automatic failover' }
    },
    trust: {
      eyebrow: '// Verifiable',
      title: 'Not a promise — verifiable',
      tee: 'Intel TDX confidential VM',
      attestation: 'Remote attestation, in your browser',
      noTraining: 'Never used for model training',
      cta: 'Verify it yourself →'
    },
    kpi: {
      sharedAccounts: 'Shared accounts',
      activeUsers: 'Active users',
      totalRequests: 'Requests served',
      totalTokens: 'Tokens processed',
      contributorEarnings: 'Paid to contributors (USDT)'
    },
    growth: {
      title: 'Request growth (last 30 days)',
      requests: 'Requests',
      users: 'Active users'
    },
    supply: {
      title: 'Supply mix',
      subtitle: 'Shared accounts by platform',
      claude: 'Claude',
      openai: 'OpenAI',
      other: 'Other'
    },
    empty: {
      title: 'Platform metrics are not published yet',
      desc: 'The operator has not turned on the public metrics display. Check back soon.'
    },
    cta: {
      use: 'Get started →',
      share: 'Earn by sharing →',
      home: 'Back to home'
    }
  }
}
