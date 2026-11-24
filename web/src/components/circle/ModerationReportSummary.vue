<template>
  <!-- 被举报队列（#31）列表项的举报信息：放在 ModerationPostItem 的 #extra 插槽，
       显示举报数、本轮首次举报时间、自动屏蔽说明，以及最近 10 条举报理由（不含举报人） -->
  <section class="report-summary">
    <div class="report-meta">
      <span class="report-count">
        <NIcon size="15"><FlagIcon /></NIcon>
        <span>{{ countText }}</span>
      </span>
      <span v-if="post.first_report_time" class="report-time">
        {{ t('circle.moderation.reported.firstReportAt', { time: formatTime(post.first_report_time) }) }}
      </span>
    </div>

    <p v-if="autoHidden" class="report-hidden-hint">{{ t('circle.moderation.reported.autoHiddenHint') }}</p>

    <div v-if="reports.length" class="report-reasons">
      <div class="report-reasons-title">{{ t('circle.moderation.reported.reasonsTitle') }}</div>
      <!-- 按理由类型聚合（最近 10 条内），按次数降序；展开后看每条补充说明 -->
      <div class="report-groups">
        <NTag v-for="g in reasonGroups" :key="g.key" size="small" round :bordered="false" class="report-group">
          {{ g.n > 1 ? t('circle.moderation.reported.reasonCount', { label: g.label, n: g.n }) : g.label }}
        </NTag>
      </div>
      <button type="button" class="report-toggle" @click="detailsOpen = !detailsOpen">
        <NIcon size="14"><component :is="detailsOpen ? ChevronUpIcon : ChevronDownIcon" /></NIcon>
        <span>{{ detailsOpen ? t('circle.moderation.reported.hideReports') : t('circle.moderation.reported.showReports') }}</span>
      </button>
      <ul v-if="detailsOpen" class="report-list">
        <li v-for="(r, idx) in reports" :key="idx" class="report-row">
          <NTag size="small" round :bordered="false" class="report-type">
            {{ t(`report.reasons.${reportReasonKey(r.reason_type)}`) }}
          </NTag>
          <span v-if="r.reason" class="report-reason">{{ r.reason }}</span>
          <span class="report-row-time">{{ formatTime(r.create_time) }}</span>
        </li>
      </ul>
      <p v-if="truncated" class="report-more">
        {{ t('circle.moderation.reported.onlyRecent', { n: REPORTS_SHOWN_MAX }) }}
      </p>
    </div>
  </section>
</template>

<script setup>
import { ref, computed } from 'vue'
import { NIcon, NTag } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Flag as FlagIcon, ChevronDown as ChevronDownIcon, ChevronUp as ChevronUpIcon } from '@vicons/tabler'
import { useFormatTime } from '@/utils/i18n'
import { POST_STATUS } from '@/composables/usePostReview'
import { REPORTS_SHOWN_MAX } from '@/constants/moderation'
import { reportReasonKey } from '@/constants/report'

const props = defineProps({
  // 被举报帖子（GET /circle/moderation/reports 的 posts[] 项）
  post: {
    type: Object,
    required: true
  }
})

const { t } = useI18n()
const { formatTime } = useFormatTime()

const reports = computed(() => (Array.isArray(props.post.reports) ? props.post.reports : []))
const count = computed(() => Number(props.post.report_count) || reports.value.length)
const countText = computed(() => (count.value === 1
  ? t('circle.moderation.reported.reportCountOne')
  : t('circle.moderation.reported.reportCount', { n: count.value })))
// 后端只返回最近 10 条，总数更多时注明
const truncated = computed(() => count.value > REPORTS_SHOWN_MAX)
const autoHidden = computed(() => props.post.status === POST_STATUS.BLOCKED)

// 理由类型聚合：{ key, label, n }，次数相同保持首次出现（新→旧）的顺序
const reasonGroups = computed(() => {
  const groups = new Map()
  for (const r of reports.value) {
    const key = reportReasonKey(r.reason_type)
    const g = groups.get(key) || { key, label: t(`report.reasons.${key}`), n: 0 }
    g.n += 1
    groups.set(key, g)
  }
  return [...groups.values()].sort((a, b) => b.n - a.n)
})

const detailsOpen = ref(false)
</script>

<style scoped>
.report-summary {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 12px;
}

.report-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 16px;
  font-size: 0.82rem;
}

.report-count {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  color: #f2c97d;
}

.report-time {
  color: rgba(255, 255, 255, 0.45);
}

.report-hidden-hint {
  margin: 0;
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.6);
}

.report-reasons-title {
  margin-bottom: 6px;
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.5);
}

.report-groups {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.report-toggle {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin: 6px 0 0 -8px;
  padding: 4px 8px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: rgba(255, 255, 255, 0.55);
  font-size: 0.8rem;
  cursor: pointer;
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1), color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (hover: hover) {
  .report-toggle:hover {
    background: rgba(102, 234, 194, 0.14);
    color: #8af0d0;
  }
}

.report-list {
  margin: 6px 0 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.report-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 0.84rem;
}

.report-type {
  flex-shrink: 0;
}

.report-reason {
  flex: 1;
  min-width: 0;
  color: rgba(255, 255, 255, 0.75);
  word-break: break-word;
}

.report-row-time {
  flex-shrink: 0;
  margin-left: auto;
  font-size: 0.76rem;
  color: rgba(255, 255, 255, 0.4);
}

.report-more {
  margin: 6px 0 0;
  font-size: 0.76rem;
  color: rgba(255, 255, 255, 0.4);
}
</style>
