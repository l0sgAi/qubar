<template>
  <!-- 审核队列列表项：作者 / 时间 / 标题 / 摘要 / 图片 / 全文（默认折叠）/ AI 审核结论。
       待审帖子与被举报帖子（#31）共用：#extra 插槽放队列特有信息（如举报数与理由），
       #actions 插槽放队列各自的处理按钮 -->
  <article class="mod-item">
    <header class="mod-head">
      <NAvatar round :size="36" :src="post.author_avatar || undefined">
        <div v-if="!post.author_avatar">{{ (post.author_name || '').charAt(0).toUpperCase() || '?' }}</div>
      </NAvatar>
      <div class="mod-author">
        <SmartLink :to="post.user_id ? `/user/${post.user_id}` : null" class="mod-author-name">
          {{ post.author_name || t('circle.moderation.unknownUser') }}
        </SmartLink>
        <span class="mod-time">{{ t('circle.moderation.postedAt', { time: formatTime(post.create_time) }) }}</span>
      </div>
      <slot name="badges" :post="post" />
    </header>

    <h3 class="mod-title">{{ post.title || t('circle.moderation.untitled') }}</h3>
    <p v-if="post.summary" class="mod-summary">{{ post.summary }}</p>

    <NImageGroup v-if="images.length">
      <div class="mod-images">
        <NImage
          v-for="(url, idx) in images"
          :key="idx"
          :src="url"
          :width="80"
          :height="80"
          object-fit="cover"
          lazy
          class="mod-image"
        />
      </div>
    </NImageGroup>

    <!-- 全文默认折叠：展开时才挂载 Markdown 渲染，长列表不必一次渲染全部正文 -->
    <div v-if="post.content" class="mod-content-wrap">
      <button type="button" class="mod-toggle" @click="contentOpen = !contentOpen">
        <NIcon size="14"><component :is="contentOpen ? ChevronUpIcon : ChevronDownIcon" /></NIcon>
        <span>{{ contentOpen ? t('circle.moderation.hideContent') : t('circle.moderation.showContent') }}</span>
      </button>
      <div v-if="contentOpen" class="mod-content">
        <MentionPreview :content="post.content" :language="locale" />
      </div>
    </div>

    <slot name="extra" :post="post" />

    <!-- AI 审核结论 -->
    <section class="ai-panel" :class="`ai-panel--${state}`">
      <div class="ai-title">
        <NIcon size="15"><RobotIcon /></NIcon>
        <span>{{ t('circle.moderation.ai.title') }}</span>
        <!-- AI 分流结果（被举报列表常见 publish / review_publish / fail_open）；fail_open 通常同时带 error_msg -->
        <NTag v-if="decision" size="small" round :bordered="false" class="ai-decision">
          {{ t(`circle.moderation.decision.${decision}`) }}
        </NTag>
      </div>

      <p v-if="state === 'pending'" class="ai-note">{{ t('circle.moderation.ai.pending') }}</p>
      <p v-else-if="state === 'failed'" class="ai-note ai-note--warn">{{ t('circle.moderation.ai.failed') }}</p>

      <template v-else>
        <div class="ai-tags">
          <NTag
            v-for="key in hitHazards"
            :key="key"
            size="small"
            round
            :type="key === CARE_HAZARD ? 'error' : 'warning'"
          >
            {{ t('circle.moderation.ai.hazardTag', { label: hazardLabel(key), percent: toPercent(hazards[key]) }) }}
          </NTag>
          <span v-if="hitHazards.length === 0" class="ai-muted">{{ t('circle.moderation.ai.noHit') }}</span>
        </div>

        <div class="ai-meta">
          <span v-if="level">
            {{ t('circle.moderation.ai.severity', {
              level: t(`circle.moderation.severity.${level}`),
              score: Number(verdict.severity).toFixed(1)
            }) }}
          </span>
          <span v-if="hasConfidence">
            {{ t('circle.moderation.ai.confidence', { percent: toPercent(verdict.severity_confidence) }) }}
          </span>
          <span v-if="verdict.model" class="ai-muted">{{ t('circle.moderation.ai.model', { model: verdict.model }) }}</span>
        </div>

        <!-- 全部风险概率：默认折叠 -->
        <button v-if="hazardRows.length" type="button" class="mod-toggle" @click="hazardsOpen = !hazardsOpen">
          <NIcon size="14"><component :is="hazardsOpen ? ChevronUpIcon : ChevronDownIcon" /></NIcon>
          <span>{{ hazardsOpen ? t('circle.moderation.ai.hideHazards') : t('circle.moderation.ai.showHazards') }}</span>
        </button>
        <div v-if="hazardsOpen" class="hazard-table">
          <div
            v-for="row in hazardRows"
            :key="row.key"
            class="hazard-row"
            :class="{ 'hazard-row--hit': row.hit }"
          >
            <span class="hazard-label">{{ hazardLabel(row.key) }}</span>
            <span class="hazard-bar"><span class="hazard-fill" :style="{ width: `${row.percent}%` }"></span></span>
            <span class="hazard-value">{{ row.percent }}%</span>
          </div>
        </div>
      </template>

      <p v-if="needsCare(verdict)" class="ai-care">
        <NIcon size="15"><HeartIcon /></NIcon>
        <span>{{ t('circle.moderation.ai.care') }}</span>
      </p>
    </section>

    <footer v-if="$slots.actions" class="mod-actions">
      <slot name="actions" :post="post" />
    </footer>
  </article>
