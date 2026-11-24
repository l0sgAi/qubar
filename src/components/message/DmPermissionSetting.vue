<!-- 个人中心「谁可以给我发私信」：四档单选，切换即保存（PUT /user/settings 的 dm_permission） -->
<template>
  <div class="dm-permission">
    <div class="dm-permission-label">{{ t('message.permission.label') }}</div>
    <div class="dm-permission-desc">{{ t('message.permission.desc') }}</div>
    <NSpin :show="loading" size="small">
      <NRadioGroup
        :value="value"
        :disabled="loading || saving || value === null"
        name="dm-permission"
        class="dm-permission-options"
        @update:value="handleChange"
      >
        <NRadio
          v-for="opt in DM_PERMISSION_OPTIONS"
          :key="opt.value"
          :value="opt.value"
          class="dm-permission-option"
        >
          {{ t(`message.permission.${opt.key}`) }}
        </NRadio>
      </NRadioGroup>
    </NSpin>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { NRadioGroup, NRadio, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { getUserSettings, updateUserSettings } from '@/api/user'
import { DM_PERMISSION_OPTIONS } from '@/utils/message'

const { t } = useI18n()
const message = useMessage()

const value = ref(null)
const loading = ref(true)
const saving = ref(false)

onMounted(async () => {
  try {
    const res = await getUserSettings()
    value.value = res.data?.dm_permission ?? 0
  } catch (error) {
    console.error('读取私信设置失败:', error)
    message.error(t('message.permission.loadFailed'))
  } finally {
    loading.value = false
  }
})

const handleChange = async (next) => {
  const prev = value.value
  if (next === prev || saving.value) return
  value.value = next
  saving.value = true
  try {
    const res = await updateUserSettings({ dm_permission: next })
    value.value = res.data?.dm_permission ?? next
    message.success(t('message.permission.saved'))
  } catch (error) {
    console.error('保存私信设置失败:', error)
    value.value = prev
    message.error(t('message.permission.saveFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.dm-permission {
  padding: 14px 16px;
  border-top: 1px solid var(--glass-border);
}

.dm-permission-label {
  font-size: 15px;
  color: var(--text-primary);
}

.dm-permission-desc {
  margin: 4px 0 10px;
  font-size: 12px;
  color: var(--text-tertiary);
}

.dm-permission-options {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dm-permission-option {
  --n-dot-color-active: #66eac2 !important;
  --n-box-shadow-active: inset 0 0 0 1px #66eac2 !important;
  --n-box-shadow-focus: inset 0 0 0 1px #66eac2, 0 0 0 2px rgba(102, 234, 194, 0.2) !important;
  --n-box-shadow-hover: inset 0 0 0 1px #66eac2 !important;
}
</style>
