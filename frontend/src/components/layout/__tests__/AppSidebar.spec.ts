import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

// APEXONE-EXT: 双边市场——供给入口必须独立成组。
//
// 这组断言防的是一次"顺手的整理"：把 supplyNavItems 合回 userNavItems
// 只需要删两行，代码更短、页面看起来也正常，而丢掉的是「使用」与「赚钱」
// 这两种相反意图的区分——供给入口重新变回消费者菜单里的第 12 个图标。
describe('AppSidebar consumer/contributor separation', () => {
  it('keeps the supply entry out of the consumer nav list', () => {
    expect(componentSource).toContain("const SUPPLY_NAV_PATHS = new Set(['/supply'])")
    // 消费者那一组必须是**排除**供给路径后的结果，不是原样全量。
    expect(componentSource).toContain('!SUPPLY_NAV_PATHS.has(item.path)')
    expect(componentSource).toContain('SUPPLY_NAV_PATHS.has(item.path)')
  })

  it('renders only one section per mode — not both reordered', () => {
    // 完全区分：userNavSections 单选，共享模式只有 sharing，消费模式只有 consumer。
    expect(componentSource).toContain(
      "supplyStore.mode === 'sharing' ? ['sharing'] : ['consumer']"
    )
    expect(componentSource).toContain('v-for="section in userNavSections"')
    expect(componentSource).toContain(`v-if="section === 'consumer'"`)
    expect(componentSource).toContain(`v-else-if="section === 'sharing'"`)
  })

  it('sharing nav is a fixed flat list: dashboard, supply, profile', () => {
    const decl = componentSource.match(/const sharingNavItems = computed[\s\S]*?\n\}\)/)?.[0]
    expect(decl).toBeDefined()
    expect(decl).toContain("'/dashboard'")
    expect(decl).toContain('SUPPLY_NAV_PATHS')
    expect(decl).toContain("'/profile'")
  })

  it('surfaces the mode switch as the single seam, gated on canSwitchMode', () => {
    // 切换器是两套体验之间唯一的接缝——功能没开时不露出。
    expect(componentSource).toContain('ConsoleModeSwitch')
    expect(componentSource).toContain('supplyStore.canSwitchMode')
    expect(componentSource).toContain('@update:mode="supplyStore.setMode"')
  })

  it('derives every nav list from one build so they cannot drift', () => {
    // 各组都从同一份 selfNavItems 派生，避免同一条目在两次构造之间漂移。
    expect(componentSource).toContain('const selfNavItems = computed')
    expect(componentSource).toContain('selfNavItems.value.map')
  })
})

// 普通用户侧栏的折叠分组。
//
// 这组断言守的是四件很容易在后续改动里悄悄失效的事：子项的开关会不会被过滤到、
// 空分组会不会留下一个点开什么都没有的标题、付费入口有没有被"顺手"折起来、
// 分组标题会不会变成一个可点击跳转的死链。
const localesDir = resolve(dirname(fileURLToPath(import.meta.url)), '../../../i18n/locales')
const zhCommonSource = readFileSync(resolve(localesDir, 'zh/common.ts'), 'utf8')
const enCommonSource = readFileSync(resolve(localesDir, 'en/common.ts'), 'utf8')