</template>

<script setup>
import { ref, computed } from 'vue'
import { NAvatar, NIcon, NImage, NImageGroup, NTag } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  ChevronDown as ChevronDownIcon,
  ChevronUp as ChevronUpIcon,
  Robot as RobotIcon,
  Heart as HeartIcon
} from '@vicons/tabler'
import SmartLink from '@/components/common/SmartLink.vue'
import MentionPreview from '@/components/post/detail/MentionPreview.vue'
import { useFormatTime } from '@/utils/i18n'
import {
  HAZARD_KEYS, CARE_HAZARD, toPercent, severityLevel, verdictState, needsCare, decisionKey
} from '@/constants/moderation'

const props = defineProps({
  // 审核队列帖子（GET /circle/moderation/posts 的 posts[] 项，被举报队列另带举报字段）
  post: {
    type: Object,
    required: true
  }
})

const { t, te, locale } = useI18n()
const { formatTime } = useFormatTime()

const contentOpen = ref(false)
const hazardsOpen = ref(false)

const images = computed(() => (Array.isArray(props.post.media_extra) ? props.post.media_extra.filter(Boolean) : []))

const verdict = computed(() => props.post.ai_verdict || null)
const state = computed(() => verdictState(verdict.value))
const decision = computed(() => decisionKey(verdict.value?.decision))
const hazards = computed(() => verdict.value?.hazards || {})
const hitHazards = computed(() => (Array.isArray(verdict.value?.hit_hazards) ? verdict.value.hit_hazards : []))
const level = computed(() => severityLevel(verdict.value?.severity))
const hasConfidence = computed(() => Number.isFinite(Number(verdict.value?.severity_confidence ?? NaN)))

// 全量表：已知 key 按固定顺序在前，后端新增的未知 key 追加在后
const hazardRows = computed(() => {
  const keys = [...HAZARD_KEYS.filter(k => k in hazards.value), ...Object.keys(hazards.value).filter(k => !HAZARD_KEYS.includes(k))]
  return keys.map(key => ({ key, percent: toPercent(hazards.value[key]), hit: hitHazards.value.includes(key) }))
})

// 后端新增的风险 key 暂无文案时原样展示 key
const hazardLabel = (key) => (te(`circle.moderation.hazards.${key}`) ? t(`circle.moderation.hazards.${key}`) : key)
</script>

