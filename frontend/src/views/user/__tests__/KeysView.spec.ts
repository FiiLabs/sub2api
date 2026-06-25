/**
 * KeysView 成员归属列测试
 *
 * 覆盖两个场景：
 * (a) 团队工作区 + team.keys.manage.all → 渲染成员列 / 按 created_by_user_id 显示成员邮箱
 * (b) 普通成员（团队工作区，只有 team.keys.manage）→ 不渲染成员列
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import KeysView from '../KeysView.vue'
import { useWorkspaceStore } from '@/stores/workspaces'

// ==================== Hoist mocked functions ====================

const { keysList, usageDashboard, listMembers, groupsGetAvailable, getUserGroupRates } = vi.hoisted(() => ({
  keysList: vi.fn(),
  usageDashboard: vi.fn(),
  listMembers: vi.fn(),
  groupsGetAvailable: vi.fn(),
  getUserGroupRates: vi.fn(),
}))

// ==================== Module mocks ====================

vi.mock('@/api', () => ({
  keysAPI: {
    list: keysList,
    toggleStatus: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    create: vi.fn(),
  },
  authAPI: {
    getPublicSettings: vi.fn().mockResolvedValue({
      api_base_url: '',
      hide_ccs_import_button: false,
      site_name: 'Test',
      custom_endpoints: [],
    }),
  },
  usageAPI: {
    getDashboardApiKeysUsage: usageDashboard,
  },
  userGroupsAPI: {
    getAvailable: groupsGetAvailable,
    getUserGroupRates: getUserGroupRates,
  },
}))

vi.mock('@/api/workspaces', () => ({
  workspacesAPI: {
    listMembers,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn().mockReturnValue(false),
    nextStep: vi.fn(),
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true),
    copied: { value: false },
  }),
}))

vi.mock('@/composables/usePersistedPageSize', () => ({
  getPersistedPageSize: () => 20,
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      // 返回 key 本身方便断言
      t: (key: string, _params?: unknown) => key,
    }),
  }
})

// ==================== Stubs ====================

/**
 * DataTable stub：渲染所有列 slot，让测试可断言列头和单元格内容。
 * columns prop 用于渲染列头；data prop 用于渲染行。
 */
const DataTableStub = {
  props: ['columns', 'data', 'loading', 'serverSideSort', 'defaultSortKey', 'defaultSortOrder'],
  emits: ['sort'],
  template: `
    <div>
      <div class="col-headers">
        <span v-for="col in columns" :key="col.key" :class="'col-header-' + col.key">{{ col.label }}</span>
      </div>
      <div v-for="row in data" :key="row.id" class="data-row">
        <slot v-for="col in columns" :name="'cell-' + col.key" :row="row" :value="row[col.key]" />
      </div>
    </div>
  `,
}

const globalStubs = {
  AppLayout: { template: '<div><slot /></div>' },
  TablePageLayout: {
    template: '<div><slot name="filters" /><slot name="actions" /><slot name="table" /><slot name="pagination" /></div>',
  },
  Pagination: true,
  EmptyState: true,
  BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
  ConfirmDialog: { props: ['show'], template: '<div v-if="show" />' },
  UseKeyModal: { props: ['show'], template: '<div v-if="show" />' },
  EndpointPopover: true,
  SearchInput: true,
  Select: true,
  Icon: true,
  GroupBadge: true,
  GroupOptionItem: true,
  Teleport: true,
  DataTable: DataTableStub,
}

// ==================== Default mock data ====================

const MOCK_KEY_WITH_CREATOR = {
  id: 101,
  user_id: 22,
  created_by_user_id: 22,
  key: 'sk-test-aaaa1111',
  name: 'Member Key',
  group_id: null,
  status: 'active' as const,
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
}

const MOCK_MEMBER = {
  id: 5,
  team_id: 7,
  user_id: 22,
  role: 'developer' as const,
  status: 'active' as const,
  user: {
    id: 22,
    username: 'devuser',
    email: 'dev@team.example.com',
    role: 'user' as const,
    balance: 0,
    concurrency: 1,
    status: 'active' as const,
    allowed_groups: null,
    balance_notify_enabled: false,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  },
}

function setupDefaultMocks() {
  keysList.mockResolvedValue({ items: [MOCK_KEY_WITH_CREATOR], total: 1, page: 1, page_size: 20, pages: 1 })
  usageDashboard.mockResolvedValue({ stats: {} })
  listMembers.mockResolvedValue({ members: [MOCK_MEMBER], invitations: [] })
  groupsGetAvailable.mockResolvedValue([])
  getUserGroupRates.mockResolvedValue({})
}

// ==================== Test suites ====================

describe('KeysView 成员归属列', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    setupDefaultMocks()
  })

  it('(a) 团队工作区 + team.keys.manage.all → 渲染成员列并显示创建者邮箱', async () => {
    const store = useWorkspaceStore()
    store.workspaces = [
      {
        billing_subject_id: 2,
        type: 'team',
        team_id: 7,
        name: '测试团队',
        role: 'owner',
        permissions: {
          'team.keys.manage': true,
          'team.keys.manage.all': true,
        },
        balance: 100,
      },
    ]
    store.activeSubjectId = 2

    const wrapper = mount(KeysView, { global: { stubs: globalStubs } })
    await flushPromises()

    // listMembers 应该被调用（团队 id = 7）
    expect(listMembers).toHaveBeenCalledWith(7)

    // 列头应当包含 keys.creator（i18n key 透传）
    expect(wrapper.text()).toContain('keys.creator')

    // 单元格应当显示成员邮箱
    expect(wrapper.text()).toContain('dev@team.example.com')
  })

  it('(b) 普通成员（只有 team.keys.manage）→ 不渲染成员列，不调用 listMembers', async () => {
    const store = useWorkspaceStore()
    store.workspaces = [
      {
        billing_subject_id: 3,
        type: 'team',
        team_id: 7,
        name: '测试团队',
        role: 'developer',
        permissions: {
          'team.keys.manage': true,
          // 没有 team.keys.manage.all
        },
        balance: 0,
      },
    ]
    store.activeSubjectId = 3

    const wrapper = mount(KeysView, { global: { stubs: globalStubs } })
    await flushPromises()

    // listMembers 不应该被调用
    expect(listMembers).not.toHaveBeenCalled()

    // 不应当出现成员列
    expect(wrapper.text()).not.toContain('keys.creator')

    // 也不应当出现邮箱（成员数据未加载）
    expect(wrapper.text()).not.toContain('dev@team.example.com')
  })
})
