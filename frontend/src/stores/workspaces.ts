import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { workspacesAPI } from '@/api/workspaces'
import type { WorkspaceSubject } from '@/types'

const ACTIVE_WORKSPACE_KEY = 'active_workspace_subject_id'

export const useWorkspaceStore = defineStore('workspaces', () => {
  const workspaces = ref<WorkspaceSubject[]>([])
  const activeSubjectId = ref<number | null>(null)
  const subjectVersion = ref(0)
  const loading = ref(false)

  const activeWorkspace = computed(() => {
    return workspaces.value.find((item) => item.billing_subject_id === activeSubjectId.value) ?? workspaces.value[0] ?? null
  })

  const isTeamWorkspace = computed(() => activeWorkspace.value?.type === 'team')

  const canManageMembers = computed(() => {
    return !!activeWorkspace.value?.permissions?.['team.members.manage']
  })

  async function loadWorkspaces(): Promise<void> {
    loading.value = true
    try {
      const response = await workspacesAPI.list()
      workspaces.value = response.workspaces
      const saved = Number(localStorage.getItem(ACTIVE_WORKSPACE_KEY) || 0)
      const savedExists = response.workspaces.some((item) => item.billing_subject_id === saved)
      activeSubjectId.value = savedExists ? saved : response.workspaces[0]?.billing_subject_id ?? null
      if (activeSubjectId.value) {
        localStorage.setItem(ACTIVE_WORKSPACE_KEY, String(activeSubjectId.value))
      }
    } finally {
      loading.value = false
    }
  }

  function switchWorkspace(subjectId: number): void {
    const exists = workspaces.value.some((item) => item.billing_subject_id === subjectId)
    if (!exists) return
    activeSubjectId.value = subjectId
    localStorage.setItem(ACTIVE_WORKSPACE_KEY, String(subjectId))
    subjectVersion.value++
  }

  return { workspaces, activeSubjectId, subjectVersion, activeWorkspace, isTeamWorkspace, canManageMembers, loading, loadWorkspaces, switchWorkspace }
})
