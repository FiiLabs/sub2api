/**
 * VideoPlayer 组件测试
 * 覆盖宣传视频最容易翻车的三件事：加载失败自动重试、重试用尽后的兜底、播放结束的循环
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import VideoPlayer from '@/components/common/VideoPlayer.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

// 全局 setup 里的 IntersectionObserver 不会回调，这里换成「立刻判定已进入视口」
class ImmediateIntersectionObserver {
  constructor(private callback: IntersectionObserverCallback) {}

  observe(target: Element) {
    this.callback(
      [{ target, isIntersecting: true, intersectionRatio: 1 } as unknown as IntersectionObserverEntry],
      this as unknown as IntersectionObserver
    )
  }

  unobserve() {}
  disconnect() {}
  takeRecords() {
    return []
  }
}

const play = vi.fn()
const load = vi.fn()
const pause = vi.fn()

async function mountPlayer(props: Record<string, unknown> = {}) {
  const wrapper = mount(VideoPlayer, {
    props: { src: 'https://cdn.example.com/promo.mp4', ...props }
  })
  // 等观察器回调 → <video> 挂载
  await flushPromises()
  await wrapper.vm.$nextTick()
  return wrapper
}

/** 让组件进入「已就绪 + 时长已知」的状态，进度条才可交互 */
async function makeSeekable(wrapper: Awaited<ReturnType<typeof mountPlayer>>, seconds = 100) {
  const video = wrapper.find('video').element as HTMLVideoElement
  Object.defineProperty(video, 'duration', { configurable: true, value: seconds })
  Object.defineProperty(video, 'currentTime', { configurable: true, writable: true, value: 0 })
  video.dispatchEvent(new Event('loadedmetadata'))
  video.dispatchEvent(new Event('canplay'))
  await flushPromises()

  const track = wrapper.find('[role="slider"]')
  // jsdom 的布局全是 0，喂一个 200px 宽的轨道
  ;(track.element as HTMLElement).getBoundingClientRect = () =>
    ({ left: 0, top: 0, right: 200, bottom: 4, width: 200, height: 4, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect

  return { video, track }
}

/** jsdom 没有真实媒体元素，手动喂一个错误码后触发 error */
async function emitError(video: HTMLVideoElement, code = 2) {
  Object.defineProperty(video, 'error', { configurable: true, value: { code } })
  video.dispatchEvent(new Event('error'))
  await flushPromises()
}

beforeEach(() => {
  vi.useFakeTimers()
  play.mockReset().mockResolvedValue(undefined)
  load.mockReset()
  pause.mockReset()

  globalThis.IntersectionObserver = ImmediateIntersectionObserver as unknown as typeof IntersectionObserver
  Object.defineProperty(HTMLMediaElement.prototype, 'play', { configurable: true, value: play })
  Object.defineProperty(HTMLMediaElement.prototype, 'load', { configurable: true, value: load })
  Object.defineProperty(HTMLMediaElement.prototype, 'pause', { configurable: true, value: pause })
})

afterEach(() => {
  vi.useRealTimers()
})

describe('VideoPlayer', () => {
  it('加载失败后按退避延迟自动重试', async () => {
    const wrapper = await mountPlayer({ maxRetries: 2, retryDelay: 1000 })
    const video = wrapper.find('video').element as HTMLVideoElement

    await emitError(video)
    expect(load).not.toHaveBeenCalled()

    // 第一次重试：1000ms
    await vi.advanceTimersByTimeAsync(1000)
    expect(load).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('error')?.[0]?.[0]).toMatchObject({ attempt: 1, recovering: true })

    // 第二次重试：指数退避到 2000ms
    await emitError(video)
    await vi.advanceTimersByTimeAsync(1000)
    expect(load).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1000)
    expect(load).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('重试次数用尽后落到错误态，手动重试会重新加载', async () => {
    const wrapper = await mountPlayer({ maxRetries: 1, retryDelay: 500 })
    const video = wrapper.find('video').element as HTMLVideoElement

    await emitError(video)
    await vi.advanceTimersByTimeAsync(500)
    await emitError(video)

    expect(wrapper.text()).toContain('common.videoPlayer.error')
    const lastError = wrapper.emitted('error')?.at(-1)?.[0]
    expect(lastError).toMatchObject({ recovering: false })

    load.mockClear()
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(load).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).not.toContain('common.videoPlayer.error')

    wrapper.unmount()
  })

  it('格式类错误只重试一次，不空转到上限', async () => {
    const wrapper = await mountPlayer({ maxRetries: 5, retryDelay: 100 })
    const video = wrapper.find('video').element as HTMLVideoElement

    // code 4 = MEDIA_ERR_SRC_NOT_SUPPORTED
    await emitError(video, 4)
    await vi.advanceTimersByTimeAsync(100)
    await emitError(video, 4)

    expect(wrapper.text()).toContain('common.videoPlayer.error')

    wrapper.unmount()
  })

  it('播放结束后回到起点继续播（loop 属性失效时的兜底）', async () => {
    const wrapper = await mountPlayer()
    const video = wrapper.find('video').element as HTMLVideoElement
    Object.defineProperty(video, 'currentTime', { configurable: true, writable: true, value: 42 })

    play.mockClear()
    video.dispatchEvent(new Event('ended'))
    await flushPromises()

    expect(video.currentTime).toBe(0)
    expect(play).toHaveBeenCalled()

    wrapper.unmount()
  })

  it('loop 关闭时结束就停住', async () => {
    const wrapper = await mountPlayer({ loop: false })
    const video = wrapper.find('video').element as HTMLVideoElement
    Object.defineProperty(video, 'currentTime', { configurable: true, writable: true, value: 42 })

    play.mockClear()
    video.dispatchEvent(new Event('ended'))
    await flushPromises()

    expect(play).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('点击进度条跳到对应时间', async () => {
    const wrapper = await mountPlayer()
    const { video, track } = await makeSeekable(wrapper)

    await track.trigger('pointerdown', { clientX: 100 })
    expect(video.currentTime).toBe(50)

    await track.trigger('pointerdown', { clientX: 20 })
    expect(video.currentTime).toBe(10)

    wrapper.unmount()
  })

  it('按住拖动时进度跟手，松手落在最终位置', async () => {
    const wrapper = await mountPlayer()
    const { video, track } = await makeSeekable(wrapper)

    await track.trigger('pointerdown', { clientX: 40 })
    expect(video.currentTime).toBe(20)

    // 拖出播放器范围也要继续跟手（监听挂在 window 上）
    window.dispatchEvent(new MouseEvent('pointermove', { clientX: 150 }))
    await vi.advanceTimersByTimeAsync(20)
    expect(video.currentTime).toBe(75)

    // 超出轨道范围的坐标被夹到两端
    window.dispatchEvent(new MouseEvent('pointerup', { clientX: 400 }))
    await flushPromises()
    expect(video.currentTime).toBe(100)

    // 松手之后的移动不再改变进度
    window.dispatchEvent(new MouseEvent('pointermove', { clientX: 0 }))
    await vi.advanceTimersByTimeAsync(20)
    expect(video.currentTime).toBe(100)

    wrapper.unmount()
  })

  it('方向键微调进度，Shift 加大步长', async () => {
    const wrapper = await mountPlayer()
    const { video, track } = await makeSeekable(wrapper)

    await track.trigger('keydown', { key: 'ArrowRight' })
    expect(video.currentTime).toBe(5)

    await track.trigger('keydown', { key: 'ArrowRight', shiftKey: true })
    expect(video.currentTime).toBe(15)

    await track.trigger('keydown', { key: 'ArrowLeft' })
    expect(video.currentTime).toBe(10)

    await track.trigger('keydown', { key: 'Home' })
    expect(video.currentTime).toBe(0)

    wrapper.unmount()
  })

  it('时长未知时进度条不可交互', async () => {
    const wrapper = await mountPlayer()
    const video = wrapper.find('video').element as HTMLVideoElement
    Object.defineProperty(video, 'duration', { configurable: true, value: Infinity })
    Object.defineProperty(video, 'currentTime', { configurable: true, writable: true, value: 0 })
    video.dispatchEvent(new Event('loadedmetadata'))
    video.dispatchEvent(new Event('canplay'))
    await flushPromises()

    const track = wrapper.find('[role="slider"]')
    expect(track.attributes('aria-disabled')).toBe('true')
    expect(track.attributes('tabindex')).toBe('-1')

    await track.trigger('pointerdown', { clientX: 100 })
    expect(video.currentTime).toBe(0)

    wrapper.unmount()
  })

  it('缓冲长时间无进展会当作一次失败处理', async () => {
    const wrapper = await mountPlayer({ stallTimeout: 3000, retryDelay: 100 })
    const video = wrapper.find('video').element as HTMLVideoElement

    video.dispatchEvent(new Event('playing'))
    video.dispatchEvent(new Event('waiting'))
    await flushPromises()

    await vi.advanceTimersByTimeAsync(3000)
    await vi.advanceTimersByTimeAsync(100)
    expect(load).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })
})
