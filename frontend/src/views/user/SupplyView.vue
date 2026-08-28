<!--
  APEXONE-EXT: 双边市场——供给者自助页（接入 + 仪表盘合一）。

  为什么是一页而不是两页：首版里供给者的全部动作是"挂一个号 / 看它有没有在接单 /
  看赚了多少"。拆成两页会让"我挂的号有没有在赚钱"这个唯一真正的问题，需要来回跳。

  界面上有三件事必须讲清楚，因为它们都是用户会误解的：
    1. 新号不接单（观察期），不是坏了；
    2. 结算开关是独立的，可能"能挂号但暂不计费"；
    3. 冻结中的余额不是丢了，是还没到解冻时间。
-->
<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <!-- 未开放：整页只剩一段说明。没配供给池时下面所有接口都会拒绝，
           显示一堆空表格只会让人以为是加载失败。 -->
      <div v-else-if="!status.enabled" class="card p-8 text-center">
        <Icon name="link" size="lg" class="mx-auto text-gray-400 dark:text-dark-500" />
        <h3 class="mt-3 text-base font-semibold text-gray-900 dark:text-white">
          {{ t('supply.disabled.title') }}
        </h3>
        <p class="mx-auto mt-2 max-w-lg text-sm text-gray-500 dark:text-dark-400">
          {{ t('supply.disabled.body') }}
        </p>
      </div>

      <template v-else>
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('supply.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.description') }}</p>
        </div>

        <!-- 接入开着但结算关着：这不是错误状态，但用户必须知道现在挂号不计费 -->
        <div
          v-if="!status.settlement_enabled"
          class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/40 dark:bg-amber-900/20"
        >
          <p class="text-sm font-medium text-amber-800 dark:text-amber-200">
            {{ t('supply.settlementOff.title') }}
          </p>
          <p class="mt-1 text-sm text-amber-700 dark:text-amber-300">{{ t('supply.settlementOff.body') }}</p>
        </div>

        <!-- ===================== 收益 ===================== -->
        <!-- 这几个 data-testid 是给排序护栏用的（见 SupplyView.layout.spec.ts）：
             区块顺序是这一页唯一一处"看起来无所谓、改回去也没人报错"的设计，
             所以它必须有一条会红的测试钉着。 -->
        <div class="space-y-3" data-testid="supply-wallet-card">
          <div class="flex items-center justify-between">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.wallet.title') }}</h3>
            <button class="btn btn-secondary btn-sm" :disabled="refreshing" @click="refreshAll">
              <Icon name="refresh" size="sm" />
              <span>{{ t('supply.wallet.refresh') }}</span>
            </button>
          </div>
          <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div class="card p-5">
              <p class="flex items-center gap-1.5 text-sm text-gray-500 dark:text-dark-400">
                <Icon name="dollar" size="sm" class="text-emerald-500" />
                {{ t('supply.wallet.available') }}
              </p>
              <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
                {{ formatCurrency(wallet?.available_credit ?? 0) }}
              </p>
              <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.wallet.availableHint') }}</p>
            </div>
            <div class="card p-5">
              <p class="flex items-center gap-1.5 text-sm text-gray-500 dark:text-dark-400">
                <Icon name="clock" size="sm" class="text-amber-500" />
                {{ t('supply.wallet.frozen') }}
              </p>
              <p class="mt-2 text-2xl font-semibold text-amber-600 dark:text-amber-400">
                {{ formatCurrency(wallet?.frozen_credit ?? 0) }}
              </p>
              <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.wallet.frozenHint') }}</p>
            </div>
            <div class="card p-5">
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('supply.wallet.history') }}</p>
              <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                {{ formatCurrency(wallet?.history_credit ?? 0) }}
              </p>
            </div>
            <div class="card p-5">
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('supply.wallet.spent') }}</p>
              <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
                {{ formatCurrency(wallet?.spent_credit ?? 0) }}
              </p>
            </div>
          </div>
        </div>

        <!-- ===================== 新手引导 ===================== -->
        <!--
          只对**一个号都没挂**的人显示，而不是常驻。
          对已经接入过的人，这三步不含任何新信息；常驻只会把他真正要看的东西
          （我的号在不在接单、赚了多少）往下推一屏，而那是他每次进来的唯一目的。

          位置在钱包卡之后、接入卡之前：钱包说的是"我现在有多少"，引导回答紧跟着
          的那个问题——"怎么让这几个数不是 0"，再往下就是他要动手的接入卡。
        -->
        <div v-if="accounts.length === 0" class="card p-6" data-testid="supply-guide-card">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.guide.title') }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.guide.subtitle') }}</p>

          <!-- 编号是圆圈里的数字而不是 list-decimal：这三步有先后依赖（没同意协议
               就走不到接入），把序号做成看得见的实体，比一个默认的列表标记更难被跳读。 -->
          <ol class="mt-5 space-y-4">
            <li
              v-for="step in guideSteps"
              :key="step.n"
              class="flex gap-3"
              :data-testid="`supply-guide-step-${step.n}`"
            >
              <span
                class="flex h-7 w-7 flex-none items-center justify-center rounded-full bg-primary-100 text-sm font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
              >
                {{ step.n }}
              </span>
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t(step.titleKey) }}</p>
                <p class="mt-0.5 text-sm text-gray-500 dark:text-dark-400">{{ t(step.descKey) }}</p>
              </div>
            </li>
          </ol>

          <div class="mt-5 border-t border-gray-100 pt-4 dark:border-dark-800">
            <p class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('supply.guide.afterTitle') }}</p>
            <ul class="mt-2 space-y-1.5 text-sm text-gray-500 dark:text-dark-400">
              <li class="flex gap-2"><span aria-hidden="true">·</span><span>{{ t('supply.guide.after1') }}</span></li>
              <li class="flex gap-2"><span aria-hidden="true">·</span><span>{{ t('supply.guide.after2') }}</span></li>
            </ul>

            <!-- 中性底色 + 加重字重，刻意**不用**红/黄的警告框：这句话不是风险提示，
                 是对"我能赚多少"的如实回答。做成警告色会让人以为有什么东西可能出错；
                 做成一行灰色小字又会被整段跳过——而它恰恰是最不该被跳过的那句。 -->
            <p
              class="mt-3 rounded-xl bg-gray-50 px-4 py-3 text-sm font-medium text-gray-700 dark:bg-dark-900 dark:text-gray-300"
              data-testid="supply-guide-disclaimer"
            >
              {{ t('supply.guide.after3') }}
            </p>

            <a
              class="mt-3 inline-flex items-center gap-1 text-sm text-primary-600 hover:underline dark:text-primary-400"
              :href="t('supply.guide.docsHref')"
              target="_blank"
              rel="noopener noreferrer"
              data-testid="supply-guide-docs"
            >
              <Icon name="externalLink" size="sm" />
              <span>{{ t('supply.guide.docsCta') }}</span>
            </a>
          </div>
        </div>

        <!-- ===================== 接入 ===================== -->
        <div class="card p-6" data-testid="supply-connect-card">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.connect.title') }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.connect.description') }}</p>

          <!-- 当前分成比例。引导卡里那句「比例见下方接入卡」指的就是这里——在这块
               存在之前，那句话把新用户指向了一个空位（比例只在流水表格里出现，
               而没挂过号的人根本没有流水）。读不到比例时整块不画：说不出这个数，
               比说一个错的好，因为它会被当成承诺。 -->
          <p
            v-if="shareRatioText"
            class="mt-2 text-sm font-medium text-primary-700 dark:text-primary-300"
            data-testid="supply-connect-share-ratio"
          >
            {{ t('supply.connect.shareRatio', { ratio: shareRatioText }) }}
          </p>

          <!-- 协议门禁。整块挡在接入按钮之前，而不是做成提交时的一个勾选框：
               服务端在 StartOAuth 和 CompleteOAuth 上都会拒绝没同意的人，界面
               把按钮留着只会让他跑完一整遍上游授权之后才被告知。 -->
          <div
            v-if="!agreement.published"
            class="mt-4 rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-900/40 dark:bg-amber-900/20"
            data-testid="supply-agreement-unpublished"
          >
            <p class="text-sm font-medium text-amber-800 dark:text-amber-200">
              {{ t('supply.agreement.unpublishedTitle') }}
            </p>
            <p class="mt-1 text-sm text-amber-700 dark:text-amber-300">{{ t('supply.agreement.unpublishedBody') }}</p>
          </div>

          <div v-else-if="!agreement.accepted" class="mt-4 space-y-3" data-testid="supply-agreement-gate">
            <!-- 「协议改版了」与「你还没同意过」是两句不同的话：对一个明明点过同意的人
                 说"请先同意"，他会以为系统坏了。 -->
            <p class="text-sm font-medium text-gray-800 dark:text-gray-100">
              {{ agreement.accepted_version ? t('supply.agreement.updatedTitle') : t('supply.agreement.title') }}
            </p>
            <p class="text-xs text-gray-400 dark:text-dark-500">
              {{ t('supply.agreement.version', { version: agreement.version }) }}
            </p>

            <!-- 正文按纯文本渲染（whitespace-pre-wrap + 插值），**不用 v-html**：
                 这份文本来自管理端表单，从别处粘来的条款里的 <script> 不该因为
                 它是"协议"就获得执行权。 -->
            <div
              v-if="agreement.body"
              class="max-h-64 overflow-y-auto whitespace-pre-wrap rounded-xl border border-gray-200 bg-gray-50 p-4 text-sm text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-300"
              data-testid="supply-agreement-body"
            >{{ agreement.body }}</div>

            <a
              v-if="agreement.url"
              class="inline-flex items-center gap-1 text-sm text-primary-600 hover:underline dark:text-primary-400"
              :href="agreement.url"
              target="_blank"
              rel="noopener noreferrer"
            >
              <Icon name="externalLink" size="sm" />
              <span>{{ t('supply.agreement.openFullText') }}</span>
            </a>

            <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input
                v-model="agreementChecked"
                class="mt-0.5"
                type="checkbox"
                data-testid="supply-agreement-checkbox"
              />
              <span>{{ t('supply.agreement.checkbox', { version: agreement.version }) }}</span>
            </label>

            <button
              class="btn btn-primary"
              :disabled="!agreementChecked || accepting"
              data-testid="supply-agreement-accept"
              @click="acceptAgreement"
            >
              <Icon name="check" size="sm" />
              <span>{{ accepting ? t('supply.agreement.accepting') : t('supply.agreement.accept') }}</span>
            </button>
          </div>

          <template v-else>
            <p class="mt-3 text-xs text-gray-400 dark:text-dark-500" data-testid="supply-agreement-accepted">
              {{
                t('supply.agreement.acceptedAt', {
                  version: agreement.version,
                  time: agreement.accepted_at ? formatDateTime(agreement.accepted_at) : '-',
                })
              }}
            </p>

            <button
              v-if="!pendingAuth"
              class="btn btn-primary mt-4"
              :disabled="starting"
              data-testid="supply-start-oauth"
              @click="startOAuth"
            >
              <Icon name="plus" size="sm" />
              <span>{{ starting ? t('supply.connect.starting') : t('supply.connect.start') }}</span>
            </button>

            <div v-else class="mt-4 space-y-4">
            <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900">
              <p class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('supply.connect.step1') }}</p>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.connect.step1Hint') }}</p>
              <div class="mt-3 flex flex-col gap-2 sm:flex-row">
                <a
                  class="btn btn-primary btn-sm"
                  :href="pendingAuth.auth_url"
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <Icon name="externalLink" size="sm" />
                  <span>{{ t('supply.connect.openAuthUrl') }}</span>
                </a>
                <button class="btn btn-secondary btn-sm" @click="copyAuthUrl">
                  <Icon name="copy" size="sm" />
                  <span>{{ t('supply.connect.copyAuthUrl') }}</span>
                </button>
              </div>
              <p class="mt-3 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.connect.expiryHint') }}</p>
            </div>

            <div class="space-y-3">
              <p class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ t('supply.connect.step2') }}</p>
              <div>
                <label class="input-label" for="supply-code">{{ t('supply.connect.codeLabel') }}</label>
                <input
                  id="supply-code"
                  v-model="authCode"
                  class="input"
                  type="text"
                  autocomplete="off"
                  :placeholder="t('supply.connect.codePlaceholder')"
                  data-testid="supply-auth-code"
                />
              </div>
              <div>
                <label class="input-label" for="supply-name">{{ t('supply.connect.nameLabel') }}</label>
                <input
                  id="supply-name"
                  v-model="accountName"
                  class="input"
                  type="text"
                  :placeholder="t('supply.connect.namePlaceholder')"
                />
              </div>
              <p class="text-xs text-gray-400 dark:text-dark-500">{{ t('supply.connect.pendingHint') }}</p>
              <div class="flex gap-2">
                <button
                  class="btn btn-primary"
                  :disabled="submitting"
                  data-testid="supply-complete-oauth"
                  @click="completeOAuth"
                >
                  <Icon name="check" size="sm" />
                  <span>{{ submitting ? t('supply.connect.submitting') : t('supply.connect.submit') }}</span>
                </button>
                <button class="btn btn-secondary" :disabled="submitting" @click="cancelAuth">
                  {{ t('supply.connect.cancel') }}
                </button>
              </div>
            </div>
            </div>

            <!-- ===== 中转接入（M7），管理端开关控制 =====
                 与 OAuth 并排的第二条路：填一个 Anthropic 兼容端点 + API Key。
                 信任提示必须在提交按钮之前：平台会把**消费者的请求**转发到他
                 填的服务器上，这件事得在他交出端点之前说清，而不是写进事后条款。 -->
            <div
              v-if="status.relay_enabled"
              class="mt-6 border-t border-gray-100 pt-4 dark:border-dark-800"
              data-testid="supply-relay-section"
            >
              <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('supply.relay.title') }}</h4>
              <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('supply.relay.description') }}</p>
              <p class="mt-2 rounded-lg border border-amber-200 bg-amber-50 p-2 text-xs text-amber-700 dark:border-amber-900/40 dark:bg-amber-900/20 dark:text-amber-300">
                {{ t('supply.relay.trustNotice') }}
              </p>

              <div class="mt-3 grid gap-3 sm:grid-cols-2">
                <div class="sm:col-span-2">
                  <label class="input-label" for="relay-base-url">{{ t('supply.relay.baseUrlLabel') }}</label>
                  <input
                    id="relay-base-url"
                    v-model="relayForm.base_url"
                    class="input font-mono text-xs"
                    type="text"
                    autocomplete="off"
                    spellcheck="false"
                    placeholder="https://relay.example.com"
                    data-testid="supply-relay-base-url"
                  />
                  <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.relay.baseUrlHint') }}</p>
                </div>
                <div class="sm:col-span-2">
                  <label class="input-label" for="relay-api-key">{{ t('supply.relay.apiKeyLabel') }}</label>
                  <input
                    id="relay-api-key"
                    v-model="relayForm.api_key"
                    class="input font-mono text-xs"
                    type="password"
                    autocomplete="off"
                    data-testid="supply-relay-api-key"
                  />
                </div>
                <div class="sm:col-span-2">
                  <label class="input-label" for="relay-name">{{ t('supply.relay.nameLabel') }}</label>
                  <input
                    id="relay-name"
                    v-model="relayForm.name"
                    class="input"
                    type="text"
                    :placeholder="t('supply.relay.namePlaceholder')"
                    data-testid="supply-relay-name"
                  />
                </div>
              </div>

              <button
                class="btn btn-primary mt-3"
                :disabled="submittingRelay"
                data-testid="supply-relay-submit"
                @click="submitRelay"
              >
                {{ submittingRelay ? t('supply.relay.submitting') : t('supply.relay.submit') }}
              </button>
              <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.relay.probeHint') }}</p>
            </div>
          </template>
        </div>

        <!-- ===================== 我的号 ===================== -->
        <div class="card p-6" data-testid="supply-accounts-card">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.accounts.title') }}</h3>
          <!-- 两条下线通道的边界，写在表格上方而不是塞进按钮 tooltip：
               "已经在流的请求停不掉"是用户唯一会因此投诉的点。 -->
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('supply.accounts.pauseHint') }}</p>

          <p v-if="accounts.length === 0" class="mt-4 text-sm text-gray-500 dark:text-dark-400">
            {{ t('supply.accounts.empty') }}
          </p>

          <div v-else class="mt-4 overflow-x-auto">
            <table class="min-w-full text-sm">
              <thead>
                <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-dark-700 dark:text-dark-400">
                  <th class="px-3 py-2">{{ t('supply.accounts.name') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.platform') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.state') }}</th>
                  <!-- 每日上限排在「接单状态」之后：那一列回答「在不在接单」，
                       紧接着的追问就是「为什么不接 / 还剩多少」。 -->
                  <th class="px-3 py-2">{{ t('supply.accounts.dailyCap') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.status') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.lastUsedAt') }}</th>
                  <th class="px-3 py-2">{{ t('supply.accounts.createdAt') }}</th>
                  <th class="px-3 py-2 text-right">{{ t('supply.accounts.actions') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                <template v-for="account in accounts" :key="account.id">
                <tr :data-testid="`supply-account-${account.id}`">
                  <td class="px-3 py-3">
                    <p class="font-medium text-gray-900 dark:text-white">{{ account.name }}</p>
                    <p v-if="account.email_address" class="text-xs text-gray-400 dark:text-dark-500">
                      {{ account.email_address }}
                    </p>
                  </td>
                  <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ account.platform }}</td>
                  <td class="px-3 py-3">
                    <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="stateBadgeClass(account.supply_state)">
                      {{ stateLabel(account.supply_state) }}
                    </span>
                    <!-- 触顶优先于 schedulable。
                         闸门是调度层过滤、不写库，所以触顶时 account.schedulable
                         **仍然是 true**——只看它就会在一个一分钱赚不到的号上显示
                         「接单中」。这一行是整个每日上限功能里最要紧的一处显示。 -->
                    <p
                      v-if="account.daily_cap_reached"
                      class="mt-1 text-xs font-medium text-amber-600 dark:text-amber-400"
                      :data-testid="`supply-daily-cap-reached-${account.id}`"
                    >
                      {{ t('supply.accounts.dailyCapReached') }}
                    </p>
                    <p v-else class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                      {{ account.schedulable ? t('supply.accounts.schedulable') : t('supply.accounts.notSchedulable') }}
                    </p>

                    <!-- 观察期进度。两个判据分开显示：用户要能分辨"还在等时间"和
                         "我的号连不上"——后者只有他自己能修。 -->
                    <template v-if="account.supply_state === 'pending_review'">
                      <p v-if="account.probe_passes > 0" class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                        {{ t('supply.accounts.probePasses', { passes: account.probe_passes }) }}
                      </p>
                      <p v-if="account.eligible_at" class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                        {{ t('supply.accounts.eligibleAt', { time: formatDateTime(account.eligible_at) }) }}
                      </p>
                      <p v-if="account.probe_error" class="mt-1 max-w-xs text-xs text-red-500">
                        {{ t('supply.accounts.probeError', { reason: account.probe_error }) }}
                      </p>
                    </template>

                    <p
                      v-if="account.supply_state === 'draining' && account.drain_until"
                      class="mt-1 text-xs text-amber-600 dark:text-amber-400"
                    >
                      {{ t('supply.accounts.drainUntil', { time: formatDateTime(account.drain_until) }) }}
                    </p>
                  </td>
                  <!-- ===== 每日共享上限 ===== -->
                  <td class="px-3 py-3" :data-testid="`supply-daily-cap-cell-${account.id}`">
                    <template v-if="hasDailyCap(account)">
                      <p v-if="(account.daily_cost_limit_usd ?? 0) > 0" class="text-xs text-gray-700 dark:text-gray-300">
                        {{ formatUsd(account.daily_cost_used_usd ?? 0) }} / {{ formatUsd(account.daily_cost_limit_usd ?? 0) }}
                      </p>
                      <p v-if="(account.daily_token_limit ?? 0) > 0" class="text-xs text-gray-700 dark:text-gray-300">
                        {{ formatTokens(account.daily_tokens_used ?? 0) }} / {{ formatTokens(account.daily_token_limit ?? 0) }}
                        {{ t('supply.accounts.dailyCapTokensUnit') }}
                      </p>
                      <!-- 这句不能省：分母是**官方牌价**，不是他的收益（两者差一个倍率）。
                           少了它，供给者会拿这个数去对自己的分成、得出「数字不对」的结论。 -->
                      <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.accounts.dailyCapBasisHint') }}</p>
                      <p v-if="account.daily_cap_reset_at" class="text-xs text-gray-400 dark:text-dark-500">
                        {{ t('supply.accounts.dailyCapResetAt', { time: formatDateTime(account.daily_cap_reset_at) }) }}
                      </p>
                    </template>
                    <p v-else class="text-xs text-gray-400 dark:text-dark-500">
                      {{ t('supply.accounts.dailyCapUnlimited') }}
                    </p>
                    <button
                      type="button"
                      class="mt-1 text-xs text-primary-600 hover:underline dark:text-primary-400"
                      :disabled="mutatingId === account.id"
                      :data-testid="`supply-daily-cap-edit-${account.id}`"
                      @click="toggleCapEditor(account)"
                    >
                      {{ capEditingId === account.id ? t('supply.accounts.dailyCapCancel') : t('supply.accounts.dailyCapEdit') }}
                    </button>
                  </td>
                  <td class="px-3 py-3">
                    <span class="text-gray-700 dark:text-gray-300">{{ account.status }}</span>
                    <p v-if="account.error_message" class="mt-1 max-w-xs text-xs text-red-500">
                      {{ account.error_message }}
                    </p>
                  </td>
                  <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                    {{ account.last_used_at ? formatDateTime(account.last_used_at) : t('supply.accounts.never') }}
                  </td>
                  <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(account.created_at) }}</td>
                  <td class="px-3 py-3 text-right">
                    <!-- 三种状态三套动作。draining 特殊在它同时能"反悔"和"别等了"，
                         所以那两个按钮必须同时在场，否则用户只能干等排空窗。 -->
                    <div class="flex flex-wrap justify-end gap-2">
                      <template v-if="account.supply_state === 'retired'">
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-resume-${account.id}`"
                          @click="resumeAccount(account)"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.resuming') : t('supply.accounts.resume') }}
                        </button>
                      </template>
                      <template v-else-if="account.supply_state === 'draining'">
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-cancel-pause-${account.id}`"
                          @click="resumeAccount(account)"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.resuming') : t('supply.accounts.cancelPause') }}
                        </button>
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-pause-immediate-${account.id}`"
                          @click="pauseAccount(account, 'immediate')"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.pausing') : t('supply.accounts.pauseNow') }}
                        </button>
                      </template>
                      <template v-else>
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-pause-${account.id}`"
                          @click="pauseAccount(account, 'graceful')"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.pausing') : t('supply.accounts.pause') }}
                        </button>
                        <button
                          class="btn btn-secondary btn-sm"
                          :disabled="mutatingId === account.id"
                          :data-testid="`supply-pause-immediate-${account.id}`"
                          @click="pauseAccount(account, 'immediate')"
                        >
                          {{ mutatingId === account.id ? t('supply.accounts.pausing') : t('supply.accounts.pauseNow') }}
                        </button>
                      </template>
                      <!-- 解绑对所有状态都在场，而且不随状态变化。
                           「撤回自己的授权」不该有任何一个状态是够不着的——包括
                           已经下线的号，那正是最该把凭证收回去的时候。 -->
                      <button
                        class="btn btn-danger btn-sm"
                        :disabled="mutatingId === account.id"
                        :data-testid="`supply-detach-${account.id}`"
                        @click="detachAccount(account)"
                      >
                        {{ mutatingId === account.id ? t('supply.accounts.detaching') : t('supply.accounts.detach') }}
                      </button>
                    </div>
                  </td>
                </tr>

                <!-- 上限编辑器：行内展开行，不做弹窗。
                     这一页目前一个弹窗都没有（OAuth 流程也是行内的），引入第一个
                     会让 diff 大很多，而这里要编辑的只有两个数字。 -->
                <tr v-if="capEditingId === account.id" :data-testid="`supply-daily-cap-editor-${account.id}`">
                  <td colspan="8" class="bg-gray-50 px-3 py-3 dark:bg-dark-800/50">
                    <div class="flex flex-wrap items-end gap-3">
                      <div>
                        <label class="mb-1 block text-xs text-gray-500 dark:text-dark-400">
                          {{ t('supply.accounts.dailyCapCostLabel') }}
                        </label>
                        <input
                          v-model="capForm.cost"
                          type="number"
                          min="0"
                          step="0.01"
                          class="input w-40 text-sm"
                          :data-testid="`supply-daily-cap-cost-${account.id}`"
                        />
                      </div>
                      <div>
                        <label class="mb-1 block text-xs text-gray-500 dark:text-dark-400">
                          {{ t('supply.accounts.dailyCapTokenLabel') }}
                        </label>
                        <input
                          v-model="capForm.tokens"
                          type="number"
                          min="0"
                          step="1000"
                          class="input w-40 text-sm"
                          :data-testid="`supply-daily-cap-tokens-${account.id}`"
                        />
                      </div>
                      <button
                        class="btn btn-primary btn-sm"
                        :disabled="mutatingId === account.id"
                        :data-testid="`supply-daily-cap-save-${account.id}`"
                        @click="saveDailyCap(account)"
                      >
                        {{ mutatingId === account.id ? t('supply.accounts.dailyCapSaving') : t('supply.accounts.dailyCapSave') }}
                      </button>
                    </div>
                    <p class="mt-2 text-xs text-gray-400 dark:text-dark-500">{{ t('supply.accounts.dailyCapHint') }}</p>
                  </td>
                </tr>
                </template>
              </tbody>
            </table>
          </div>
        </div>

        <!-- ===================== 提现 ===================== -->
        <!--
          排在接入和账号之后，而不是紧跟着钱包。
          页面的第一屏要回答"怎么开始赚"，不是"怎么把钱取走"：对一个还没接入任何
          号的人，提现表单里的每一个控件都是他此刻按不动的——余额是 0、地址没绑、
          提交必然被拒。把它放在第一眼，等于用一屏他做不了的事挡住他唯一该做的事。

          往下挪不影响已经在赚的人：他们进这一页是带着"取钱"这个目的来的，会往下找；
          而新用户不会去找一个他还不知道自己需要的东西。

          整块在提现没开时**不隐藏**，而是显示一句为什么。隐藏等于让供给者对着一个
          只增不减的余额自己猜，而"暂未开放"和"渠道维护中"是两句不同的话（见下）。
        -->
        <div class="card p-6" data-testid="supply-withdrawal-card">
          <div class="flex items-center justify-between">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">
              {{ t('supply.withdrawal.title') }}
            </h3>
            <p v-if="withdrawalOptions" class="text-xs text-gray-400 dark:text-dark-500">
              {{
                t('supply.withdrawal.pendingCount', {
                  count: withdrawalOptions.pending_count,
                  max: withdrawalOptions.max_pending,
                })
              }}
            </p>
          </div>

          <!-- 三种关闭原因三句话：
               1. enabled=false        → 平台还没开提现
               2. enabled 但没配渠道   → 开了但配漏了，是**平台的**问题，不是他的
               运营看到的和供给者看到的是同一件事，所以第 2 句不能写成"暂未开放"。 -->
          <div
            v-if="withdrawalOptions && !withdrawalOptions.available"
            class="mt-4 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900"
            data-testid="supply-withdrawal-unavailable"
          >
            <p class="text-sm font-medium text-gray-800 dark:text-gray-100">
              {{
                withdrawalOptions.enabled
                  ? t('supply.withdrawal.channelsMissingTitle')
                  : t('supply.withdrawal.closedTitle')
              }}
            </p>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{
                withdrawalOptions.enabled
                  ? t('supply.withdrawal.channelsMissingBody')
                  : t('supply.withdrawal.closedBody')
              }}
            </p>
          </div>

          <template v-else-if="withdrawalOptions">
            <!-- 「申请即扣款」写在表单最上面，不是提交后的 toast 里：
                 供给者以为申请只是排队，然后发现余额少了一块，那是一次支持工单。 -->
            <p class="mt-3 text-sm text-gray-500 dark:text-dark-400">
              {{ t('supply.withdrawal.deductHint') }}
            </p>
            <p v-if="withdrawalOptions.notice" class="mt-2 whitespace-pre-wrap text-sm text-gray-500 dark:text-dark-400">
              {{ withdrawalOptions.notice }}
            </p>

            <div class="mt-4 grid gap-4 sm:grid-cols-2">
              <div>
                <label class="input-label" for="withdrawal-amount">
                  {{ t('supply.withdrawal.amountLabel', { min: formatCurrency(withdrawalOptions.min_amount) }) }}
                </label>
                <input
                  id="withdrawal-amount"
                  v-model="withdrawalForm.amount"
                  class="input"
                  type="number"
                  min="0"
                  step="0.01"
                  data-testid="supply-withdrawal-amount"
                />
                <button
                  class="mt-1 text-xs text-primary-600 hover:underline dark:text-primary-400"
                  type="button"
                  data-testid="supply-withdrawal-all"
                  @click="fillMaxAmount"
                >
                  {{ t('supply.withdrawal.useAll', { amount: formatCurrency(withdrawalOptions.available_credit) }) }}
                </button>
              </div>

              <div>
                <label class="input-label" for="withdrawal-channel">{{ t('supply.withdrawal.channelLabel') }}</label>
                <!-- 只有一个渠道时不画下拉（onMounted 已自动选中）：
                     让人"选择"一个没有选择的下拉是噪音。多渠道（将来加链/加币）
                     时下拉回来。渠道仍是 select 而不是自由输入：后端按**完全
                     相等**校验，手打的 "usdt" 会被拒，而那个错误像系统在刁难人。 -->
                <p
                  v-if="withdrawalOptions.channels.length === 1"
                  class="input flex items-center text-gray-700 dark:text-gray-300"
                  data-testid="supply-withdrawal-single-channel"
                >
                  {{ t('supply.withdrawal.singleChannel', { channel: withdrawalOptions.channels[0] }) }}
                </p>
                <select
                  v-else
                  id="withdrawal-channel"
                  v-model="withdrawalForm.payout_channel"
                  class="input"
                  data-testid="supply-withdrawal-channel"
                >
                  <option value="" disabled>{{ t('supply.withdrawal.channelPlaceholder') }}</option>
                  <option v-for="channel in withdrawalOptions.channels" :key="channel" :value="channel">
                    {{ channel }}
                  </option>
                </select>
              </div>

              <!-- 收款地址只有一个来源：绑定（M6b 起提现只剩链上渠道）。
                   这张表单上**不存在**自由输入收款账号的地方——一串手填的、
                   未经校验的字符被当成链上地址打出去，正是绑定机制要消灭的东西。 -->
              <div v-if="selectedOnchainChannel" class="sm:col-span-2" data-testid="supply-payout-wallet">
                <label class="input-label">
                  {{
                    t('supply.withdrawal.wallet.label', {
                      network: selectedOnchainChannel.network,
                      token: selectedOnchainChannel.token_symbol,
                    })
                  }}
                </label>
                <p class="text-xs text-gray-400 dark:text-dark-500">
                  {{ t('supply.withdrawal.wallet.autoNotice', { channel: selectedOnchainChannel.channel }) }}
                </p>

                <!-- 已绑定：默认只读地显示那一串，不摊在可编辑的输入框里。
                     地址是个看一眼就够的东西，让它一直可编辑只是多一次误改机会。 -->
                <div
                  v-if="boundPayoutWallet && !editingWallet"
                  class="mt-2 flex flex-wrap items-center gap-2 rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-900"
                >
                  <code class="break-all text-sm text-gray-800 dark:text-gray-100" data-testid="supply-payout-wallet-address">
                    {{ boundPayoutWallet.address }}
                  </code>
                  <span class="grow"></span>
                  <button
                    class="btn btn-secondary btn-sm"
                    type="button"
                    data-testid="supply-payout-wallet-rebind"
                    @click="startEditingWallet"
                  >
                    {{ t('supply.withdrawal.wallet.rebind') }}
                  </button>
                  <button
                    class="btn btn-secondary btn-sm"
                    type="button"
                    :disabled="unbindingWallet"
                    data-testid="supply-payout-wallet-unbind"
                    @click="unbindPayoutWallet"
                  >
                    {{
                      unbindingWallet
                        ? t('supply.withdrawal.wallet.unbinding')
                        : t('supply.withdrawal.wallet.unbind')
                    }}
                  </button>
                </div>

                <!-- 未绑定 / 换绑中 -->
                <div v-else class="mt-2">
                  <p
                    v-if="!boundPayoutWallet"
                    class="mb-2 text-sm text-amber-700 dark:text-amber-300"
                    data-testid="supply-payout-wallet-empty"
                  >
                    {{ t('supply.withdrawal.wallet.empty', { network: selectedOnchainChannel.network }) }}
                  </p>
                  <p v-else class="mb-2 text-xs text-gray-400 dark:text-dark-500">
                    {{ t('supply.withdrawal.wallet.rebindNotice') }}
                  </p>
                  <div class="flex flex-wrap items-start gap-2">
                    <input
                      v-model="payoutAddressInput"
                      class="input grow"
                      type="text"
                      autocomplete="off"
                      spellcheck="false"
                      :placeholder="t('supply.withdrawal.wallet.placeholder')"
                      data-testid="supply-payout-wallet-input"
                    />
                    <button
                      class="btn btn-primary"
                      type="button"
                      :disabled="bindingWallet"
                      data-testid="supply-payout-wallet-bind"
                      @click="bindPayoutWallet"
                    >
                      {{
                        bindingWallet
                          ? t('supply.withdrawal.wallet.binding')
                          : t('supply.withdrawal.wallet.bind')
                      }}
                    </button>
                    <button
                      v-if="boundPayoutWallet"
                      class="btn btn-secondary"
                      type="button"
                      @click="editingWallet = false"
                    >
                      {{ t('supply.withdrawal.wallet.cancelEdit') }}
                    </button>
                  </div>
                  <!-- 这句话说的是后果，不是"请核对"。链上转账不可逆，而
                       这一刻是整条链路上最后一个还没有钱牵扯进来的时刻。 -->
                  <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
                    {{ t('supply.withdrawal.wallet.hint') }}
                  </p>
                </div>
              </div>

              <div class="sm:col-span-2">
                <label class="input-label" for="withdrawal-note">{{ t('supply.withdrawal.noteLabel') }}</label>
                <input
                  id="withdrawal-note"
                  v-model="withdrawalForm.user_note"
                  class="input"
                  type="text"
                  :placeholder="t('supply.withdrawal.notePlaceholder')"
                />
              </div>
            </div>

            <button
              class="btn btn-primary mt-4"
              :disabled="submittingWithdrawal"
              data-testid="supply-withdrawal-submit"
              @click="submitWithdrawal"
            >
              <Icon name="dollar" size="sm" />
              <span>
                {{ submittingWithdrawal ? t('supply.withdrawal.submitting') : t('supply.withdrawal.submit') }}
              </span>
            </button>
          </template>

          <!-- 单据列表。即使一条都没有也留着表头之外的那句空文案，
               因为"我上次申请过没有"是这一块最常被问的问题。 -->
          <div class="mt-6 border-t border-gray-100 pt-4 dark:border-dark-800">
            <p v-if="withdrawals.length === 0" class="text-sm text-gray-500 dark:text-dark-400">
              {{ t('supply.withdrawal.empty') }}
            </p>
            <div v-else class="overflow-x-auto">
              <table class="min-w-full text-sm">
                <thead>
                  <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-dark-700 dark:text-dark-400">
                    <th class="px-3 py-2">{{ t('supply.withdrawal.createdAt') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.withdrawal.amount') }}</th>
                    <th class="px-3 py-2">{{ t('supply.withdrawal.channel') }}</th>
                    <th class="px-3 py-2">{{ t('supply.withdrawal.status') }}</th>
                    <th class="px-3 py-2">{{ t('supply.withdrawal.reviewNote') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.withdrawal.actions') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                  <tr v-for="item in withdrawals" :key="item.id" :data-testid="`supply-withdrawal-${item.id}`">
                    <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(item.created_at) }}</td>
                    <td class="px-3 py-3 text-right font-medium text-gray-900 dark:text-white">
                      {{ formatCurrency(item.amount) }}
                      <!-- 有手续费的单子必须把「扣了多少、到手多少」写在他看得见的地方：
                           申请 100 到账 99.7，那 0.3 只存在于数据库里的话，
                           对他就是一笔凭空少掉的钱。 -->
                      <p
                        v-if="item.fee_amount > 0"
                        class="text-xs font-normal text-gray-400 dark:text-dark-500"
                        :data-testid="`supply-withdrawal-fee-${item.id}`"
                      >
                        {{
                          t('supply.withdrawal.fee.line', {
                            fee: formatCurrency(item.fee_amount),
                            net: formatCurrency(item.net_amount),
                          })
                        }}
                      </p>
                    </td>
                    <td class="px-3 py-3 text-gray-700 dark:text-gray-300">
                      <p>{{ item.payout_channel }}</p>
                      <p class="text-xs text-gray-400 dark:text-dark-500">{{ item.payout_account }}</p>
                      <!-- network 非空 = worker 自动打款。这一行决定他对时效的预期：
                           自动的是几分钟，人工的是工作日——说错了他会在第一个小时就来问。 -->
                      <p v-if="item.network" class="text-xs text-emerald-600 dark:text-emerald-400">
                        {{ t('supply.withdrawal.fee.auto', { network: item.network }) }}
                      </p>
                    </td>
                    <td class="px-3 py-3">
                      <span
                        class="rounded-full px-2 py-0.5 text-xs font-medium"
                        :class="withdrawalBadgeClass(item.status)"
                      >
                        {{ withdrawalStatusLabel(item.status) }}
                      </span>
                    </td>
                    <td class="px-3 py-3 text-xs text-gray-500 dark:text-dark-400">
                      <!-- 被拒时 review_note 是他唯一能拿到的解释，
                           打款后 external_ref 是他对账的锚点。两个都要显示。 -->
                      <p v-if="item.review_note" class="max-w-xs">{{ item.review_note }}</p>
                      <p v-if="item.external_ref" class="max-w-xs">
                        {{ t('supply.withdrawal.externalRef', { ref: item.external_ref }) }}
                      </p>
                      <span v-if="!item.review_note && !item.external_ref">-</span>
                    </td>
                    <td class="px-3 py-3 text-right">
                      <!-- 撤回只对 pending 在场。终态单子上放一个灰按钮，
                           只会让人反复去点它。 -->
                      <button
                        v-if="item.status === 'pending'"
                        class="btn btn-secondary btn-sm"
                        :disabled="cancellingWithdrawalId === item.id"
                        :data-testid="`supply-withdrawal-cancel-${item.id}`"
                        @click="cancelWithdrawal(item)"
                      >
                        {{
                          cancellingWithdrawalId === item.id
                            ? t('supply.withdrawal.cancelling')
                            : t('supply.withdrawal.cancel')
                        }}
                      </button>
                      <span v-else class="text-xs text-gray-400 dark:text-dark-500">
                        {{ item.resolved_at ? formatDateTime(item.resolved_at) : '-' }}
                      </span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>

        <!-- ===================== 流水 ===================== -->
        <div class="card p-6" data-testid="supply-ledger-card">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('supply.ledger.title') }}</h3>

          <p v-if="ledger.length === 0" class="mt-4 text-sm text-gray-500 dark:text-dark-400">
            {{ t('supply.ledger.empty') }}
          </p>

          <template v-else>
            <div class="mt-4 overflow-x-auto">
              <table class="min-w-full text-sm">
                <thead>
                  <tr class="border-b border-gray-200 text-left text-xs uppercase text-gray-500 dark:border-dark-700 dark:text-dark-400">
                    <th class="px-3 py-2">{{ t('supply.ledger.time') }}</th>
                    <th class="px-3 py-2">{{ t('supply.ledger.action') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.ledger.amount') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.ledger.basis') }}</th>
                    <th class="px-3 py-2 text-right">{{ t('supply.ledger.ratio') }}</th>
                    <th class="px-3 py-2">{{ t('supply.ledger.frozenUntil') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
                  <tr v-for="entry in ledger" :key="entry.id">
                    <td class="px-3 py-3 text-gray-500 dark:text-dark-400">{{ formatDateTime(entry.created_at) }}</td>
                    <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ actionLabel(entry.action) }}</td>
                    <td class="px-3 py-3 text-right font-medium" :class="entry.amount >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-500'">
                      {{ formatCurrency(entry.amount) }}
                    </td>
                    <td class="px-3 py-3 text-right text-gray-500 dark:text-dark-400">
                      {{ entry.basis_amount === undefined ? '-' : formatCurrency(entry.basis_amount) }}
                    </td>
                    <td class="px-3 py-3 text-right text-gray-500 dark:text-dark-400">
                      {{ entry.share_ratio === undefined ? '-' : `${Math.round(entry.share_ratio * 1000) / 10}%` }}
                    </td>
                    <td class="px-3 py-3 text-gray-500 dark:text-dark-400">
                      {{ entry.frozen_until ? formatDateTime(entry.frozen_until) : '-' }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div class="mt-4 flex items-center justify-between">
              <p class="text-xs text-gray-400 dark:text-dark-500">
                {{ t('supply.ledger.pageInfo', { page: ledgerPage, pages: ledgerPages, total: ledgerTotal }) }}
              </p>
              <div class="flex gap-2">
                <button class="btn btn-secondary btn-sm" :disabled="ledgerPage <= 1" @click="goLedgerPage(ledgerPage - 1)">
                  {{ t('supply.ledger.prev') }}
                </button>
                <button class="btn btn-secondary btn-sm" :disabled="ledgerPage >= ledgerPages" @click="goLedgerPage(ledgerPage + 1)">
                  {{ t('supply.ledger.next') }}
                </button>
              </div>
            </div>
          </template>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { supplyAPI, type SupplyAccount, type SupplyAgreement, type SupplyLedgerEntry, type SupplyOnchainChannel, type SupplyPauseMode, type SupplyPayoutWallet, type SupplyStatus, type SupplyWallet, type SupplyWithdrawal, type SupplyWithdrawalOptions, type StartOAuthResponse } from '@/api/supply'
import { useAppStore } from '@/stores/app'
import { useSupplyStore } from '@/stores/supply'
import { SUPPLY_GUIDE_STEPS } from '@/constants/supplyGuide'
import { useClipboard } from '@/composables/useClipboard'
import { formatCurrency, formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const supplyStore = useSupplyStore()
const { copyToClipboard } = useClipboard()

// 与控制台那张精简引导共用同一张步骤表：编号和文案的对应关系只存一处，
// 否则两处会在某次改动后开始讲不一样的流程。
const guideSteps = SUPPLY_GUIDE_STEPS

const LEDGER_PAGE_SIZE = 20
// 提现单不分页：一个人的单子就那么几张，翻页控件比它要翻的内容还大。
// 真有人攒到超过这个数，看到的是最近 20 张——倒序，最该被关心的那张在最上面。
const WITHDRAWAL_PAGE_SIZE = 20

const loading = ref(true)
const refreshing = ref(false)
const starting = ref(false)
const submitting = ref(false)
const mutatingId = ref<number | null>(null)

// ---- 每日共享上限 ----
// 展开中的那一行；null = 没有任何行在编辑。一次只允许开一个，与 mutatingId 同理。
const capEditingId = ref<number | null>(null)
// 输入框存字符串：type=number 的空值是空串，转成 0 会把「没填」变成「设成 0」，
// 而 0 的语义是「取消上限」——两者相反。
const capForm = reactive<{ cost: string | number; tokens: string | number }>({ cost: '', tokens: '' })

/** 这个号设过任一种上限没有。与后端 HasSupplyDailyCap 同一判据。 */
function hasDailyCap(account: SupplyAccount): boolean {
  return (account.daily_cost_limit_usd ?? 0) > 0 || (account.daily_token_limit ?? 0) > 0
}

function formatUsd(value: number): string {
  return `$${value.toFixed(2)}`
}

/** 大数字缩写成 12.3K / 4.5M，表格里放得下。 */
function formatTokens(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return String(value)
}

function toggleCapEditor(account: SupplyAccount): void {
  if (capEditingId.value === account.id) {
    capEditingId.value = null
    return
  }
  capEditingId.value = account.id
  // 用当前值预填，0（不限）显示成空——让「不限」在输入框里就是「什么都没填」。
  capForm.cost = (account.daily_cost_limit_usd ?? 0) > 0 ? String(account.daily_cost_limit_usd) : ''
  capForm.tokens = (account.daily_token_limit ?? 0) > 0 ? String(account.daily_token_limit) : ''
}

/**
 * 空值 → 0（清除上限）；非法输入 → null（不发这一项，服务端也就不改它）。
 * 把非法输入当 0 会把一次手滑变成一次静默的「取消上限」。
 *
 * 入参按 unknown 收：`<input type="number">` 上的 v-model 给回来的可能是 number
 * 也可能是空串，直接当 string 用会在 .trim() 上炸掉。
 */
function parseCapInput(raw: unknown): number | null {
  if (raw === '' || raw === null || raw === undefined) return 0
  const parsed = typeof raw === 'number' ? raw : Number(String(raw).trim())
  if (!Number.isFinite(parsed) || parsed < 0) return null
  return parsed
}

async function saveDailyCap(account: SupplyAccount): Promise<void> {
  const cost = parseCapInput(capForm.cost)
  const tokens = parseCapInput(capForm.tokens)
  if (cost === null || tokens === null) {
    appStore.showError(t('supply.error.dailyCapInvalid'))
    return
  }
  mutatingId.value = account.id
  try {
    // 从**响应**回填，不用本地输入值：服务端会夹取值区间、把金额截到分，
    // 显示他填的那个数会让界面和实际生效的值对不上。
    replaceAccount(await supplyAPI.setDailyCap(account.id, {
      daily_cost_limit_usd: cost,
      daily_token_limit: tokens,
    }))
    capEditingId.value = null
    appStore.showSuccess(t('supply.accounts.dailyCapSaved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.dailyCapFailed')))
  } finally {
    mutatingId.value = null
  }
}

const status = ref<SupplyStatus>({ enabled: false, settlement_enabled: false })

/**
 * 分成比例的显示文本，如 "80%"；后端没给（或给了脏值）时为 null → 界面不显示。
 *
 * 存在的理由是引导卡里那句「比例见下方接入卡」：在补上这个之前，那句话指向的是
 * 一个空位——接入卡上从来没有出现过比例，它只出现在流水表格里，也就是只有
 * **已经赚到过钱的人**才看得见。而需要这个数来做决定的，恰恰是还一个号都没挂的人。
 *
 * 取整到整数百分比：0.8 → "80%"。运营不会配 0.805 这种值，多出来的小数位
 * 只会让这个数看起来像浮点误差。
 */
const shareRatioText = computed<string | null>(() => {
  const ratio = status.value.share_ratio
  if (typeof ratio !== 'number' || ratio <= 0 || ratio > 1) return null
  return `${Math.round(ratio * 100)}%`
})

const wallet = ref<SupplyWallet | null>(null)
const accounts = ref<SupplyAccount[]>([])
const ledger = ref<SupplyLedgerEntry[]>([])
const ledgerPage = ref(1)
const ledgerPages = ref(1)
const ledgerTotal = ref(0)

const accepting = ref(false)
const agreementChecked = ref(false)
// 初值是「未发布」而不是「已同意」：还没拉到协议之前，接入按钮不该先亮出来。
const agreement = ref<SupplyAgreement>({ version: '', published: false, accepted: false })

const pendingAuth = ref<StartOAuthResponse | null>(null)

// 中转接入（M7）。key 是 password 框、提交成功即清空——凭证不多留一秒。
const submittingRelay = ref(false)
const relayForm = ref({ base_url: '', api_key: '', name: '' })
const authCode = ref('')
const accountName = ref('')

// 初值是 null 而不是一份「关着」的默认值：两者在界面上是不同的东西——
// null 表示还没问过（那块什么都不画），一份 available=false 的 options 表示
// 问过了、答案是没开（那块要显示为什么）。
const withdrawalOptions = ref<SupplyWithdrawalOptions | null>(null)
const withdrawals = ref<SupplyWithdrawal[]>([])
const submittingWithdrawal = ref(false)
const cancellingWithdrawalId = ref<number | null>(null)
// amount 声明成 string | number，因为它运行时**两种都会是**：<input type="number">
// 上的 v-model 会把填进去的东西转成 number（Vue 对 type=number 自动套 .number），
// 而清空时留下的是空串。初值取 '' 是为了让「还没填」和「填了 0」在界面上分得开。
// 读它的地方一律先 String(...) 再判，别信这里的初值。
const withdrawalForm = ref<{
  amount: string | number
  payout_channel: string
  payout_account: string
  user_note: string
}>({ amount: '', payout_channel: '', payout_account: '', user_note: '' })

// 链上收款地址绑定。
//
// 只存 wallets，不存绑定接口一起返回的那份 channels：「选中的渠道是不是链上渠道」
// 必须与「有哪些渠道能选」来自同一个响应（withdrawalOptions），否则两个接口之间
// 的时间差会画出「渠道列表里没有 BSC-USDT、下面却在催你绑 bsc 地址」的界面。
const payoutWallets = ref<SupplyPayoutWallet[]>([])
const bindingWallet = ref(false)
const unbindingWallet = ref(false)
// 已绑定时输入框默认收起来。地址是一个看一眼就够的东西，让它一直摊在
// 可编辑的输入框里，只会增加一次误改的机会——而误改在这里是不可逆的。
const editingWallet = ref(false)
const payoutAddressInput = ref('')

/** 当前选中的渠道是不是链上渠道；不是则返回 null（走人工那条路径）。 */
const selectedOnchainChannel = computed<SupplyOnchainChannel | null>(() => {
  const channel = withdrawalForm.value.payout_channel
  if (!channel) return null
  return withdrawalOptions.value?.onchain_channels?.find((item) => item.channel === channel) ?? null
})

/** 选中渠道那条链上已绑定的地址。没绑（或不是链上渠道）返回 null。 */
const boundPayoutWallet = computed<SupplyPayoutWallet | null>(() => {
  const network = selectedOnchainChannel.value?.network
  if (!network) return null
  return payoutWallets.value.find((item) => item.network === network) ?? null
})

function stateLabel(state: string): string {
  const known = ['pending_review', 'active', 'draining', 'retired']
  return known.includes(state) ? t(`supply.state.${state}`) : t('supply.state.unknown')
}

function stateBadgeClass(state: string): string {
  switch (state) {
    case 'active':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'retired':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
    case 'draining':
      // 排空中：已经不接新单了，但还没到终态，也还能反悔——用橙色而不是灰色，
      // 灰色会读成"已经结束了"。
      return 'bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-300'
    default:
      // pending_review 及任何未知状态都按"还没在接单"呈现——这是保守的那一边。
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
}

function actionLabel(action: string): string {
  // withdraw_revert 必须在这张表里：它是流水上唯一一条"钱回来了"的记录，
  // 落到 unknown（"其他"）会让一次退款在账单上读起来像一笔来路不明的收入。
  const known = ['accrue', 'spend', 'thaw', 'clawback', 'withdraw', 'withdraw_revert']
  return known.includes(action) ? t(`supply.action.${action}`) : t('supply.action.unknown')
}

function withdrawalStatusLabel(status: string): string {
  const known = ['pending', 'processing', 'failed', 'paid', 'rejected', 'canceled']
  return known.includes(status) ? t(`supply.withdrawal.state.${status}`) : t('supply.withdrawal.state.unknown')
}

function withdrawalBadgeClass(status: string): string {
  switch (status) {
    case 'paid':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'processing':
      // 蓝色 = "系统正在动"，与琥珀色的"等着被处理"分开：
      // 打款中的单子没有撤回按钮，颜色得先把"为什么点不了"说了。
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
    case 'rejected':
      // 被拒是要他做点什么（多半是改收款账号），用红色而不是灰色。
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'canceled':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
    default:
      // pending 及任何未知状态都按"还挂着"呈现——保守的那一边。
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  }
}

async function loadStatus(): Promise<void> {
  try {
    status.value = await supplyAPI.getStatus()
  } catch (error) {
    // 状态读不到就按"未开放"渲染，而不是把整页错误抛给用户：
    // 后面所有请求都会失败，弹一堆 toast 帮不上任何忙。
    status.value = { enabled: false, settlement_enabled: false }
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadWallet(): Promise<void> {
  try {
    wallet.value = await supplyAPI.getWallet()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadAccounts(): Promise<void> {
  try {
    accounts.value = await supplyAPI.listAccounts()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadLedger(page = ledgerPage.value): Promise<void> {
  try {
    const result = await supplyAPI.listLedger({ page, page_size: LEDGER_PAGE_SIZE })
    ledger.value = result?.items ?? []
    ledgerPage.value = result?.page ?? page
    ledgerPages.value = Math.max(result?.pages ?? 1, 1)
    ledgerTotal.value = result?.total ?? 0
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function goLedgerPage(page: number): Promise<void> {
  if (page < 1 || page > ledgerPages.value) return
  await loadLedger(page)
}

async function loadAgreement(): Promise<void> {
  try {
    agreement.value = await supplyAPI.getAgreement()
  } catch (error) {
    // 读不到就按"平台尚未发布"渲染——与服务端 fail-closed 同向。把接入按钮
    // 留着更糟：他会一路走到最后一步才被拒，而那时授权码已经生成了。
    agreement.value = { version: '', published: false, accepted: false }
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadWithdrawals(): Promise<void> {
  try {
    const [options, page] = await Promise.all([
      supplyAPI.getWithdrawalOptions(),
      supplyAPI.listWithdrawals({ page: 1, page_size: WITHDRAWAL_PAGE_SIZE }),
    ])
    withdrawalOptions.value = options
    withdrawals.value = page?.items ?? []
    // 渠道只剩一个时替他选上。让人在一个只有一项的下拉框里点一下，
    // 是纯粹的仪式——但仍然要保留他改的余地，所以只在还没选时填。
    if (!withdrawalForm.value.payout_channel && options.channels.length === 1) {
      withdrawalForm.value.payout_channel = options.channels[0]
    }
    // 只在这个部署真的有链上渠道时才去问绑定。没有链上渠道的部署（默认就是）
    // 一次也不该打这个接口——否则一个没装配起来的绑定服务会让每次进页面
    // 都弹一条与他无关的错误。代价是多一趟往返，只发生在用得上的部署里。
    if ((options.onchain_channels?.length ?? 0) > 0) {
      await loadPayoutWallets()
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

async function loadPayoutWallets(): Promise<void> {
  try {
    const options = await supplyAPI.getPayoutWallets()
    payoutWallets.value = options?.wallets ?? []
  } catch (error) {
    // 读失败时保持空数组——界面会显示"还没绑定"，而那句话此刻是**不确定**的。
    // 所以错误必须弹出来：他至少要知道页面上这一块的信息不可信，
    // 而不是照着一句可能错的提示去重新绑一次。
    appStore.showError(extractApiErrorMessage(error, t('supply.error.loadFailed')))
  }
}

/**
 * 绑定/换绑当前渠道那条链上的收款地址。
 *
 * 地址**原样**提交，不做 trim 之外的任何加工——尤其不做 toLowerCase：
 * EIP-55 的校验和就编码在字母的大小写里，前端归一化一下，「你改过其中一位」
 * 这个唯一能挡住不可逆损失的信号就在到达后端之前消失了。
 */
async function bindPayoutWallet(): Promise<void> {
  const network = selectedOnchainChannel.value?.network
  if (!network) return

  const address = payoutAddressInput.value.trim()
  if (!address) {
    appStore.showError(t('supply.error.payoutAddressRequired'))
    return
  }

  bindingWallet.value = true
  try {
    const wallet = await supplyAPI.bindPayoutWallet(network, address)
    // 用返回值而不是手里这串输入更新本地状态：后端存的是小写形态，
    // 而界面上显示的必须是**库里那一个**——两者不一致时，人会以为自己绑的
    // 是屏幕上这个，下次比对交易记录时对不上。
    payoutWallets.value = [...payoutWallets.value.filter((item) => item.network !== network), wallet]
    payoutAddressInput.value = ''
    editingWallet.value = false
    appStore.showSuccess(t('supply.withdrawal.wallet.bindSuccess'))
  } catch (error) {
    // 地址被别人绑走（409）、格式错、校验和不符——后端每一种都有自己的文案，
    // 原样透出来。前端归成一句"绑定失败"会把"你粘少了几位"和"这个地址已经
    // 是别人的"混成同一件事，而它们要做的动作完全不同。
    appStore.showError(extractApiErrorMessage(error, t('supply.error.payoutBindFailed')))
  } finally {
    bindingWallet.value = false
  }
}

async function unbindPayoutWallet(): Promise<void> {
  const network = selectedOnchainChannel.value?.network
  if (!network) return
  if (!window.confirm(t('supply.withdrawal.wallet.unbindConfirm', { network }))) return

  unbindingWallet.value = true
  try {
    await supplyAPI.unbindPayoutWallet(network)
    payoutWallets.value = payoutWallets.value.filter((item) => item.network !== network)
    editingWallet.value = false
    appStore.showSuccess(t('supply.withdrawal.wallet.unbindSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.payoutUnbindFailed')))
  } finally {
    unbindingWallet.value = false
  }
}

/** 展开换绑输入框。预填当前地址只会让人在上面改字，而改字正是要防的那件事。 */
function startEditingWallet(): void {
  payoutAddressInput.value = ''
  editingWallet.value = true
}

/** 并发拉五份数据。任一份失败只影响它自己那一块，不该让整页空着。 */
async function loadAll(): Promise<void> {
  await Promise.all([loadAgreement(), loadWallet(), loadAccounts(), loadLedger(1), loadWithdrawals()])
}

/**
 * 把可用余额填进金额框。
 *
 * 不做成"全部提现"直接提交：这个数是**读这一刻**的快照，中间他自己发起的请求
 * 会把余额扣下去，提交时再撞一个"余额不足"。填进输入框让他自己按一下提交，
 * 那一步之间的时间差就成了他的选择，而不是一个看起来像 bug 的失败。
 */
function fillMaxAmount(): void {
  if (!withdrawalOptions.value) return
  withdrawalForm.value.amount = String(withdrawalOptions.value.available_credit)
}

/**
 * 提交申请。
 *
 * 三道本地校验只挡"一定错"的输入（空、非数、非正）。起提额、余额、未决单上限
 * 全部交给后端——那三个判据在提交的这一刻可能已经和页面上显示的不一样了，
 * 前端拦一遍只是把同一句话说两遍，还多一份会过期的规则。
 */
async function submitWithdrawal(): Promise<void> {
  // 先转成字符串再判空。amount 的**运行时**类型是 string | number，尽管初值是 ''：
  // Vue 的 vModelText 对 <input type="number"> 会自动套上 .number 的转换
  // （castToNumber = number || props.type === 'number'），所以填了数字之后
  // 这里拿到的是 number，清空时才留下空串。
  //
  // 早先这行写的是 `withdrawalForm.value.amount.trim()`——它在**任何**填了金额的
  // 提交上都抛 TypeError，也就是提现按钮实际上一次也按不动；而 TS 看不见这件事，
  // 因为 v-model 的写入绕过了 ref 的类型。
  const raw = String(withdrawalForm.value.amount).trim()
  const amount = Number(raw)
  if (!raw || Number.isNaN(amount) || amount <= 0) {
    appStore.showError(t('supply.error.withdrawalAmountInvalid'))
    return
  }
  if (!withdrawalForm.value.payout_channel) {
    appStore.showError(t('supply.error.withdrawalChannelRequired'))
    return
  }

  // 收款账号只来自绑定（M6b 起提现只剩链上渠道），**不**来自任何输入框。
  // 后端会用同一条规则覆盖手填内容（ResolvePayoutAddress），但等它兜底时
  // 单子已经建了一半——前端必须让这条路径根本不存在。
  const payoutAccount = boundPayoutWallet.value?.address ?? ''
  if (!payoutAccount) {
    appStore.showError(t('supply.error.payoutWalletRequired'))
    return
  }

  submittingWithdrawal.value = true
  try {
    await supplyAPI.requestWithdrawal({
      amount,
      payout_channel: withdrawalForm.value.payout_channel,
      payout_account: payoutAccount,
      user_note: withdrawalForm.value.user_note.trim() || undefined,
    })
    appStore.showSuccess(t('supply.withdrawal.submitted'))
    withdrawalForm.value.amount = ''
    withdrawalForm.value.user_note = ''
    // 钱在这一刻已经出了可用区，钱包和流水都必须跟着重拉——
    // 只刷单据列表会让余额停在旧数上，看起来像申请没生效。
    await Promise.all([loadWithdrawals(), loadWallet(), loadLedger(1)])
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.withdrawalFailed')))
  } finally {
    submittingWithdrawal.value = false
  }
}

/** 撤回。钱退回可用区，所以和提交一样要连钱包和流水一起刷。 */
async function cancelWithdrawal(item: SupplyWithdrawal): Promise<void> {
  if (!window.confirm(t('supply.withdrawal.cancelConfirm', { amount: formatCurrency(item.amount) }))) return
  cancellingWithdrawalId.value = item.id
  try {
    await supplyAPI.cancelWithdrawal(item.id)
    appStore.showSuccess(t('supply.withdrawal.cancelled'))
    await Promise.all([loadWithdrawals(), loadWallet(), loadLedger(1)])
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.withdrawalCancelFailed')))
  } finally {
    cancellingWithdrawalId.value = null
  }
}

/**
 * 同意当前版本。
 *
 * 回传的是页面上正在显示的那个版本号，不是让服务端自己取当前版本：页面开了很久
 * 的人点的是旧版正文。服务端这时会回 VERSION_MISMATCH，界面该做的是重新拉一次
 * 协议让他读新的那一份，而不是原样重试——所以失败分支里必然跟着一次 loadAgreement。
 */
async function acceptAgreement(): Promise<void> {
  if (!agreement.value.published || !agreementChecked.value) return
  accepting.value = true
  try {
    agreement.value = await supplyAPI.acceptAgreement(agreement.value.version)
    agreementChecked.value = false
    appStore.showSuccess(t('supply.agreement.acceptedToast'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.acceptFailed')))
    agreementChecked.value = false
    await loadAgreement()
  } finally {
    accepting.value = false
  }
}

async function refreshAll(): Promise<void> {
  refreshing.value = true
  try {
    await loadAll()
  } finally {
    refreshing.value = false
  }
}

async function submitRelay(): Promise<void> {
  const baseURL = relayForm.value.base_url.trim()
  const apiKey = relayForm.value.api_key.trim()
  if (!baseURL || !apiKey) {
    appStore.showError(t('supply.relay.fieldsRequired'))
    return
  }
  submittingRelay.value = true
  try {
    await supplyAPI.submitRelayAccount({
      base_url: baseURL,
      api_key: apiKey,
      name: relayForm.value.name.trim() || undefined,
    })
    appStore.showSuccess(t('supply.relay.submitted'))
    relayForm.value = { base_url: '', api_key: '', name: '' }
    accounts.value = await supplyAPI.listAccounts()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.relay.failed')))
  } finally {
    submittingRelay.value = false
  }
}

async function startOAuth(): Promise<void> {
  starting.value = true
  try {
    pendingAuth.value = await supplyAPI.startOAuth()
    authCode.value = ''
    accountName.value = ''
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.startFailed')))
  } finally {
    starting.value = false
  }
}

async function copyAuthUrl(): Promise<void> {
  if (!pendingAuth.value) return
  await copyToClipboard(pendingAuth.value.auth_url, t('supply.connect.authUrlCopied'))
}

function cancelAuth(): void {
  // 只丢掉本地这份 session_id。服务端那行会话留着自然过期（15 分钟），
  // 不发"作废"请求：一个已经没人持有 id 的一次性会话，删不删都一样。
  pendingAuth.value = null
  authCode.value = ''
  accountName.value = ''
}

async function completeOAuth(): Promise<void> {
  if (!pendingAuth.value) return
  const code = authCode.value.trim()
  if (!code) {
    appStore.showError(t('supply.error.codeRequired'))
    return
  }

  submitting.value = true
  try {
    await supplyAPI.completeOAuth({
      session_id: pendingAuth.value.session_id,
      code,
      name: accountName.value.trim() || undefined,
    })
    appStore.showSuccess(t('supply.connect.success'))
    cancelAuth()
    await Promise.all([loadAccounts(), loadWallet()])
  } catch (error) {
    // 失败时保留 pendingAuth：会话可能还没被消费（比如授权码贴错了），
    // 清掉的话用户就得从第一步重来。
    appStore.showError(extractApiErrorMessage(error, t('supply.error.completeFailed')))
  } finally {
    submitting.value = false
  }
}

function replaceAccount(updated: SupplyAccount): void {
  accounts.value = accounts.value.map(item => (item.id === updated.id ? updated : item))
}

/**
 * 下线。两条通道用**不同的确认文案**：immediate 不可撤销，graceful 可以，
 * 共用一句"确定要下线吗"会把这个差别抹掉。
 */
async function pauseAccount(account: SupplyAccount, mode: SupplyPauseMode): Promise<void> {
  const confirmKey = mode === 'immediate' ? 'supply.accounts.pauseNowConfirm' : 'supply.accounts.pauseConfirm'
  if (!window.confirm(t(confirmKey))) return
  mutatingId.value = account.id
  try {
    const updated = await supplyAPI.pauseAccount(account.id, mode)
    replaceAccount(updated)
    // 用返回的实际状态而不是请求的 mode 来选提示：排空窗配成 0 时，
    // graceful 会直接落到 retired，这时说"排空中"就是在骗人。
    appStore.showSuccess(
      updated.supply_state === 'draining' ? t('supply.accounts.draining') : t('supply.accounts.paused')
    )
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.pauseFailed')))
  } finally {
    mutatingId.value = null
  }
}

async function resumeAccount(account: SupplyAccount): Promise<void> {
  const wasDraining = account.supply_state === 'draining'
  mutatingId.value = account.id
  try {
    replaceAccount(await supplyAPI.resumeAccount(account.id))
    // 取消排空是"什么都没发生"，重新挂回是"要重走观察期"——两件事，两句话。
    appStore.showSuccess(wasDraining ? t('supply.accounts.pauseCancelled') : t('supply.accounts.resumed'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.resumeFailed')))
  } finally {
    mutatingId.value = null
  }
}

/**
 * 解绑。与下线是两件事，确认文案必须把差别摆出来：
 * 下线只是不再派单，凭证还在平台手里；解绑才是把凭证删掉，而且不可撤销。
 *
 * 成功之后额外弹一条 warning：平台这边的凭证没了，但上游那边的授权记录还在，
 * 只有供给者自己能去清。不说这句话，他会以为解绑等于上游也撤销了。
 */
async function detachAccount(account: SupplyAccount): Promise<void> {
  if (!window.confirm(t('supply.accounts.detachConfirm', { name: account.name }))) return
  mutatingId.value = account.id
  try {
    const result = await supplyAPI.detachAccount(account.id)
    // 号已经被摘掉了，不能像 pause/resume 那样替换——它不在列表里了。
    accounts.value = accounts.value.filter(item => item.id !== account.id)
    appStore.showSuccess(t('supply.accounts.detached'))
    if (result.upstream_revoke_required) {
      appStore.showWarning(t('supply.accounts.detachUpstreamHint'), 10000)
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('supply.error.detachFailed')))
  } finally {
    mutatingId.value = null
  }
}

onMounted(async () => {
  try {
    await loadStatus()
    // 让侧边栏与本页共用同一份判断：用户从这里进来时顺手把 store 也刷新一次。
    supplyStore.enabled = status.value.enabled
    supplyStore.settlementEnabled = status.value.settlement_enabled
    // 账号数一起带过去：一个人接入第一个号之后，控制台该自动变成共享模式，
    // 而这一页正是他接入的地方——不同步的话他要刷新整个应用才看得到。
    supplyStore.accountCount = status.value.account_count ?? 0
    // 走 shareRatioText 的判定而不是直接透传：这里绕过了 store 自己的清洗，
    // 不加这道门槛的话一个脏值会从这条路径漏进去，两个入口对同一个数得出不同结论。
    supplyStore.shareRatio = shareRatioText.value ? (status.value.share_ratio ?? 0) : 0
    supplyStore.loaded = true
    if (status.value.enabled) {
      await loadAll()
    }
  } finally {
    loading.value = false
  }
})
</script>
