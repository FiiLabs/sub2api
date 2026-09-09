// /stats public metrics dashboard copy. Numbers are real aggregates + operator-configured
// baseline offsets; the growth curve is an illustrative synthesized shape.
export default {
  statsPage: {
    eyebrow: '// Platform metrics',
    title: 'ApexOne by the numbers',
    subtitle: 'A verifiable AI network, and it is growing.',
    kpi: {
      sharedAccounts: 'Shared accounts',
      activeUsers: 'Active users',
      totalRequests: 'Requests served',
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
