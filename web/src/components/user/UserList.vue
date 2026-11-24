<template>
  <div class="user-list">
    <!-- 空状态 -->
    <div v-if="!loading && users.length === 0" class="empty-state">
      <NIcon size="64" :color="'rgba(255,255,255,0.2)'">
        <SearchIcon />
      </NIcon>
      <p class="empty-text">{{ t('user.noUserResults') }}</p>
      <p class="empty-hint">{{ t('common.search') }}</p>
    </div>

    <!-- 用户列表：整行为真实链接，右键/中键可新标签页打开 -->
    <div v-else class="users">
      <SmartLink
        v-for="user in users"
        :key="user.id"
        class="user-item"
        :to="user.id ? `/user/${user.id}` : null"
      >
        <div class="user-avatar">
          <NAvatar
            round
            :size="56"
            :src="user.avatar_url"
            >
            <div v-if="!user.avatar_url">{{ user.username?.charAt(0) }}</div>
          </NAvatar>
        </div>
        <div class="user-info">
          <h3 class="user-name">{{ user.username || t('user.unknownUser') }}</h3>
          <p class="user-email">{{ user.email || '' }}</p>
          <div class="user-stats">
            <span class="stat-item">
              <NIcon size="14">
                <CalendarIcon />
              </NIcon>
              {{ formatDate(user.create_time) }}
            </span>
            <span class="stat-item" v-if="user.role === 1">
              <NIcon size="14">
                <ShieldIcon />
              </NIcon>
              {{ t('user.admin') }}
            </span>
          </div>
        </div>
      </SmartLink>
    </div>

    <!-- 加载更多 -->
    <div v-if="hasMore && !loading" class="load-more-trigger" ref="loadMoreTrigger"></div>

    <!-- 加载状态 -->
    <div v-if="loading" class="loading-state">
      <NSpin size="medium" />
      <p class="loading-text">{{ t('common.loading') }}</p>
    </div>

    <!-- 没有更多 -->
    <div v-if="!hasMore && users.length > 0" class="no-more">
      <p>{{ t('common.noMore') }}</p>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted, watch } from 'vue'
import { NAvatar, NIcon, NSpin } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Search as SearchIcon, Calendar as CalendarIcon, Shield as ShieldIcon } from '@vicons/tabler'
import SmartLink from '@/components/common/SmartLink.vue'

const { t } = useI18n()

const props = defineProps({
  users: {
    type: Array,
    default: () => []
  },
  loading: {
    type: Boolean,
    default: false
  },
  hasMore: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['load-more'])

const loadMoreTrigger = ref(null)
const observer = ref(null)

// 格式化日期
const formatDate = (dateString) => {
  if (!dateString) return ''
  try {
    const date = new Date(dateString)
    if (Number.isNaN(date.getTime())) return dateString
    const now = new Date()
    // 按自然日（本地零点）计算天数差：今天=0、昨天=1；
    // 不取绝对值，未来时间戳不会被误贴「昨天/N天前」标签
    const startOfDay = (d) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
    const diffDays = Math.round((startOfDay(now) - startOfDay(date)) / 86400000)

    if (diffDays <= 0) {
      return t('user.today')
    } else if (diffDays === 1) {
      return t('user.yesterday')
    } else if (diffDays < 30) {
      return t('user.daysAgo', { days: diffDays })
    } else if (diffDays < 365) {
      const months = Math.floor(diffDays / 30)
      return t('user.monthsAgo', { months })
    } else {
      const years = Math.floor(diffDays / 365)
      return t('user.yearsAgo', { years })
    }
  } catch (error) {
    return dateString
  }
}

// 点击用户
// 用户跳转已由行级 SmartLink 承担：真实 <a href>，支持 hover URL / 右键新标签页

// 设置 Intersection Observer 实现无限滚动
const setupObserver = () => {
  // 清理旧的观察器
  if (observer.value) {
    observer.value.disconnect()
  }

  // 只有在有触发元素且有更多数据时才创建观察器
  if (!loadMoreTrigger.value || !props.hasMore || props.loading) {
    return
  }

  observer.value = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting && props.hasMore && !props.loading) {
          emit('load-more')
        }
      })
    },
    {
      rootMargin: '200px',
      threshold: 0
    }
  )

  observer.value.observe(loadMoreTrigger.value)
}

// 清理 Observer
const cleanupObserver = () => {
  if (observer.value && loadMoreTrigger.value) {
    observer.value.unobserve(loadMoreTrigger.value)
    observer.value.disconnect()
    observer.value = null
  }
}

onMounted(() => {
  setupObserver()
})

onUnmounted(() => {
  cleanupObserver()
})

// 监听 users、hasMore 和 loading 变化，重新设置 observer
watch([() => props.users, () => props.hasMore, () => props.loading, loadMoreTrigger], () => {
  setupObserver()
})
</script>

<style scoped>
.user-list {
  width: 100%;
}

/* 空状态 */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 80px 20px;
  color: rgba(255, 255, 255, 0.4);
}

.empty-text {
  margin-top: 16px;
  font-size: 1.1rem;
  font-weight: 500;
  color: rgba(255, 255, 255, 0.6);
}

.empty-hint {
  margin-top: 8px;
  font-size: 0.9rem;
  color: rgba(255, 255, 255, 0.4);
}

/* 用户列表 */
.users {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 20px;
}

.user-item {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  background: rgba(30, 30, 46, 0.6);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 16px;
  cursor: pointer;
  text-decoration: none;
  transition: all 0.3s ease;
}

@media (hover: hover) {
  .user-item:hover {
    background: rgba(40, 40, 60, 0.8);
    border-color: rgba(72, 236, 143, 0.3);
    transform: translateX(4px);
    box-shadow: 0 4px 20px rgba(72, 236, 176, 0.15);
  }
}

.user-avatar {
  flex-shrink: 0;
}

.user-avatar :deep(.n-avatar) {
  /* background: linear-gradient(135deg, #3b82f6 0%, #8b5cf6 50%, #ec4899 100%); */
  color: white;
  font-weight: 700;
  font-size: 1.5rem;
  /* box-shadow: 0 4px 16px rgba(59, 130, 246, 0.4); */
}

.user-info {
  flex: 1;
  min-width: 0;
}

.user-name {
  font-size: 1.1rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.95);
  margin-bottom: 6px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-email {
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.5);
  margin-bottom: 10px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-stats {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.5);
}

/* 触发器 */
.load-more-trigger {
  height: 1px;
  margin-top: 20px;
}

/* 加载状态 */
.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 20px;
  color: rgba(255, 255, 255, 0.5);
}

.loading-text {
  margin-top: 12px;
  font-size: 0.9rem;
}

/* 没有更多 */
.no-more {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 30px 20px;
  color: rgba(255, 255, 255, 0.4);
  font-size: 0.85rem;
}

.no-more::before,
.no-more::after {
  content: '';
  flex: 1;
  height: 1px;
  background: rgba(255, 255, 255, 0.1);
  margin: 0 20px;
}

/* 响应式 */
@media (max-width: 768px) {
  .user-item {
    padding: 14px 16px;
    gap: 12px;
  }

  .user-avatar :deep(.n-avatar) {
    width: 48px !important;
    height: 48px !important;
    font-size: 1.2rem;
  }

  .user-name {
    font-size: 1rem;
  }

  .user-email {
    font-size: 0.8rem;
  }

  .user-stats {
    gap: 12px;
  }
}
</style>
