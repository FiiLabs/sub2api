// APEXONE-EXT: two-sided market — supplier-side copy.
// Note: vue-i18n treats "@" and "|" as syntax; a literal @ must be written {'@'}.
export default {
  supply: {
    // navLabel is separate from title: a sidebar has no room for a sentence.
    // Deliberately NOT placed in common.ts's `nav` namespace — that is an upstream
    // file, and this module is a pure addition.
    navLabel: 'Share Subscription',
    title: 'Share my subscription',
    description:
      'Connect your AI subscription. When someone else uses your idle quota, you earn a share of it.',

    disabled: {
      title: 'The supply market is not open yet',
      body: 'This site has no supply pool configured. Once an administrator sets one up, you will be able to connect your subscription here.'
    },

    settlementOff: {
      title: 'Settlement is currently off',
      body: 'Accounts you connect now will work, but the usage they serve will not be credited for the time being. Any balance you already earned is unaffected.'
    },

    wallet: {
      title: 'My earnings',
      available: 'Available',
      availableHint: 'Past the freeze window; can be spent',
      frozen: 'Frozen',
      frozenHint: 'Moves to available automatically once the freeze window passes',
      history: 'Total earned',
      spent: 'Spent',
      refresh: 'Refresh'
    },

    connect: {
      title: 'Connect a subscription',
      description: 'Two steps: authorize, then paste the code back here.',
      start: 'Connect my subscription',
      starting: 'Generating authorization link…',
      step1: 'Step 1 — authorize in a new tab',
      step1Hint: 'The link below opens the official authorization page. It will hand you an authorization code when you are done.',
      openAuthUrl: 'Open authorization page',
      copyAuthUrl: 'Copy link',
      authUrlCopied: 'Authorization link copied',
      step2: 'Step 2 — paste the code back',
      codeLabel: 'Authorization code',
      codePlaceholder: 'Paste the code from the authorization page',
      nameLabel: 'Name this account (optional)',
      namePlaceholder: 'Defaults to the upstream account email',
      submit: 'Finish connecting',
      submitting: 'Connecting…',
      cancel: 'Cancel',
      success: 'Connected. This account is now in review.',
      expiryHint: 'The authorization link is valid for 15 minutes. Start over if it expires.',
      pendingHint:
        'A newly connected account starts in review and does not take traffic. It only starts serving once the platform has verified it.'
    },

    accounts: {
      title: 'Connected subscriptions',
      empty: 'You have not connected any subscription yet.',
      name: 'Name',
      platform: 'Platform',
      state: 'Supply state',
      status: 'Account status',
      createdAt: 'Connected at',
      lastUsedAt: 'Last used',
      actions: 'Actions',
      never: 'Never used',
      pause: 'Take offline',
      pausing: 'Taking offline…',
      resume: 'Put back',
      resuming: 'Putting back…',
      pauseConfirm:
        'This account stops serving immediately. Earnings already accrued are unaffected. Take it offline?',
      paused: 'Taken offline',
      resumed: 'Put back — it re-enters review',
      schedulable: 'Serving',
      notSchedulable: 'Not serving'
    },

    state: {
      pending_review: 'In review',
      active: 'Live',
      retired: 'Offline',
      unknown: 'Unknown'
    },

    stateHint: {
      pending_review: 'Being verified; not serving yet',
      active: 'Serving normally',
      retired: 'You took this account offline'
    },

    ledger: {
      title: 'Earnings ledger',
      empty: 'No entries yet.',
      time: 'Time',
      action: 'Type',
      amount: 'Amount',
      basis: 'Basis',
      ratio: 'Share',
      frozenUntil: 'Unfreezes at',
      remark: 'Note',
      prev: 'Previous',
      next: 'Next',
      pageInfo: 'Page {page} of {pages} — {total} entries'
    },

    action: {
      accrue: 'Accrual',
      spend: 'Spend',
      thaw: 'Unfreeze',
      clawback: 'Clawback',
      withdraw: 'Withdrawal',
      unknown: 'Other'
    },

    error: {
      loadFailed: 'Failed to load',
      startFailed: 'Failed to start authorization',
      completeFailed: 'Failed to connect',
      pauseFailed: 'Failed to take offline',
      resumeFailed: 'Failed to put back',
      codeRequired: 'Enter the authorization code first'
    }
  },

  supplyAdmin: {
    navLabel: 'Two-sided market',
    title: 'Two-sided market',
    description: 'Configure supplier revenue share and supply-pool routing. The two save separately.',

    settlement: {
      title: 'Settlement',
      description: 'Decides how much suppliers earn and when they can touch it.',
      enabled: 'Enable settlement',
      enabledHint:
        'When off, supply accounts degrade to ordinary first-party accounts: they still serve traffic, but no revenue share is produced.',
      shareRatio: 'Share ratio',
      shareRatioHint:
        'Applied to what the consumer actually paid (not list price). 0.7 means the supplier keeps 70%. Max {max}.',
      freezeHours: 'Freeze hours',
      freezeHoursHint:
        'How long an accrual stays frozen. Must be at least as long as your payment provider chargeback window, or a chargeback after the freeze expires comes out of the platform. Max {max} hours.',
      spendFromWalletFirst: 'Spend from earn wallet first',
      spendFromWalletFirstHint:
        'When on, a user spends earned balance before topped-up balance. You can enable accrual first and open this outlet later.',
      save: 'Save settlement',
      saved: 'Settlement saved'
    },

    pool: {
      title: 'Supply-pool routing',
      description: 'When the supply pool has no usable account, fall back to the first-party pool.',
      enabled: 'Enable overflow',
      enabledHint: 'When off, scheduling behaves exactly as upstream.',
      supplyGroupId: 'Supply group ID',
      supplyGroupIdHint:
        'Only requests that resolve to this group overflow. The narrow gate is deliberate: if any empty group could overflow, one misconfigured group would silently serve traffic from platform-owned accounts.',
      overflowGroupId: 'Fallback group ID (first-party pool)',
      overflowGroupIdHint:
        'Must differ from the supply group. Existence is not validated here — a group can be deleted after configuration, so the real backstop lives in the scheduler.',
      costWarning:
        'Every overflow serves traffic at first-party cost while charging supply-pool price. Overflow rate is an operational metric worth watching (server log: [SupplyPool] supply pool exhausted).',
      save: 'Save pool routing',
      saved: 'Pool routing saved'
    },

    error: {
      loadFailed: 'Failed to load settings',
      saveFailed: 'Failed to save'
    }
  }
}
