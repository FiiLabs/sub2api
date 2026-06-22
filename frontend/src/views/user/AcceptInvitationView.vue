<template>
  <AppLayout>
    <div class="mx-auto flex max-w-lg flex-col gap-6 py-8">
      <div class="card p-8">
        <!-- Loading -->
        <div v-if="state === 'loading'" class="flex flex-col items-center gap-3 py-8">
          <LoadingSpinner />
          <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('acceptInvitation.loading') }}</p>
        </div>

        <!-- Error states (missing token / invalid / expired / already handled / email mismatch) -->
        <div v-else-if="state === 'error'" class="flex flex-col items-center gap-4 py-4 text-center">
          <div class="flex h-12 w-12 items-center justify-center rounded-full bg-red-100 text-red-600 dark:bg-red-900/30 dark:text-red-400">
            <Icon name="exclamationTriangle" size="lg" />
          </div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('acceptInvitation.title') }}</h2>
          <p class="text-sm text-gray-600 dark:text-dark-300">{{ errorMessage }}</p>
          <RouterLink to="/dashboard" class="btn btn-secondary mt-2">
            {{ t('acceptInvitation.backToDashboard') }}
          </RouterLink>
        </div>

        <!-- Ready: show invitation details + accept button -->
        <div v-else class="flex flex-col items-center gap-5 text-center">
          <div class="flex h-12 w-12 items-center justify-center rounded-full bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
            <Icon name="users" size="lg" />
          </div>
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('acceptInvitation.heading') }}</h2>
            <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">
              {{ t('acceptInvitation.invitedTo') }}
              <span class="font-semibold text-gray-900 dark:text-white">{{ preview?.team_name }}</span>
              <span v-if="preview?.role"> {{ t('acceptInvitation.asRole', { role: roleLabel(preview.role) }) }}</span>
            </p>
          </div>

          <div class="w-full rounded-lg bg-gray-50 px-4 py-3 text-left dark:bg-dark-800">
            <div class="text-xs uppercase text-gray-400 dark:text-dark-500">{{ t('acceptInvitation.invitedEmail') }}</div>
            <div class="mt-1 text-sm text-gray-900 dark:text-white">{{ preview?.email }}</div>
          </div>

          <div class="flex w-full flex-col gap-2 sm:flex-row sm:justify-center">
            <button
              type="button"
              class="btn btn-primary"
              :disabled="accepting"
              @click="handleAccept"
            >
              <Icon v-if="accepting" name="refresh" size="sm" class="mr-2 animate-spin" />
              {{ accepting ? t('acceptInvitation.accepting') : t('acceptInvitation.accept') }}
            </button>
            <RouterLink to="/dashboard" class="btn btn-secondary">
              {{ t('acceptInvitation.decline') }}
            </RouterLink>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter, RouterLink } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useWorkspaceStore } from '@/stores/workspaces'
import { workspacesAPI, type InvitationPreview } from '@/api/workspaces'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const workspaceStore = useWorkspaceStore()

type ViewState = 'loading' | 'error' | 'ready'

const state = ref<ViewState>('loading')
const errorMessage = ref('')
const preview = ref<InvitationPreview | null>(null)
const accepting = ref(false)

const token = computed(() => {
  const raw = route.query.token
  return typeof raw === 'string' ? raw : Array.isArray(raw) ? raw[0] ?? '' : ''
})

function roleLabel(role: string): string {
  const key = `members.roles.${role}`
  const label = t(key)
  return label === key ? role : label
}

function normalizeEmail(value: string | null | undefined): string {
  return (value || '').trim().toLowerCase()
}

function setError(message: string): void {
  state.value = 'error'
  errorMessage.value = message
}

async function loadPreview(): Promise<void> {
  if (!token.value) {
    setError(t('acceptInvitation.missingToken'))
    return
  }
  state.value = 'loading'
  try {
    const data = await workspacesAPI.previewInvitation(token.value)
    preview.value = data

    if (data.expired) {
      setError(t('acceptInvitation.expired'))
      return
    }
    if (data.status && data.status !== 'pending') {
      setError(t('acceptInvitation.alreadyHandled'))
      return
    }
    // Friendly client-side email check (acceptance is enforced server-side too).
    const currentEmail = normalizeEmail(authStore.user?.email)
    if (currentEmail && normalizeEmail(data.email) !== currentEmail) {
      setError(t('acceptInvitation.emailMismatch', { email: data.email }))
      return
    }

    state.value = 'ready'
  } catch (error: unknown) {
    const err = error as { status?: number; message?: string }
    setError(err?.message || t('acceptInvitation.invalid'))
  }
}

async function handleAccept(): Promise<void> {
  if (!token.value || accepting.value) return
  accepting.value = true
  try {
    const result = await workspacesAPI.acceptInvitation(token.value)
    await workspaceStore.loadWorkspaces()

    // Switch to the joined team workspace (match by billing subject, fall back to team id).
    const joined = workspaceStore.workspaces.find(
      (ws) => ws.billing_subject_id === result.team.billing_subject_id || ws.team_id === result.team.id
    )
    if (joined) {
      workspaceStore.switchWorkspace(joined.billing_subject_id)
    }

    appStore.showSuccess(t('acceptInvitation.acceptSuccess'))
    await router.push('/members')
  } catch (error: unknown) {
    const err = error as { status?: number; message?: string }
    // A 4xx here is most likely an email mismatch or an expired/handled invitation.
    if (err?.status && err.status >= 400 && err.status < 500) {
      const email = preview.value?.email
      setError(
        err.message ||
          (email ? t('acceptInvitation.emailMismatch', { email }) : t('acceptInvitation.acceptFailed'))
      )
    } else {
      appStore.showError(err?.message || t('acceptInvitation.acceptFailed'))
    }
  } finally {
    accepting.value = false
  }
}

onMounted(() => {
  loadPreview()
})
</script>
