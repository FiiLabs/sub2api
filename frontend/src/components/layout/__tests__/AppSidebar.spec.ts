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
describe('AppSidebar two-sided navigation grouping', () => {
  it('keeps the supply entry out of the consumer nav list', () => {
    expect(componentSource).toContain("const SUPPLY_NAV_PATHS = new Set(['/supply'])")
    // 消费者那一组必须是**排除**供给路径后的结果，不是原样全量。
    expect(componentSource).toContain('!SUPPLY_NAV_PATHS.has(item.path)')
    expect(componentSource).toContain('SUPPLY_NAV_PATHS.has(item.path)')
  })

  it('renders the supply group as its own titled section', () => {
    expect(componentSource).toContain("t('supply.navSection')")
    expect(componentSource).toContain('sidebar-section-title')
  })

  it('hides the whole section when supply is off instead of showing an empty heading', () => {
    // 只有标题没有条目的分区比没有分区更糟：它看起来像功能坏了。
    expect(componentSource).toContain('v-if="supplyNavItems.length > 0"')
  })

  it('derives both groups from one build so the two lists cannot drift', () => {
    // 两个 computed 各调一次 buildSelfNavItems 的话，特性开关在两次调用之间
    // 变化就会让同一个条目同时出现在两组里、或者两组都没有。
    expect(componentSource).toContain('const selfNavItems = computed')
    expect(componentSource).toContain('selfNavItems.value.filter')
  })
})
