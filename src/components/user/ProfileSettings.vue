<!-- 个人中心「隐私设置」分组（GET / PUT /user/settings，后端 v0.9.0）。
     一次读取全部设置，每一项是一个 .setting-item，按顺序追加；保存统一走 updateSetting(key, value)。 -->
<template>
  <section class="profile-settings" :aria-label="t('follow.settings.groupTitle')">
    <div class="settings-group-title">{{ t('follow.settings.groupTitle') }}</div>

    <div v-if="loadFailed" class="setting-item setting-error">
      <span>{{ t('follow.settings.loadFailed') }}</span>
      <NButton text type="primary" size="small" @click="loadSettings">{{ t('common.retry') }}</NButton>
    </div>

    <template v-else>
      <!-- 1. 关注 / 粉丝列表公开（follow_list_visibility：0 公开 / 1 私密） -->
      <div class="setting-item">
        <div class="setting-text">
          <label class="setting-label" :for="switchId">{{ t('follow.settings.listVisibility') }}</label>
          <div class="setting-hint">{{ t('follow.settings.listVisibilityHint') }}</div>
        </div>
        <NSwitch
          :id="switchId"
          :value="listPublic"
          :loading="saving.follow_list_visibility"
          :disabled="!settings || saving.follow_list_visibility"
          @update:value="handleListVisibilityChange" />
      </div>

      <DmPermissionSetting
        class="setting-dm"
        :value="settings && (settings.dm_permission ?? 0)"
        :loading="!settings"
        :saving="saving.dm_permission"
        @update:value="updateSetting('dm_permission', $event)" />
    </template>
  </section>
</template>

<script setup>
import DmPermissionSetting from '@/components/message/DmPermissionSetting.vue'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { NButton, NSwitch, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { getUserSettings, updateUserSettings } from '@/api/user'
import { auth } from '@/utils/auth'
import { FOLLOW_LIST_VISIBILITY } from '@/utils/follow'

const { t } = useI18n()
const message = useMessage()

// 同页可能同时存在桌面 / 移动两处实例（v-if 互斥，一般只有一个），id 加随机后缀避免重复
const switchId = `follow-list-visibility-${Math.random().toString(36).slice(2, 8)}`

// 后端返回的全部设置：{ follow_list_visibility, dm_permission }；null 表示还没加载到
const settings = ref(null)
const loadFailed = ref(false)
// 每个设置项各自的保存中状态（key → boolean）
const saving = ref({})
let loadVersion = 0

const loadSettings = async () => {
  const version = ++loadVersion
  settings.value = null
  saving.value = {}
  loadFailed.value = false
  if (!auth.isAuthenticated()) return
  try {
    const res = await getUserSettings()
    if (version !== loadVersion) return
    settings.value = res?.data || null
    if (!settings.value) loadFailed.value = true
  } catch (error) {
    if (version !== loadVersion) return
    console.error('获取设置失败:', error)
    loadFailed.value = true
  }
}

/**
 * 保存单个设置项（部分更新）。先乐观更新，失败回滚并提示；每个响应只确认本次修改的字段，避免覆盖另一项并发保存。
 * @param {'follow_list_visibility'|'dm_permission'} key
 * @param {number} value
 * @returns {Promise<boolean>} 是否保存成功
 */
const updateSetting = async (key, value) => {
  if (!settings.value || saving.value[key] || !auth.isAuthenticated()) return false
  const version = loadVersion
  const prev = settings.value[key]
  if (prev === value) return true
  settings.value = { ...settings.value, [key]: value }
  saving.value[key] = true
  try {
    const res = await updateUserSettings({ [key]: value })
    if (version !== loadVersion) return false
    settings.value[key] = res?.data?.[key] ?? value
    message.success(t(key === 'dm_permission' ? 'message.permission.saved' : 'follow.settings.saved'))
    return true
  } catch (error) {
    if (version !== loadVersion) return false
    console.error('保存设置失败:', error)
    settings.value = { ...settings.value, [key]: prev }
    message.error(t(key === 'dm_permission' ? 'message.permission.saveFailed' : 'follow.settings.saveFailed'))
    return false
  } finally {
    if (version === loadVersion) saving.value[key] = false
  }
}

const listPublic = computed(() => settings.value?.follow_list_visibility !== FOLLOW_LIST_VISIBILITY.PRIVATE)

const handleListVisibilityChange = (isPublic) => {
  updateSetting('follow_list_visibility', isPublic ? FOLLOW_LIST_VISIBILITY.PUBLIC : FOLLOW_LIST_VISIBILITY.PRIVATE)
}

onMounted(loadSettings)
onBeforeUnmount(() => { ++loadVersion })

defineExpose({ settings, updateSetting, reload: loadSettings })
</script>

<style scoped>
.profile-settings {
  border-radius: 16px;
  background: var(--glass-bg);
  border: 1px solid var(--glass-border);
  overflow: hidden;
}

.settings-group-title {
  padding: 14px 16px 6px;
  font-size: 13px;
  color: var(--text-tertiary);
}

.setting-dm {
  padding: 10px 16px;
}

.setting-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 52px;
  padding: 10px 16px;
  border-top: 1px solid var(--glass-border);
}

.setting-text {
  min-width: 0;
}

.setting-label {
  display: block;
  font-size: 14px;
  color: var(--text-primary);
}

.setting-hint {
  margin-top: 2px;
  font-size: 12px;
  line-height: 1.4;
  color: var(--text-tertiary);
}

.setting-error {
  font-size: 13px;
  color: var(--text-secondary);
}
</style>
