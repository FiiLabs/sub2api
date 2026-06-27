import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useI18n } from 'vue-i18n'
import { useWorkspaceStore } from '@/stores/workspaces'
import { paletteForSubject } from '@/constants/workspacePalette'
import type { WorkspaceSubject } from '@/types'

/** 工作区视觉身份单一来源：默认取当前激活工作区，可传入指定 subject。 */
export function useWorkspaceIdentity(
  subject?: MaybeRefOrGetter<WorkspaceSubject | null | undefined>
) {
  const store = useWorkspaceStore()
  const { t } = useI18n()
  const current = computed<WorkspaceSubject | null>(() => toValue(subject) ?? store.activeWorkspace)
  const isTeam = computed(() => current.value?.type === 'team')
  return {
    subject: current,
    isTeam,
    icon: computed<'user' | 'users'>(() => (isTeam.value ? 'users' : 'user')),
    name: computed(() => current.value?.name ?? ''),
    typeLabel: computed(() => t(isTeam.value ? 'workspace.team' : 'workspace.personal')),
    palette: computed(() => paletteForSubject(current.value))
  }
}
