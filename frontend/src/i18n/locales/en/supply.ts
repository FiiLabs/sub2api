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
      pauseNow: 'Take offline now',
      resume: 'Put back',
      resuming: 'Putting back…',
      cancelPause: 'Cancel offline',
      pauseHint:
        '"Take offline" starts a drain window: no new requests are routed to the account, and you can still change your mind. "Take offline now" goes straight to the final state, and putting the account back means going through review again. Neither one cuts off requests already streaming — those finish normally.',
      pauseConfirm:
        'The account stops taking new requests immediately and enters the drain window, which you can still cancel. Earnings already accrued are unaffected. Take it offline?',
      pauseNowConfirm:
        'Taking it offline now is final and cannot be cancelled; putting the account back means going through review again. Requests already streaming still finish. Continue?',
      paused: 'Taken offline',
      draining: 'Stopped taking new requests — draining',
      pauseCancelled: 'Offline cancelled — the account keeps serving',
      resumed: 'Put back — it re-enters review',
      schedulable: 'Serving',
      notSchedulable: 'Not serving',
      probePasses: '{passes} consecutive health checks passed',
      eligibleAt: 'Observation window is met no earlier than {time}',
      probeError: 'Health check failed: {reason}. You most likely need to re-authorize this account.',
      drainUntil: 'Draining until {time}'
    },

    state: {
      pending_review: 'In review',
      active: 'Live',
      draining: 'Draining',
      retired: 'Offline',
      unknown: 'Unknown'
    },

    stateHint: {
      pending_review: 'Being verified; not serving yet',
      active: 'Serving normally',
      draining: 'No longer taking new requests; goes offline when the drain window ends',
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
      dailyOverflowLimit: 'Daily overflow cap',
      dailyOverflowLimitHint:
        '0 = unlimited (still counted). Once the cap is used up, an exhausted supply pool returns the same "no available account" error it would have returned anyway — no new failure mode. If the counter is unreachable, overflow is refused: spending platform money cannot rest on not knowing how much was already spent.',
      overflowUsage: 'Today ({day}): {used} overflowed, {denied} blocked by the cap.',
      costWarning:
        'Every overflow serves traffic at first-party cost while charging supply-pool price. Overflow rate is an operational metric worth watching (server log: [SupplyPool] supply pool exhausted).',
      save: 'Save pool routing',
      saved: 'Pool routing saved'
    },

    probation: {
      title: 'Review period and offboarding',
      description: 'What a newly connected account must satisfy before it joins the supply pool, and how long a graceful offline waits.',
      enabled: 'Enable automatic admission',
      enabledHint:
        'When off, the review period still probes and still records, but nothing is admitted automatically — an operator has to make the account schedulable by hand. It ships off by default: watch a few days of data first.',
      minObservation: 'Minimum observation (minutes)',
      minObservationHint:
        'Counted from the moment the account is connected. ANDed with the success count: no matter how well the probes go, this much time still has to pass. Max {max} minutes.',
      requiredSuccesses: 'Consecutive successes required',
      requiredSuccessesHint:
        'One failure resets the counter and surfaces the reason to the supplier. Max {max}.',
      probeInterval: 'Probe interval (minutes)',
      probeIntervalHint:
        "Every probe is a real upstream request billed to the supplier's own quota — too short an interval spends their quota on probes. Range {min}–{max} minutes.",
      probeModel: 'Probe model ID',
      probeModelPlaceholder: 'Leave empty to use the platform default test model',
      probeModelHint: 'Pick a cheap small model. This affects probing only, never real scheduling.',
      drainWindow: 'Drain window (minutes)',
      drainWindowHint:
        'How long after a supplier takes an account offline before it reaches the final state. This is not a hard drain — the platform cannot interrupt requests already streaming, and the window doubles as the supplier\'s chance to change their mind. Set 0 and "take offline" becomes immediate. Max {max} minutes.',
      clampNotice:
        'Out-of-range values in this group are clamped and saved (not rejected). After saving, the form shows what is actually stored.',
      save: 'Save review settings',
      saved: 'Review settings saved'
    },

    error: {
      loadFailed: 'Failed to load settings',
      saveFailed: 'Failed to save'
    }
  },

  // APEXONE-EXT: read-only operations view. Kept apart from supplyAdmin (the
  // settings page): that one changes parameters, this one only reports numbers.
  supplyOps: {
    navLabel: 'Supply ops',
    title: 'Supply operations',
    description: 'What the supply side looks like right now: what is owed, who is serving, whose accounts are broken, what is stuck in review. Read-only.',
    search: 'Search',
    loading: 'Loading…',
    empty: 'Nothing matches.',

    window: {
      label: 'Window',
      days: 'Last {days} days'
    },

    overview: {
      owed: 'Owed to suppliers',
      owedBreakdown: 'Available {available} · Frozen {frozen}',
      suppliers: 'Suppliers',
      wallets: '{count} of them have a wallet record',
      accounts: 'Supply accounts',
      accountsBreakdown: 'Active {active} · Serving {schedulable}',
      accrued: 'Accrued in {days} days',
      windowBreakdown: 'Clawed back {clawed} · Spent by suppliers {spent}',
      unhealthy: 'Unhealthy accounts',
      thawHint: 'Thawed {thawed} and withdrew {withdrawn} in this window. Thawing only moves frozen balance into available — do not add it to accruals, that counts the same money twice. There is no withdrawal path in this release, so a non-zero figure means someone inserted ledger rows by hand.'
    },

    roster: {
      title: 'Supplier roster',
      description: 'Everyone who has had a supply account or a wallet balance. Deleted users who are still owed money stay listed — hiding them only makes the liability reappear at reconciliation time.',
      keywordPlaceholder: 'Email / username',
      supplier: 'Supplier',
      accounts: 'Accounts',
      accountsHint: 'Active {active} · In review {pending} · Unhealthy {unhealthy}',
      owed: 'Owed',
      history: 'Lifetime accrued',
      lastAccrual: 'Last accrual',
      neverAccrued: 'Never accrued',
      actions: 'Actions',
      viewAccounts: 'Their accounts',
      viewLedger: 'Their ledger',
      sort: {
        owed: 'Sort by owed',
        history: 'Sort by lifetime accrued',
        accounts: 'Sort by account count',
        recent: 'Sort by last accrual'
      }
    },

    accounts: {
      title: 'Supply accounts',
      description: 'Only accounts with an owner. First-party accounts are not listed here.',
      account: 'Account',
      owner: 'Owner',
      state: 'Supply state',
      health: 'Upstream health',
      lastUsedAt: 'Last used',
      never: 'Never used',
      anyState: 'Any state',
      anyHealth: 'Any health',
      healthy: 'Healthy',
      unhealthy: 'Unhealthy',
      schedulable: 'Serving traffic',
      notSchedulable: 'Not serving',
      probationSince: 'In review since {time}',
      probePasses: '{passes} consecutive probe successes',
      probeError: 'Probe failed: {reason}',
      drainUntil: 'Draining until {time}',
      ownerFilter: 'Owner #{id} only'
    },

    ledger: {
      title: 'Site-wide ledger',
      description: 'Wallet ledger across all suppliers. Reconciliation usually starts from a single request_id, so it can be matched exactly.',
      time: 'Time',
      user: 'Payee',
      action: 'Action',
      amount: 'Amount',
      basis: 'Basis',
      requestId: 'Request ID',
      anyAction: 'Any action',
      requestIdPlaceholder: 'Exact request_id',
      userFilter: 'User #{id} only'
    },

    error: {
      overviewFailed: 'Failed to load the dashboard',
      rosterFailed: 'Failed to load the roster',
      accountsFailed: 'Failed to load accounts',
      ledgerFailed: 'Failed to load the ledger'
    }
  }
}