<style scoped>
.mod-item {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 18px 16px;
  background: rgba(255, 255, 255, 0.025);
  border: 1px solid rgba(255, 255, 255, 0.07);
  border-radius: 14px;
}

.mod-head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.mod-author {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mod-author-name {
  font-weight: 600;
  font-size: 0.92rem;
  color: var(--text-primary, #fff);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.mod-author-name:hover {
  color: #8af0d0;
}

.mod-time {
  font-size: 0.78rem;
  color: rgba(255, 255, 255, 0.45);
}

.mod-title {
  margin: 0;
  font-size: 1.05rem;
  font-weight: 700;
  color: var(--text-primary, #fff);
  word-break: break-word;
}

.mod-summary {
  margin: 0;
  font-size: 0.88rem;
  line-height: 1.6;
  color: rgba(255, 255, 255, 0.7);
  word-break: break-word;
}

.mod-images {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.mod-image {
  border-radius: 8px;
  overflow: hidden;
}

.mod-toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  align-self: flex-start;
  padding: 4px 8px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: rgba(255, 255, 255, 0.55);
  font-size: 0.8rem;
  cursor: pointer;
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1), color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.mod-toggle:hover {
  background: rgba(102, 234, 194, 0.14);
  color: #8af0d0;
}

.mod-content-wrap {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.mod-content {
  padding: 4px 12px;
  border-radius: 10px;
  background: rgba(0, 0, 0, 0.18);
  border: 1px solid rgba(255, 255, 255, 0.06);
  max-height: 520px;
  overflow-y: auto;
}

/* 正文透出容器底色（同评论区写法），不用 md-editor 暗色主题自带的纯黑底 */
.mod-content :deep(.md-editor) {
  background: transparent !important;
}

.mod-content :deep(.md-editor-dark) {
  --md-bk-color: transparent;
  --md-scrollbar-bg-color: transparent;
}

.mod-content :deep(.md-editor-dark .md-editor-preview) {
  --md-theme-bg-color: transparent;
  font-size: 14px;
}

/* AI 结论区 */
.ai-panel {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  border-radius: 12px;
  background: rgba(102, 234, 194, 0.05);
  border: 1px solid rgba(102, 234, 194, 0.16);
}

.ai-panel--pending,
.ai-panel--failed {
  background: rgba(255, 255, 255, 0.03);
  border-color: rgba(255, 255, 255, 0.08);
}

.ai-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.82rem;
  font-weight: 600;
  color: #8af0d0;
}

.ai-decision {
  margin-left: auto;
  font-weight: 400;
}

.ai-note {
  margin: 0;
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.65);
}

.ai-note--warn {
  color: #f2c97d;
}

.ai-tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
}

.ai-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 16px;
  font-size: 0.82rem;
  color: rgba(255, 255, 255, 0.75);
}

.ai-muted {
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.45);
}

.ai-care {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 0;
  padding: 8px 10px;
  border-radius: 10px;
  background: rgba(255, 90, 90, 0.1);
  border: 1px solid rgba(255, 90, 90, 0.25);
  color: #ff9d9d;
  font-size: 0.85rem;
  font-weight: 500;
}

.hazard-table {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 20px;
}

.hazard-row {
  display: grid;
  grid-template-columns: 96px 1fr 40px;
  align-items: center;
  gap: 8px;
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.6);
}

.hazard-row--hit {
  color: #ff9d9d;
  font-weight: 600;
}

.hazard-label {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.hazard-bar {
  height: 6px;
  border-radius: 3px;
  background: rgba(255, 255, 255, 0.08);
  overflow: hidden;
}

.hazard-fill {
  display: block;
  height: 100%;
  border-radius: 3px;
  background: rgba(102, 234, 194, 0.7);
}

.hazard-row--hit .hazard-fill {
  background: rgba(255, 90, 90, 0.85);
}

.hazard-value {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.mod-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

@media (max-width: 640px) {
  .hazard-table {
    grid-template-columns: 1fr;
  }
}
</style>
