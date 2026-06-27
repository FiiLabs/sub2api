<template>
  <BaseDialog :show="show" :title="operation === 'add' ? t('admin.teams.deposit') : t('admin.teams.withdraw')" width="narrow" @close="$emit('close')">
    <form v-if="team" id="team-balance-form" @submit.prevent="handleSubmit" class="space-y-5">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30"><span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ team.name.charAt(0).toUpperCase() }}</span></div>
        <div class="flex-1"><p class="font-medium text-gray-900 dark:text-white">{{ team.name }}</p><p class="text-sm text-gray-500">{{ t('admin.teams.currentBalance') }}: ${{ formatBalance(team.balance) }}</p></div>
      </div>
      <div>
        <label class="input-label">{{ operation === 'add' ? t('admin.teams.depositAmount') : t('admin.teams.withdrawAmount') }}</label>
        <div class="relative flex gap-2">
          <div class="relative flex-1"><div class="absolute left-3 top-1/2 -translate-y-1/2 font-medium text-gray-500">$</div><input v-model.number="form.amount" type="number" step="any" min="0" required class="input pl-8" /></div>
          <button v-if="operation === 'subtract'" type="button" @click="fillAll" class="btn btn-secondary whitespace-nowrap">{{ t('admin.teams.withdrawAll') }}</button>
        </div>
      </div>
      <div><label class="input-label">{{ t('admin.teams.balanceNotes') }}</label><textarea v-model="form.notes" rows="3" class="input"></textarea></div>
      <div v-if="form.amount > 0" class="rounded-xl border border-blue-200 bg-blue-50 p-4 dark:border-blue-800 dark:bg-blue-950"><div class="flex items-center justify-between text-sm"><span class="text-gray-700 dark:text-gray-300">{{ t('admin.teams.newBalance') }}:</span><span class="font-bold text-gray-900 dark:text-gray-100">${{ formatBalance(calculateNew()) }}</span></div></div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="team-balance-form" :disabled="submitting || !form.amount" class="btn" :class="operation === 'add' ? 'bg-emerald-600 text-white' : 'btn-danger'">{{ submitting ? t('common.saving') : t('common.confirm') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminTeamsAPI } from '@/api/admin/teams'
import type { AdminTeam } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; team: AdminTeam | null; operation: 'add' | 'subtract' }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const submitting = ref(false)
const form = reactive({ amount: 0, notes: '' })
watch(() => props.show, (v) => { if (v) { form.amount = 0; form.notes = '' } })

const formatBalance = (value: number) => {
  if (value === 0) return '0.00'
  const formatted = value.toFixed(8).replace(/\.?0+$/, '')
  const parts = formatted.split('.')
  if (parts.length === 1) return formatted + '.00'
  if (parts[1].length === 1) return formatted + '0'
  return formatted
}
const fillAll = () => { if (props.team) form.amount = props.team.balance }
const calculateNew = () => {
  if (!props.team) return 0
  const result = props.operation === 'add' ? props.team.balance + form.amount : props.team.balance - form.amount
  return Math.abs(result) < 1e-10 ? 0 : result
}
const handleSubmit = async () => {
  if (!props.team) return
  if (!form.amount || form.amount <= 0) { appStore.showError(t('admin.teams.amountRequired')); return }
  if (props.operation === 'subtract' && form.amount > props.team.balance) { appStore.showError(t('admin.teams.insufficientBalance')); return }
  submitting.value = true
  try {
    await adminTeamsAPI.updateBalance(props.team.id, form.amount, props.operation, form.notes)
    appStore.showSuccess(t('admin.teams.balanceUpdated'))
    emit('success'); emit('close')
  } catch (e: any) {
    appStore.showError(e?.message || t('admin.teams.failedToUpdateBalance'))
  } finally { submitting.value = false }
}
</script>
