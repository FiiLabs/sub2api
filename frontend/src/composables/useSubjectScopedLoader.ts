import { onMounted, watch } from 'vue'
import { useWorkspaceStore } from '@/stores/workspaces'

export interface SubjectScopedLoaderOptions {
  immediate?: boolean
}

/**
 * 注册一个"主体相关"的加载函数：默认挂载即加载，且工作区切换（activeSubjectId 或
 * subjectVersion 变化）时自动重载。用于替换各数据视图中仅 onMounted 的加载，
 * 解决切换主体后数据不刷新的问题。
 */
export function useSubjectScopedLoader(
  load: () => void | Promise<void>,
  options: SubjectScopedLoaderOptions = {}
): void {
  const ws = useWorkspaceStore()
  watch(
    () => [ws.activeSubjectId, ws.subjectVersion] as const,
    () => { void load() }
  )
  if (options.immediate !== false) {
    onMounted(() => { void load() })
  }
}
