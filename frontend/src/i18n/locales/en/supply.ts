// APEXONE-EXT: two-sided market — supplier-side copy.
// Note: vue-i18n treats "@" and "|" as syntax; a literal @ must be written {'@'}.
export default {
  supply: {
    // navLabel is separate from title: a sidebar has no room for a sentence.
    // Deliberately NOT placed in common.ts's `nav` namespace — that is an upstream
    // file, and this module is a pure addition.
    navLabel: 'Share Subscription',
    // Section heading, deliberately different from the item label: the heading
    // says what this whole block is for (earning), the item says where it goes.
    navSection: 'Earn by Sharing',
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
      // Deliberately NOT "Available balance" — that name belongs to the
      // consumer top-up balance (common.availableBalance). A user who both
      // supplies and consumes would otherwise see two "available balances"
      // governed by entirely different rules: one is topped up and can only be
      // spent; this one is earned and can be withdrawn on-chain.
      available: 'Ready to withdraw',
      availableHint: 'Past the freeze window; can be withdrawn or spent',
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

    // Relay onboarding (M7). trustNotice is the one sentence that must never be
    // cut: the platform forwards consumer requests to the supplier's server,
    // and that has to be said BEFORE they hand over an endpoint.
    relay: {
      title: 'Or: connect an API relay',
      description: 'Provide an Anthropic-compatible /v1/messages endpoint and its API key; the platform forwards requests to it and you earn a share of the usage.',
      trustNotice: 'Note: once connected, the platform forwards user request content to the server you provide. You are obliged to handle that data properly — this is also part of the supplier agreement.',
      baseUrlLabel: 'Endpoint (base URL)',
      baseUrlHint: 'Must be https, without a path suffix — the platform appends /v1/messages itself.',
      apiKeyLabel: 'API key',
      nameLabel: 'Name this account (optional)',
      namePlaceholder: 'Defaults to the endpoint host',
      submit: 'Submit & verify',
      submitting: 'Verifying the endpoint…',
      probeHint: 'On submit, one minimal real request (1 token, same model as the review-period probe, claude-sonnet-4-5 by default) is sent to verify reachability and the key.',
      submitted: 'Connected. This account is now in review.',
      failed: 'Failed to connect',
      fieldsRequired: 'Both the base URL and the API key are required'
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
      // With a single channel there is no dropdown: a choice with no choices is noise.
      singleChannel: '{channel} · paid automatically to your bound address',
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

      wallet: {
        autoNotice: '{channel} pays out automatically to the on-chain address you bind — there is no account to type in.',
        empty: 'You have not bound a {network} payout address yet. This channel cannot be used until you do.',
        label: '{network} payout address ({token})',
        placeholder: 'The 0x address copied from your wallet',
        hint: 'Copy and paste it from your wallet — do not type it out and do not adjust a single character from memory. On-chain transfers cannot be reversed: a wrong address means the money successfully lands in someone else\'s hands and cannot be recovered.',
        bind: 'Bind',
        binding: 'Binding…',
        rebind: 'Change',
        cancelEdit: 'Cancel',
        rebindNotice: 'Changing it does not affect requests you already submitted — those carry the address as it was at submission time.',
        unbind: 'Unbind',
        unbinding: 'Unbinding…',
        unbindConfirm: 'Unbind your payout address on {network}? This channel becomes unusable until you bind a new one; requests already submitted are unaffected.',
        bindSuccess: 'Payout address bound.',
        unbindSuccess: 'Payout address unbound.'
      },

      // Fees. Withdrawals are fee-free now (the treasury covers gas);
      // `line` only renders on historical requests where fee_amount > 0.
      fee: {
        line: 'fee {fee} · net {net}',
        auto: 'auto payout · {network}'
      },

      state: {
        pending: 'Pending',
        // The two on-chain intermediate states (M4). "failed" deliberately
        // avoids the word itself: it means "automatic payout hit a snag and a
        // human is on it" — the money is still on the request.
        processing: 'Paying out',
        failed: 'Payout issue (being handled)',
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
      withdrawalFailed: 'Failed to submit the withdrawal request',
      withdrawalCancelFailed: 'Failed to cancel the withdrawal request',
      payoutAddressRequired: 'Enter the payout address',
      payoutWalletRequired: 'Bind an on-chain payout address for this channel first',
      payoutBindFailed: 'Failed to bind the payout address',
      payoutUnbindFailed: 'Failed to unbind the payout address'
    }
  },

  supplyAdmin: {
    navLabel: 'Two-sided market',
    title: 'Two-sided market',
    description: 'Configure supplier revenue share, supply-pool routing, probation, agreement and withdrawals. Each group saves separately.',

    // Pricing and supply health (read-only). Deliberately above the settings
    // cards: whether any parameter below is right can only be answered by these
    // numbers, which are derived from the money that actually moved — not by
    // whatever the input boxes say.
    health: {
      title: 'Pricing & supply health',
      description: 'Whether the multiplier, the share and the fallback are set right can only be read back from the money that moved. Everything on this card is read-only.',
      window: 'Window',
      windowDays: 'Last {days} days',
      loadFailed: 'Failed to load health readings',
      retry: 'Reload',
      listValue: 'Volume, last {days} days (list-price equivalent)',
      listValueHint: 'Platform revenue (what consumers actually paid): {revenue}',
      grossMargin: 'Gross margin',
      grossMarginHint: 'Revenue {revenue} − supplier share {payout}. Excludes fallback subscriptions, servers and other fixed costs; to see whether those are covered, compare this against your monthly fixed spend.',
      medianOutput: 'Median monthly output per supply account',
      medianOutputHint: '{suppliers} suppliers · {accounts} accounts producing. Median rather than mean: one or two heavy accounts pull the mean up and hide the fact that most suppliers earn almost nothing — which is exactly what precedes supply churn.',
      overflowShare: 'Fallback-served share',
      overflowShareHint: 'Platform-owned accounts served {value} of list-price equivalent. A high number means shared supply is thin and you should recruit suppliers; it does not mean the fallback accounts are running out.',

      selfCheck: {
        title: 'Configuration self-check',
        multiplier: 'Multiplier',
        share: 'Revenue share',
        effective: 'Effective {value}',
        configured: 'Configured {value}',
        aligned: 'aligned',
        drift: 'off by {drift}',
        noPool: 'no supply group configured, nothing to compare against',
        noShare: 'share configured as 0, nothing to compare against',
        // The causes are ordered by how often they happen: after a pricing
        // change this highlight stays on for a whole window, and copy that only
        // mentions "keys bound to another group" sends the operator hunting for
        // an incident that never occurred.
        mismatch:
          'The effective values differ from the configured ones. Two common causes: (1) the window spans a parameter change, so old and new billing are mixed inside it — expected right after a repricing, and it converges over time; (2) some consumer keys are bound to a different group, which uses its own multiplier. Switch to the 7-day window and see whether it converges to tell the two apart.'
      },

      exhausted: {
        title: '{count} requests today hit an empty supply pool AND an empty fallback pool.',
        body: 'Those consumers got "no available account" on the spot. This is the only signal that justifies adding fallback accounts — a high fallback-served share does not; that only means the fallback is being used.'
      },

      accounts: {
        title: 'Supply account output',
        description: 'Sorted by output in the window, descending. Accounts producing under $1500 a month land in the lowest band of the pricing plan, where the prescribed action is to raise the multiplier or suppliers will not stay.',
        name: 'Account',
        monthlyOutput: 'Monthly output',
        earned: 'Supplier earnings',
        requests: 'Requests',
        low: 'below expectation',
        empty: 'No supply account produced anything in this window.'
      },

      empty: {
        title: 'No volume in this window.',
        body: 'With no usage there is no multiplier or share to read back, so the tiles and the self-check stay hidden. Try a longer window, or come back once the first requests have run.'
      }
    },

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
      advancedTucked:
        'Engineering knobs (probe interval, required passes, drain window, probe model) are tucked away: the defaults are the recommendation, and the settings API still accepts manual overrides.',
      clampNotice:
        'Out-of-range values in this group are clamped and saved (not rejected). After saving, the form shows what is actually stored.',
      save: 'Save review settings',
      saved: 'Review settings saved'
    },

    // APEXONE-EXT: onboarding caps. The two gates get separate copy because they
    // stop different things, and the operator needs to know how wide the second
    // one's blast radius is before turning it on.
    onboarding: {
      relayEnabled: 'Allow “URL + API key” relay onboarding',
      relayEnabledHint: 'When on, suppliers can self-submit Anthropic-compatible relay endpoints, going through the same review and settlement as subscription accounts.',
      relayTrustWarning: 'Turning this on means accepting that consumer request content is forwarded to supplier-controlled servers. Make sure the supplier agreement spells out the data-handling obligations.',
      title: 'Onboarding limits',
      description: 'How many supply accounts one person, and one egress network, may connect. 0 means unlimited for both.',
      maxPerUser: 'Max accounts per user',
      maxPerUserHint:
        'Counts currently connected accounts — detaching one frees a slot. This gate is only a polite guardrail: registering a second user bypasses it. Set 0 for unlimited. Max {max}.',
      ipWarning:
        'This gate is on. Carrier-grade NAT, campus and office networks put hundreds of unrelated real users behind one address; anyone it blocks simply sees "cannot connect" and will not file a report. Look at your actual IP distribution first, then set a number far larger than one household.',
      clampNotice:
        'Out-of-range values in this group are clamped and saved (not rejected). After saving, the form shows what is actually stored.',
      save: 'Save onboarding limits',
      saved: 'Onboarding limits saved'
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

    // On-chain treasury (M6). The key goes in and never comes back out:
    // nothing on this card ever echoes the signer key; after saving, all you
    // can see is the treasury address derived from it.
    payoutChain: {
      title: 'On-chain treasury',
      description: 'Withdrawals are paid out on-chain by the treasury configured here. Saving takes effect immediately, no restart.',
      statusLive: 'Payout client is assembled (LIVE); chain id verified against the node.',
      statusUnverified: 'Client assembled, but the chain id could not be verified or there is a warning — see the error below.',
      statusOff: 'On-chain payout is off. The withdrawal entry is unavailable to suppliers.',
      enabled: 'Enable on-chain payout',
      enabledHint: 'With the treasury configured, withdrawal requests are settled on-chain by the worker. While off, suppliers cannot request withdrawals.',
      rpcUrl: 'Node RPC URL',
      chainId: 'Chain ID',
      chainIdHint: 'BSC mainnet is 56, testnet 97. Verified against the node on save.',
      tokenAddress: 'Stablecoin contract address',
      tokenAddressHint: 'Changing this address changes the coin: old requests are pinned to the contract at creation time, and the worker refuses to pay them in the new coin (failed → operator).',
      disperseAddress: 'Batch contract address (optional)',
      disperseHint: 'Leave empty for one transfer per request. With it, same-coin requests in a round are combined into one disperseToken call.',
      signerKey: 'Treasury signer key',
      signerKeyPlaceholder: '64 hex characters, 0x prefix optional',
      signerKeyKeep: 'Configured. Leave empty to keep it',
      signerKeyHint: 'Stored encrypted (AES-256-GCM) and never echoed back; the only thing shown is the treasury address derived from it.',
      treasury: 'Treasury address: {address}',
      verify: 'Test connection',
      verifying: 'Verifying…',
      save: 'Save & apply',
      saved: 'Treasury configuration saved and applied'
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
      noNotifyNotice:
        'Withdrawals are on, but nobody gets alerted when an on-chain payout fails — a failed payout holds the money on the request waiting for a human verdict, and the dashboard does not pop up by itself. Fill in recipients below.',
      enabled: 'Enable withdrawals',
      enabledHint: 'When off, balances keep accruing but no new request can be submitted. Requests already in flight are unaffected.',
      minAmount: 'Minimum amount',
      minAmountHint: 'A request below this is **rejected**, not rounded up to it. Max {max}.',
      notifyEmails: 'Payout-failure alert recipients',
      // A bare @ is vue-i18n linked-message syntax; it must be escaped as {'@'}.
      notifyEmailsPlaceholder: "finance{'@'}example.com\nops{'@'}example.com",
      notifyEmailsHint:
        'One per line, up to {max}, each at most {len} characters. Empty means nobody is called in when an on-chain payout fails — that money sits on a request nobody knows about. Kept separate from the quota-alert recipients: payout failures go to finance, alerts to ops. A malformed address fails the save with the culprit named, never silently dropped.',
      notice: 'Notice shown to suppliers',
      noticePlaceholder: 'Processing time, fees, what information you need…',
      noticeHint: 'Displayed on the supplier withdrawal form. Plain text, at most {max} characters.',
      rejectNotice: 'Out-of-range values are rejected outright rather than clamped: a minimum silently clamped to the cap would lock everyone out of their money with nothing visibly wrong on this page.',
      channelsRetired:
        'Payout channels are now derived from the on-chain treasury card: whatever coin the treasury can settle is what suppliers can withdraw. The old channel whitelist is no longer read.',
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

    // Two different time bases live side by side here (window vs. right now);
    // the copy has to say which is which or the numbers read as contradictory.
    incidents: {
      title: 'Supply account failures',
      description: 'One failure = one row. The account table above answers "how is it right now"; this answers "what happened over this period" — once an account recovers, the table above can no longer tell you it broke five times this month.',
      openOnly: 'Unresolved only',
      userFilter: 'Only #{id}',
      opened: 'New failures',
      resolved: 'Recovered',
      open: 'Currently broken',
      openNoWindow: 'Not limited by the window: accounts broken for months are counted',
      suppliers: 'Suppliers affected',
      inWindow: 'Last {days} days',
      ofAccounts: '{count} supply accounts in total',
      supplier: 'Supplier',
      accountsCol: 'Accounts',
      incidentsCol: 'Failures in window',
      openCol: 'Still broken',
      rateCol: 'Failure rate',
      lastDetectedAt: 'Last seen',
      detectedAt: 'Detected',
      account: 'Account',
      reason: 'Upstream status',
      state: 'State',
      closed: 'Recovered',
      stillOpen: 'Still broken',
      notNotified: 'Supplier not notified yet'
    },

    export: {
      button: 'Export last {days}d (CSV)',
      running: 'Exporting…',
      done: 'Export finished ({note})',
      truncated: 'Export was truncated: {note}. Narrow the time window and export again.',
      incomplete:
        'This file is incomplete and must not be used for payouts: the server failed mid-write. Saved as {name}. Please export again.'
    },

    error: {
      overviewFailed: 'Failed to load the dashboard',
      exportFailed: 'Export failed',
      rosterFailed: 'Failed to load the roster',
      accountsFailed: 'Failed to load accounts',
      ledgerFailed: 'Failed to load the ledger',
      withdrawalsFailed: 'Failed to load withdrawal requests',
      incidentSummaryFailed: 'Failed to load the failure report',
      incidentsFailed: 'Failed to load failure events',
      withdrawalResolveFailed: 'Failed to resolve the withdrawal request',
      rejectNoteRequired: 'A rejection needs a reason'
    }
  }
}