describe('AppSidebar user nav grouping', () => {
  it('filters children recursively so per-child feature flags keep working', () => {
    // 最隐蔽的一处：只过滤顶层的话，关掉支付的站点仍然会在「我的账单」里
    // 看到「我的订单」——而且要展开分组才看得见，几乎不会被报上来。
    expect(componentSource).toContain('function filterSimpleMode(items: NavItem[]): NavItem[]')
    expect(componentSource).toContain('filterSimpleMode(item.children)')
    expect(componentSource).toContain('authStore.isSimpleMode ? filterSimpleMode(visible) : visible')
    // featureFlag 那一侧本来就是递归的，别把它退化成一层。
    expect(componentSource).toContain('children: applyFeatureFlags(item.children)')
  })

  it('drops a collapsible group entirely once its children are all filtered out', () => {
    // 简易模式下「我的账单」三项全被隐藏。留下一个空标题比不显示更糟。
    expect(componentSource).toContain('function dropEmptyGroups(items: NavItem[]): NavItem[]')
    expect(componentSource).toContain('items.filter((item) => !item.children || item.children.length > 0)')
    expect(componentSource).toContain('dropEmptyGroups(finalizeNav(groupUserNav(rawSelfNavItems.value)))')
  })

  it('keeps /purchase at the top level, never inside a collapsible group', () => {
    // 付费入口。折进「我的账单」只是让列表短一行，代价是多一次点击。
    expect(componentSource).toContain("const USER_NAV_TOP_PATHS = ['/dashboard', '/keys', '/usage', '/purchase', '/batch-image', '/affiliate']")
    expect(componentSource).toContain("const BILLING_GROUP_PATHS = ['/subscriptions', '/orders', '/redeem']")
    expect(componentSource).toContain("const SERVICE_GROUP_PATHS = ['/available-channels', '/monitor']")
    for (const group of [
      componentSource.match(/const BILLING_GROUP_PATHS = \[[^\]]*\]/)?.[0],
      componentSource.match(/const SERVICE_GROUP_PATHS = \[[^\]]*\]/)?.[0],
    ]) {
      expect(group).toBeDefined()
      expect(group).not.toContain('/purchase')
    }
  })

  it('makes the group headers expand-only instead of navigable routes', () => {
    // /billing 和 /service-status 没有对应路由，点标题跳转就是死链。
    expect(componentSource).toContain("const BILLING_GROUP_PATH = '/billing'")
    expect(componentSource).toContain("const SERVICE_GROUP_PATH = '/service-status'")
    const groupDecls = componentSource.match(/path: (?:BILLING_GROUP_PATH|SERVICE_GROUP_PATH),[\s\S]*?children:/g)
    expect(groupDecls).toHaveLength(2)
    for (const decl of groupDecls ?? []) {
      expect(decl).toContain('expandOnly: true')
    }
    // expandOnly 的分支必须只切换展开状态，不 router.push。
    expect(componentSource).toContain('if (item.expandOnly) {')
  })

  it('titles both groups from aligned zh/en keys', () => {
    expect(componentSource).toContain("t('nav.myBilling')")
    expect(componentSource).toContain("t('nav.serviceStatus')")
    for (const key of ['myBilling', 'serviceStatus']) {
      expect(zhCommonSource).toContain(`${key}: '`)
      expect(enCommonSource).toContain(`${key}: '`)
    }
  })

  it('does not silently drop nav items that are missing from the grouping tables', () => {
    // 自定义菜单项（运营在后台配的）不在分组表里；以后往 buildSelfNavItems 里加条目
    // 的人也未必会记得登记。落到第一组末尾，好过从侧栏里消失。
    expect(componentSource).toContain('if (!listed.has(item.path) && !SUPPLY_NAV_PATHS.has(item.path)) top.push(item)')
  })
})

// 收起态摊平回扁平列表。
//
// 这条护栏防的是"这段 if 看着多余"式的删除：分组在 72px 图标条上不产生任何收益
// （没地方放标题），却会让折叠组里的子项彻底够不到——handleGroupClick 在收起时
// 直接 return，children 的 v-if 也带着 !sidebarCollapsed。删掉这段，
// 「我的订单」「兑换」这些改版前点得到的图标会在收起状态下凭空消失。
describe('AppSidebar collapsed-mode reachability', () => {
  it('flattens groups when the sidebar is collapsed', () => {
    expect(componentSource).toContain('if (!sidebarCollapsed.value) return grouped')
    expect(componentSource).toContain('item.children?.length ? item.children : [item]')
  })

  it('still groups when the sidebar is expanded', () => {
    // 摊平只能发生在收起态：展开态摊平就等于这次重构白做。
    expect(componentSource).toContain('dropEmptyGroups(finalizeNav(groupUserNav(rawSelfNavItems.value)))')
  })
})
