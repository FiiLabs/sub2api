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

    // Four states, four sentences: unpublished / never accepted / accepted an
    // older version / accepted. Telling someone who did click accept to
    // "accept first" reads as a broken system, hence the separate updatedTitle.
    agreement: {
      title: 'Read and accept the supplier agreement before connecting',
      updatedTitle: 'The supplier agreement has been updated — please review it again',
      version: 'Current version: {version}',
      openFullText: 'Read the full agreement',
      checkbox: 'I have read and accept version {version} of the Supplier Agreement',
      accept: 'Accept and continue',
      accepting: 'Submitting…',
      acceptedToast: 'Your acceptance has been recorded',
      acceptedAt: 'You accepted version {version} on {time}.',
      unpublishedTitle: 'No supplier agreement has been published yet',
      unpublishedBody: 'Subscriptions cannot be connected until the agreement is published. Only an administrator can do this — please contact the site admin.'
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
      detach: 'Disconnect',
      detaching: 'Disconnecting…',
      // The whole job of this copy is to spell out how disconnecting differs from
      // taking offline: the latter leaves the credential with us, only this deletes it.
      // Glossing over that lets someone who wants out believe "take offline" was enough.
      detachConfirm:
        'Disconnecting "{name}" stops it from taking requests immediately and permanently deletes the authorization credential we hold for it, after which the platform can no longer use the account. This cannot be undone. Earnings already accrued are unaffected and remain withdrawable. Disconnect it?',
      detached: 'Disconnected — the platform no longer holds a credential for this account',
      // The grant on the upstream side survives — Anthropic publishes no revoke endpoint
      // for the platform to call. Without this line people assume disconnecting revoked it.
      detachUpstreamHint:
        'The credential on our side is deleted. To revoke the grant on Anthropic’s side as well, go to your claude.ai account settings and remove the Claude Code authorization.',
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
      // Kept separate from `withdraw` on purpose: a refund has to read as money
      // coming back, not as income from nowhere.
      withdraw_revert: 'Withdrawal refund',
      unknown: 'Other'
    },

    // Withdrawals. Three things must be visible on screen, because all three
    // get misread: (1) the money leaves when you submit, not when it is
    // approved; (2) an amount below the minimum is rejected, not rounded up;
    // (3) "enabled but no channel" is the platform's misconfiguration.
    withdrawal: {
      title: 'Withdraw',
      closedTitle: 'Withdrawals are not open yet',
      closedBody: 'This platform has not opened withdrawals. Your balance is not going anywhere — you can withdraw it once they do.',
      channelsMissingTitle: 'Payout channels are being set up',
      channelsMissingBody: 'Withdrawals are enabled, but no payout channel has been configured yet. This is a platform-side configuration issue — contact an administrator instead of retrying.',
      deductHint: 'The amount leaves your available balance the moment you submit, not when the request is approved. If it is rejected or you cancel it, the money returns to your available balance.',
      pendingCount: '{count} / {max} pending',
      amountLabel: 'Amount (minimum {min})',
      useAll: 'All available ({amount})',
      channelLabel: 'Payout channel',
      channelPlaceholder: 'Select a payout channel',
      accountLabel: 'Payout account',
      accountPlaceholder: 'Your account for the selected channel',
      accountHint: 'Double-check this: payment goes to exactly what you enter here, and a wrong account cannot be recovered.',
      noteLabel: 'Note (optional)',
      notePlaceholder: 'Anything the operator should know',
      submit: 'Submit withdrawal request',
      submitting: 'Submitting…',
      submitted: 'Request submitted. The amount has been deducted from your available balance.',
      empty: 'No withdrawal requests yet.',
      createdAt: 'Requested',
      amount: 'Amount',
      channel: 'Payout',
      status: 'Status',
      reviewNote: 'Review note',
      actions: 'Actions',
      externalRef: 'Payment reference: {ref}',
      cancel: 'Cancel',
      cancelling: 'Cancelling…',
      cancelConfirm: 'Cancel this {amount} withdrawal request? The amount returns to your available balance.',
      cancelled: 'Cancelled. The amount is back in your available balance.',

      state: {
        pending: 'Pending',
        paid: 'Paid',
        rejected: 'Rejected',
        canceled: 'Cancelled',
        unknown: 'Unknown'
      }
    },

    error: {
      loadFailed: 'Failed to load',
      startFailed: 'Failed to start authorization',
      completeFailed: 'Failed to connect',
      pauseFailed: 'Failed to take offline',
      resumeFailed: 'Failed to put back',
      detachFailed: 'Failed to disconnect',
      acceptFailed: 'Failed to accept the agreement',
      codeRequired:'Enter the authorization code first',
      withdrawalAmountInvalid: 'Enter a withdrawal amount greater than 0',
      withdrawalChannelRequired: 'Select a payout channel',
      withdrawalAccountRequired: 'Enter your payout account',
      withdrawalFailed: 'Failed to submit the withdrawal request',
      withdrawalCancelFailed: 'Failed to cancel the withdrawal request'
    }
  },

  supplyAdmin: {
    navLabel: 'Two-sided market',
    title: 'Two-sided market',
    description: 'Configure supplier revenue share, supply-pool routing, probation, agreement and withdrawals. Each group saves separately.',

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

    // APEXONE-EXT: supplier agreement. Keep the tone narrow: this page decides
    // which version is published, not what the agreement says.
    agreement: {
      title: 'Supplier agreement',
      description: 'The text a supplier must accept before their first connection. With no agreement published, self-service onboarding is closed entirely.',
      publishedNotice: 'Agreement published. The consent gate is live: anyone who has not accepted this version cannot start an authorization.',
      unpublishedNotice: 'No agreement published — nobody can connect an account right now (the backend refuses to start authorization). Fill in a version and save to open it up.',
      version: 'Version',
      versionPlaceholder: 'e.g. v1, 2026-08-01',
      versionHint: 'Clearing the version withdraws the agreement and closes self-service onboarding. Changing it to a new value forces everyone who already accepted to accept again — including a typo fix. Handle with care.',
      url: 'Full-text link',
      urlHint: 'Optional. Absolute http/https URLs only. When set, the consent screen shows a "read the full text" link.',
      body: 'Agreement text',
      bodyPlaceholder: 'Paste plain text. The frontend renders it as plain text and does not parse HTML or Markdown.',
      bodyHint: 'Optional (leaving it empty means the link is all suppliers get). Up to {max} characters, counted as characters and not bytes.',
      rejectNotice: 'Out-of-range values here are rejected with an error rather than silently clamped like the review settings — half a truncated agreement is not an acceptable outcome.',
      save: 'Save agreement',
      saved: 'Agreement saved'
    },

    // Withdrawal parameters. Every hint here describes what goes wrong when the
    // value is wrong, because this group fails silently: enabled with no channel
    // configured looks perfectly healthy on the dashboard and hard-rejects every
    // supplier who tries.
    withdrawal: {
      title: 'Withdrawals',
      description: 'Decides whether suppliers can take their balance out, the minimum, and which payout channels exist.',
      openNotice: 'Withdrawals are open. The amount leaves the supplier\'s available balance the moment they submit, and waits for you on the Supply Operations page.',
      closedNotice: 'Withdrawals are closed. Balances keep accruing; they just cannot be taken out.',
      noChannelNotice: 'Withdrawals are enabled but no payout channel is configured — suppliers see an entry point that cannot be used. Either add a channel or turn the switch off.',
      enabled: 'Enable withdrawals',
      enabledHint: 'When off, balances keep accruing but no new request can be submitted. Requests already in flight are unaffected.',
      minAmount: 'Minimum amount',
      minAmountHint: 'A request below this is **rejected**, not rounded up to it. Max {max}.',
      maxPending: 'Pending requests per supplier',
      maxPendingHint: 'How many unresolved requests one supplier may hold at once. Max {max}.',
      channels: 'Payout channels',
      channelsPlaceholder: 'One per line, e.g.\nUSDT-TRC20\nPayPal\nBank transfer',
      channelsHint: 'One per line, at most {max} entries of {len} characters each. Submissions are matched **exactly** (only surrounding whitespace is trimmed), so USDT and usdt are two different channels and renaming one retires it.',
      notice: 'Notice shown to suppliers',
      noticePlaceholder: 'Processing time, fees, what information you need…',
      noticeHint: 'Displayed on the supplier withdrawal form. Plain text, at most {max} characters.',
      rejectNotice: 'Out-of-range values are rejected outright rather than clamped: a minimum silently clamped to the cap would lock everyone out of their money with nothing visibly wrong on this page.',
      save: 'Save withdrawal settings',
      saved: 'Withdrawal settings saved'
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
      thawHint: 'Thawed {thawed}, requested {withdrawn} and refunded {reverted} in this window. Thawing only moves frozen balance into available — do not add it to accruals, that counts the same money twice. Withdrawals are deducted **when requested**, so {withdrawn} is the requested amount, not what has been paid out; rejections and cancellations are reported separately as {reverted}. Do not net the two — the number of refunds is itself the signal that a channel is misconfigured or that review standards are off.'
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

    // Withdrawal review: the only write path on this page. The tone differs
    // from the rest — every line here describes an irreversible action.
    withdrawals: {
      title: 'Withdrawal review',
      description: 'The money already left the supplier\'s available balance when they submitted; this only moves the request forward. Marking paid cannot be undone. Rejecting returns the amount to their available balance.',
      anyStatus: 'Any status',
      userFilter: 'Only #{id}',
      requestedAt: 'Requested',
      user: 'Supplier',
      amount: 'Amount',
      payout: 'Payout',
      status: 'Status',
      actions: 'Actions',
      markPaid: 'Mark paid',
      markPaidConfirm: 'Confirm you have paid {amount} to {account}? There is no undo — the amount will not be returned.',
      externalRefPrompt: 'Payment reference / transaction id (may be left empty, but it is the only shared record if this is ever disputed)',
      markedPaid: 'Marked as paid.',
      reject: 'Reject',
      rejectPrompt: 'Reject this {amount} request. Enter a reason — it is shown to the supplier verbatim and is the only explanation they get:',
      rejected: 'Rejected. The amount is back in the supplier\'s available balance.'
    },

    error: {
      overviewFailed: 'Failed to load the dashboard',
      rosterFailed: 'Failed to load the roster',
      accountsFailed: 'Failed to load accounts',
      ledgerFailed: 'Failed to load the ledger',
      withdrawalsFailed: 'Failed to load withdrawal requests',
      withdrawalResolveFailed: 'Failed to resolve the withdrawal request',
      rejectNoteRequired: 'A rejection needs a reason'
    }
  }
}
