/**
 * APEXONE-EXT: 首页两支视频的归属与语言。
 *
 * 守两件事：
 *
 * 1) **每支片子待在它说的那件事旁边**。原来只有一支通用片，单独挂在页面最后
 *    （id="why"），读者走完消费侧、价格、供给侧三段才撞见一个没有上下文的
 *    播放器。现在消费侧的片子在 #personal 里、共享者的片子在 #supply 里。
 *    这条断言的形式是「在哪个 section 内部」而不是「在第几个位置」——位置会随
 *    版式调整而变，归属不该变。
 *
 * 2) **共享者视频跟着界面语言换中英版本**。这条最容易坏在别处：`<video>` 不会
 *    因为 `<source>` 子节点变化就重新选源，光把 URL 算对、画面上仍是旧片子。
 *    所以这里既断言算出来的 src，也断言切语言之后 src 真的变了。
 *    （真正的 load() 在 VideoPlayer 里，那一层由组件自己的 watch 负责。）
 */
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import HomeView from '../HomeView.vue'

// vi.hoisted 的函数体跑在所有 import 之前，直接用顶部 import 进来的 ref 会撞上
// TDZ（Cannot access '__vi_import_1__' before initialization）。所以在里面
// 现取一次 vue。需要 ref 而不是普通对象：切语言那条用例要真的触发重新渲染。
const { localeRef, supplyEnabled } = await vi.hoisted(async () => {
  const { ref } = await import('vue')
  return { localeRef: ref('zh'), supplyEnabled: ref(true) }
})

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, locale: localeRef })
  }
})

vi.mock('@/stores', () => ({
  useAppStore: () => ({ publicSettingsLoaded: true, fetchPublicSettings: vi.fn() }),
  useAuthStore: () => ({ isAuthenticated: false, isAdmin: false, user: null, checkAuth: vi.fn() })
}))

vi.mock('@/stores/supply', () => ({
  useSupplyStore: () => ({ enabled: supplyEnabled.value, ensureLoaded: vi.fn() })
}))

// VideoPlayer 换成一个只把 src 摊到 DOM 上的桩：这个 spec 关心的是
// 「哪支片子被交给了播放器」，不是播放器自己的加载/重试行为。
const VideoPlayerStub = {
  props: ['src', 'poster', 'fit'],
  template: '<div class="video-stub" :data-src="src" :data-poster="poster || \'\'" />'
}

function mountHome() {
  return mount(HomeView, {
    global: {
      stubs: {
        VideoPlayer: VideoPlayerStub,
        Icon: true,
        LocaleSwitcher: true,
        StatusIcon: true,
        Header: true,
        AppFooter: true,
        RouterLink: { props: ['to'], template: '<a><slot /></a>' }
      }
    }
  })
}

const ZH = 'https://publicai.s3.ap-east-1.amazonaws.com/common/apex1_contributor_zh.mp4'
const EN = 'https://publicai.s3.ap-east-1.amazonaws.com/common/apex1_contributor_en.mp4'

beforeEach(() => {
  localeRef.value = 'zh'
  supplyEnabled.value = true
})

describe('首页视频的归属', () => {
  it('消费者视频在 #personal 区块里', () => {
    const wrapper = mountHome()
    const personal = wrapper.get('#personal')
    const video = personal.get('.video-stub')
    expect(video.attributes('data-src')).toContain('apex1-launch-47s-v4.mp4')
  })

  it('共享者视频在 #supply 区块里', () => {
    const wrapper = mountHome()
    const supply = wrapper.get('#supply')
    expect(supply.get('[data-testid="home-contributor-video"]').attributes('data-src')).toBe(ZH)
  })

  it('两支片子不是同一支，也没有互相跑错区块', () => {
    const wrapper = mountHome()
    const consumer = wrapper.get('#personal .video-stub').attributes('data-src')
    const contributor = wrapper.get('#supply .video-stub').attributes('data-src')
    expect(consumer).not.toBe(contributor)
    expect(wrapper.get('#personal').html()).not.toContain('apex1_contributor')
    expect(wrapper.get('#supply').html()).not.toContain('apex1-launch')
  })

  // 片子搬走之后那一节没了，锚点也一起删。Header 的导航清单里没有 why，
  // 留着只是一个谁都到不了的 id。
  it('原来的独立视频段 #why 已经不存在', () => {
    expect(mountHome().find('#why').exists()).toBe(false)
  })

  // 消费侧那支有封面图，共享者那支没做，靠 VideoPlayer 的深色渐变垫底。
  // 借用消费侧的封面会让共享者看到一张讲另一件事的图。
  it('共享者视频不借用消费侧的封面', () => {
    const wrapper = mountHome()
    expect(wrapper.get('#personal .video-stub').attributes('data-poster')).not.toBe('')
    expect(wrapper.get('[data-testid="home-contributor-video"]').attributes('data-poster')).toBe('')
  })
})

describe('共享者视频跟随界面语言', () => {
  it('中文界面取中文版', () => {
    localeRef.value = 'zh'
    expect(mountHome().get('[data-testid="home-contributor-video"]').attributes('data-src')).toBe(ZH)
  })

  it('英文界面取英文版', () => {
    localeRef.value = 'en'
    expect(mountHome().get('[data-testid="home-contributor-video"]').attributes('data-src')).toBe(EN)
  })

  it('切换语言后 src 跟着变（不是只在首次渲染时算一次）', async () => {
    const wrapper = mountHome()
    const sel = '[data-testid="home-contributor-video"]'
    expect(wrapper.get(sel).attributes('data-src')).toBe(ZH)

    localeRef.value = 'en'
    await wrapper.vm.$nextTick()
    expect(wrapper.get(sel).attributes('data-src')).toBe(EN)

    localeRef.value = 'zh'
    await wrapper.vm.$nextTick()
    expect(wrapper.get(sel).attributes('data-src')).toBe(ZH)
  })

  // 将来加第三种语言时，缺失的片子该退到英文，而不是拼出一个 404 地址。
  it('未知语言退到英文版', () => {
    localeRef.value = 'ja'
    expect(mountHome().get('[data-testid="home-contributor-video"]').attributes('data-src')).toBe(EN)
  })

  // 消费侧那支是通用宣传片，只有一个版本——别在改语言逻辑时顺手把它也接上。
  it('消费者视频不随语言变', () => {
    localeRef.value = 'zh'
    const zh = mountHome().get('#personal .video-stub').attributes('data-src')
    localeRef.value = 'en'
    const en = mountHome().get('#personal .video-stub').attributes('data-src')
    expect(zh).toBe(en)
  })
})
