<template>
  <NConfigProvider :theme="darkTheme" :theme-overrides="themeOverrides">
    <div class="user-profile-page">
      <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
      <AppShell v-model:offset="offset" />

      <!-- 主内容区域 -->
      <div class="main-content" :style="contentStyle">
        <!-- 移动端「我的」：只读资料卡（不提供编辑资料 / 改密码，见 docs/mobile-adaptation-plan.md 6.5） -->
        <div v-if="mobileShell" class="mobile-profile-card">
          <NAvatar :size="64" :src="userInfo.avatar_url || undefined" round>
            <span v-if="!userInfo.avatar_url" class="mobile-avatar-font">{{ userInfo.username?.charAt(0) }}</span>
          </NAvatar>
          <div class="mobile-profile-text">
            <h2 class="mobile-username">{{ userInfo.username || t('user.notSet') }}</h2>
            <p v-if="userInfo.email" class="mobile-email">{{ userInfo.email }}</p>
            <!-- 我的关注数 / 粉丝数 / 互关入口（本人始终可看自己的列表） -->
            <FollowStats
              v-if="myFollowStats"
              class="mobile-follow-stats"
              :user-id="userInfo.id"
              :following-count="myFollowStats.following_count"
              :follower-count="myFollowStats.follower_count">
              <SmartLink class="mutual-link" :to="{ name: 'follow-mutual' }">{{ t('follow.mutualEntry') }}</SmartLink>
            </FollowStats>
          </div>
        </div>

        <!-- 左侧内容区域 -->
        <div class="content-area">
          <!-- 圈子邀请入口（后端 #169 不发邮件，这里是被邀请人的常驻入口） -->
          <MyInvitesEntry />
          <!-- 标签页内容 -->
          <NCard class="tabs-card" :bordered="false">
          <NTabs
            v-model:value="activeTab"
            type="line"
            animated
            size="large"
            class="profile-tabs">

            <!-- 我的帖子 Tab -->
            <NTabPane name="posts" display-directive="show">
              <template #tab>
                <NSpace align="center" :size="6">
                  <span>{{ t('user.myPosts') }}</span>
                  <NTag size="small" :bordered="false" round>{{ postsTotal }}</NTag>
                </NSpace>
              </template>
              <MyPosts
                :active="activeTab === 'posts'"
                @edit="handlePostEdit"
                @delete="handlePostDelete"
                @total-change="handlePostsTotalChange"/>
            </NTabPane>

            <!-- 加入的兴趣圈 Tab -->
            <NTabPane name="groups" display-directive="show">
              <template #tab>
                <NSpace align="center" :size="6">
                  <span>{{ t('user.myGroups') }}</span>
                  <NTag size="small" :bordered="false" round>{{ groupsTotal }}</NTag>
                </NSpace>
              </template>
              <MyGroups :active="activeTab === 'groups'" @total-change="handleGroupsTotalChange"/>
            </NTabPane>

            <!-- 我的收藏 Tab -->
            <NTabPane name="favorites" display-directive="show">
              <template #tab>
                <NSpace align="center" :size="6">
                  <span>{{ t('user.myFavorites') }}</span>
                  <NTag size="small" :bordered="false" round>{{ favoritesTotal }}</NTag>
                </NSpace>
              </template>
              <MyFavorites :active="activeTab === 'favorites'" @total-change="handleFavoritesTotalChange"/>
            </NTabPane>

            <!-- 浏览历史 Tab -->
            <NTabPane name="history" display-directive="show">
              <template #tab>
                <NSpace align="center" :size="6">
                  <span>{{ t('user.browseHistory') }}</span>
                  <NTag size="small" :bordered="false" round>{{ historyTotal }}</NTag>
                </NSpace>
              </template>
              <BrowseHistory
                :active="activeTab === 'history'"
                @total-change="handleHistoryTotalChange"/>
            </NTabPane>
          </NTabs>
        </NCard>
        </div>

        <!-- 移动端隐私设置分组（关注列表公开、私信权限…，各项在 ProfileSettings 内追加） -->
        <ProfileSettings v-if="mobileShell" class="mobile-privacy-settings" />

        <!-- 移动端设置列表：语言 / 协议 / 退出登录（原桌面头像下拉菜单的入口） -->
        <div v-if="mobileShell" class="mobile-settings">
          <div class="settings-title">{{ t('mobile.me.settings') }}</div>
          <div class="settings-row">
            <span>{{ t('mobile.me.language') }}</span>
            <LanguageSwitcher />
          </div>
          <SmartLink class="settings-row" to="/terms">
            <span>{{ t('mobile.me.terms') }}</span>
            <NIcon size="18"><ChevronRight /></NIcon>
          </SmartLink>
          <SmartLink class="settings-row" to="/privacy">
            <span>{{ t('mobile.me.privacy') }}</span>
            <NIcon size="18"><ChevronRight /></NIcon>
          </SmartLink>
          <button type="button" class="settings-row logout-row" @click="confirmLogout">
            {{ t('common.logout') }}
          </button>
        </div>

        <!-- 右侧用户信息栏（移动端由上方只读资料卡替代） -->
        <div v-if="!mobileShell" class="sidebar-area">
          <NCard class="profile-sidebar-card" :bordered="false">
            <!-- 用户头像和名称 -->
            <div class="sidebar-header">
              <NAvatar
                :size="260"
                :src="userInfo.avatar_url"
                round
                class="sidebar-avatar">
                 <div class="avatar-font" 
                 v-if="!userInfo.avatar_url || userInfo.avatar_url == ''">
                 {{ userInfo.username?.charAt(0) }}
                </div>
              </NAvatar>
              <h2 class="sidebar-username">{{ userInfo.username || t('user.notSet') }}</h2>
              <!-- 我的关注数 / 粉丝数 / 互关入口 -->
              <FollowStats
                v-if="myFollowStats"
                class="sidebar-follow-stats"
                :user-id="userInfo.id"
                :following-count="myFollowStats.following_count"
                :follower-count="myFollowStats.follower_count">
                <SmartLink class="mutual-link" :to="{ name: 'follow-mutual' }">{{ t('follow.mutualEntry') }}</SmartLink>
              </FollowStats>
              <NButton type="primary" size="small" @click="handleEditClick" style="width: 100%; margin-top: 16px; border-radius: 20px;">
                <template #icon>
                  <NIcon :size="14">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                    </svg>
                  </NIcon>
                </template>
                {{ t('user.editProfile') }}
              </NButton>
              <NButton quaternary size="small" @click="openPasswordModal" style="width: 100%; margin-top: 8px; border-radius: 20px;">
                {{ t('user.passwordModal.button') }}
              </NButton>
            </div>

            <!-- 用户基本信息 -->
            <div class="sidebar-section">
              <!-- <NText depth="3" style="font-size: 12px; margin-bottom: 8px; display: block;">{{ t('user.basicInfo') }}</NText> -->
              <div class="info-list">
                <div class="info-row">
                  <NText depth="3" style="font-size: 15px; margin-right: 1dvw;">{{ t('user.email') }}</NText>
                  <NText style="font-size: 15px;">{{ userInfo.email || t('user.notSet') }}</NText>
                </div>
                <div class="info-row">
                  <NText depth="3" style="font-size: 15px; margin-right: 1dvw;">{{ t('user.phone') }}</NText>
                  <NText style="font-size: 15px;">{{ userInfo.phone || t('user.notBound') }}</NText>
                </div>
                <div class="info-row">
                  <NText depth="3" style="font-size: 15px; margin-right: 1dvw;">{{ t('user.gender') }}</NText>
                  <NTag :type="getGenderType(userInfo.gender)" size="small" round>
                    {{ getGenderText(userInfo.gender) }}
                  </NTag>
                </div>
                <div class="info-row">
                  <NText depth="3" style="font-size: 15px; margin-right: 1dvw;">{{ t('user.birthday') }}</NText>
                  <NText style="font-size: 15px;">{{ formatDate(userInfo.birthdate) }}</NText>
                </div>
                <div class="info-row">
                  <NText depth="3" style="font-size: 15px; margin-right: 1dvw;">{{ t('user.role') }}</NText>
                  <NTag :type="getRoleType(userInfo.role)" size="small" round>
                    {{ getRoleText(userInfo.role) }}
                  </NTag>
                </div>
                <div class="info-row">
                  <NText depth="3" style="font-size: 15px; margin-right: 1dvw;">{{ t('user.status') }}</NText>
                  <NTag :type="userInfo.status === 1 ? 'success' : 'error'" size="small" round>
                    {{ userInfo.status === 1 ? t('user.normal') : t('user.disabled') }}
                  </NTag>
                </div>
              </div>
            </div>

            <!-- 设置区（隐私设置分组：关注列表公开、私信权限…，各项在 ProfileSettings 内追加） -->
            <ProfileSettings class="sidebar-settings" />
          </NCard>
        </div>
      </div>

      <!-- 编辑个人信息模态框 -->
      <NModal
        v-model:show="showEditModal"
        preset="card"
        :title="t('user.editModal.title')"
        class="edit-modal"
        :mask-closable="false"
        :bordered="false"
        :segmented="{ content: 'soft' }"
        size="huge"
        header-style="font-size: 26px;"
        :style="{ width: '50dvw', borderRadius: '24px' }">
        <NForm ref="formRef" :model="formData" :rules="rules" label-placement="top" size="large">
          <NFormItem :label="t('user.username')" path="username">
            <NInput
              v-model:value="formData.username"
              :placeholder="t('user.editModal.usernamePlaceholder')"
              maxlength="50"
              show-count
              size="large"
            />
          </NFormItem>

          <NFormItem :label="t('user.phone')" path="phone">
            <NInput
              v-model:value="formData.phone"
              :placeholder="t('user.editModal.phonePlaceholder')"
              maxlength="11"
              size="large"
            />
          </NFormItem>

          <NFormItem :label="t('user.gender')" path="gender">
            <NSelect
              v-model:value="formData.gender"
              :options="genderOptions"
              :placeholder="t('user.editModal.genderPlaceholder')"
              size="large"
            />
          </NFormItem>

          <NFormItem :label="t('user.birthday')" path="birthdate">
            <NDatePicker
              v-model:value="formData.birthdate"
              type="date"
              :placeholder="t('user.editModal.birthdayPlaceholder')"
              :is-date-disabled="(timestamp) => timestamp > Date.now()"
              format="yyyy-MM-dd"
              value-format="yyyy-MM-dd"
              style="width: 100%;"
              size="large"
            />
          </NFormItem>

          <NFormItem :label="t('user.avatar')">
            <NUpload
              :max="1"
              :file-list="avatarFileList"
              list-type="image-card"
              :custom-request="handleAvatarUpload"
              @before-upload="beforeAvatarUpload"
              @update:file-list="handleFileListChange"
              accept="image/*"
            >
              <div style="text-align: center;">
                <div>{{ t('user.editModal.clickUploadAvatar') }}</div>
              </div>
            </NUpload>
            <template #feedback>
              <NText depth="3" style="font-size: 12px">{{ t('user.editModal.avatarTip') }}</NText>
            </template>
          </NFormItem>
        </NForm>

        <template #footer>
          <div class="modal-footer">
            <NButton @click="showEditModal = false">{{ t('common.cancel') }}</NButton>
            <NButton type="primary" @click="handleUpdateInfo" :loading="updating">
              {{ t('user.editModal.saveChanges') }}
            </NButton>
          </div>
        </template>
      </NModal>

      <!-- 修改密码 / 设置密码模态框（不校验旧密码；提交后当前会话保持有效） -->
      <NModal
        v-model:show="showPasswordModal"
        preset="card"
        :title="t('user.passwordModal.title')"
        class="edit-modal"
        :mask-closable="false"
        :bordered="false"
        :segmented="{ content: 'soft' }"
        size="huge"
        header-style="font-size: 26px;"
        :style="{ width: '40dvw', borderRadius: '24px' }">
        <NForm ref="passwordFormRef" :model="passwordForm" :rules="passwordRules" label-placement="top" size="large">
          <NFormItem :label="t('user.passwordModal.newPassword')" path="password">
            <NInput
              v-model:value="passwordForm.password"
              type="password"
              show-password-on="click"
              :placeholder="t('user.passwordModal.newPasswordPlaceholder')"
              size="large"
            />
          </NFormItem>
          <NFormItem :label="t('user.passwordModal.confirmPassword')" path="confirmPassword">
            <NInput
              v-model:value="passwordForm.confirmPassword"
              type="password"
              show-password-on="click"
              :placeholder="t('user.passwordModal.confirmPasswordPlaceholder')"
              size="large"
              @keyup.enter="handleResetPassword"
            />
          </NFormItem>
          <NText depth="3" style="font-size: 12px;">{{ t('user.passwordModal.sessionTip') }}</NText>
        </NForm>
        <template #footer>
          <div class="modal-footer">
            <NButton @click="showPasswordModal = false">{{ t('common.cancel') }}</NButton>
            <NButton type="primary" @click="handleResetPassword" :loading="passwordUpdating">
              {{ t('user.editModal.saveChanges') }}
            </NButton>
          </div>
        </template>
      </NModal>

      <!-- 头像裁剪弹窗（1:1，仅预览圆形头像） -->
      <ImageCropperModal
        v-model:show="cropperVisible"
        :image-src="cropperImageSrc"
        :aspect-ratio="1"
        :title="t('user.editModal.cropAvatarTitle')"
        type="avatar"
        avatar-only
        :avatar-url="formData.avatar_url"
        :preview-title="t('user.editModal.previewTitle')"
        :preview-hint="t('user.editModal.previewHint')"
        :upload-handler="handleCropUpload"
        @confirm="handleCropConfirm"
        @cancel="handleCropCancel"
      />
    </div>
  </NConfigProvider>
</template>

<script setup>
import { ref, onMounted, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  NConfigProvider,
  NCard,
  NAvatar,
  NIcon,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NButton,
  NUpload,
  NSelect,
  NDatePicker,
  NTabs,
  NTabPane,
  NSpace,
  NTag,
  NText,
  darkTheme,
  useMessage
} from 'naive-ui'
import AppShell from '@/components/layout/AppShell.vue'
import { useAppShell } from '@/composables/useAppShell'
import { useLogout } from '@/composables/useLogout'
import LanguageSwitcher from '@/components/common/LanguageSwitcher.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import { ChevronRight } from '@vicons/tabler'
import MyPosts from '@/components/user/MyPosts.vue'
import MyGroups from '@/components/user/MyGroups.vue'
import MyInvitesEntry from '@/components/user/MyInvitesEntry.vue'
import MyFavorites from '@/components/user/MyFavorites.vue'
import BrowseHistory from '@/components/user/BrowseHistory.vue'
import ProfileSettings from '@/components/user/ProfileSettings.vue'
import FollowStats from '@/components/follow/FollowStats.vue'
import { getFollowStats } from '@/api/follow'
import ImageCropperModal from '@/components/common/ImageCropperModal.vue'
import { auth } from '@/utils/auth'
import request from '@/utils/request'
import { usePageTitle } from '@/composables/usePageTitle'
import { updateUserInfo, resetPassword } from '@/api/user'
import { useImageUpload } from '@/composables/useImageUpload'
import { useI18n } from 'vue-i18n'

const router = useRouter()
const route = useRoute()
const message = useMessage()
const { t } = useI18n()
const { setTitleData } = usePageTitle()
// 侧栏宽度与内容区偏移（移动端外框下不偏移），见 composables/useAppShell.js
const { offset, mobileShell, contentStyle } = useAppShell()
const { confirmLogout } = useLogout()

// 当前激活的标签页（支持 /profile?tab=groups 等定位）
const VALID_TABS = ['posts', 'groups', 'favorites', 'history']
const initialTab = VALID_TABS.includes(route.query.tab) ? route.query.tab : 'posts'
const activeTab = ref(initialTab)

// 同页 query 变化时同步 tab（路由组件复用不重建）
watch(() => route.query.tab, (v) => {
  if (v && VALID_TABS.includes(v)) activeTab.value = v
})

// 帖子总数（由 MyPosts 通过 total-change 上报，用于 Tab 计数）
const postsTotal = ref(0)
const handlePostsTotalChange = (total) => {
  postsTotal.value = total || 0
}

// 加入的兴趣圈总数（由 MyGroups 通过 total-change 上报，用于 Tab 计数）
const groupsTotal = ref(0)
const handleGroupsTotalChange = (total) => {
  groupsTotal.value = total || 0
}

// 我的收藏总数（由 MyFavorites 通过 total-change 上报，用于 Tab 计数）
const favoritesTotal = ref(0)
const handleFavoritesTotalChange = (total) => {
  favoritesTotal.value = total || 0
}

// 浏览历史总数（由 BrowseHistory 通过 total-change 上报，用于 Tab 计数）
const historyTotal = ref(0)
const handleHistoryTotalChange = (total) => {
  historyTotal.value = total || 0
}

// 用户信息
const userInfo = ref({
  id: null,
  username: '',
  email: '',
  phone: '',
  google_id: '',
  x_id: '',
  github_id: '',
  avatar_url: '',
  gender: 0,
  birthdate: null,
  status: 1,
  role: 0,
  create_time: null,
  update_time: null
})

// 模拟数据
const mockData = ref({
  posts: [
    {
      id: 1,
      title: '分享一个超实用的Vue3组合式API使用技巧',
      content: '最近在项目中使用Vue3的组合式API，发现了一些很有用的技巧，比如利用computed来实现复杂的响应式逻辑...',
      authorName: '张三',
      authorAvatar: 'https://07akioni.oss-cn-beijing.aliyuncs.com/07akioni.jpeg',
      time: '2小时前',
      images: ['https://picsum.photos/400/300?random=1', 'https://picsum.photos/400/300?random=2'],
      tags: ['Vue3', '前端开发'],
      comments: 24,
      likes: 156,
      views: 1234
    },
    {
      id: 2,
      title: 'TypeScript进阶：类型体操实战',
      content: '本文将介绍TypeScript中的高级类型特性，通过实际案例来演示如何使用条件类型、映射类型等...',
      authorName: '李四',
      authorAvatar: 'https://07akioni.oss-cn-beijing.aliyuncs.com/07akioni.jpeg',
      time: '1天前',
      images: [],
      tags: ['TypeScript', '教程'],
      comments: 18,
      likes: 89,
      views: 567
    },
    {
      id: 3,
      title: '2024年前端技术栈选型建议',
      content: '随着前端技术的不断发展，选择合适的技术栈变得越来越重要。本文将分享一些关于技术栈选型的思考...',
      authorName: '王五',
      authorAvatar: 'https://07akioni.oss-cn-beijing.aliyuncs.com/07akioni.jpeg',
      time: '3天前',
      images: ['https://picsum.photos/400/300?random=3'],
      tags: ['前端', '技术选型'],
      comments: 32,
      likes: 234,
      views: 1890
    }
  ],
  favorites: [
    {
      id: 1,
      type: 'post',
      title: '深入理解React Hooks原理',
      description: '全面解析React Hooks的实现机制和使用技巧',
      time: '2024-01-10'
    },
    {
      id: 2,
      type: 'group',
      title: 'JavaScript高级编程',
      description: '深入学习JavaScript核心概念和高级特性',
      time: '2024-01-09'
    },
    {
      id: 3,
      type: 'post',
      title: 'Webpack配置完全指南',
      description: '从零开始学习Webpack的配置和优化',
      time: '2024-01-08'
    }
  ]
})

// 我的关注数 / 粉丝数（GET /follow/stats/:me）；失败不影响页面其余部分
const myFollowStats = ref(null)
const fetchMyFollowStats = async () => {
  if (!userInfo.value.id) return
  try {
    const res = await getFollowStats(userInfo.value.id)
    myFollowStats.value = res?.data || null
  } catch (error) {
    console.error('获取关注统计失败:', error)
  }
}

// 获取用户信息
const fetchUserInfo = async () => {
  try {
    const response = await request.get('/user/get')
    if (response.data) {
      userInfo.value = response.data
      // 数据加载成功后用用户名覆盖标签页标题
      if (userInfo.value.username) {
        setTitleData('title.profileName', { name: userInfo.value.username })
      }
      fetchMyFollowStats()
    }
  } catch (error) {
    console.error(t('user.editModal.fetchFailed') + ':', error)
    message.error(t('user.editModal.fetchFailed'))
  }
}

// 编辑模态框相关
const showEditModal = ref(false)
const updating = ref(false)
const avatarFileList = ref([])
const { uploadOne } = useImageUpload()

// 修改密码模态框相关（不校验旧密码；密码原样提交，不 trim）
const showPasswordModal = ref(false)
const passwordUpdating = ref(false)
const passwordFormRef = ref(null)
const passwordForm = ref({
  password: '',
  confirmPassword: ''
})
const passwordRules = {
  password: {
    required: true,
    trigger: ['blur', 'input'],
    validator: (_rule, value) => {
      if (!value || value.length < 6) {
        return new Error(t('user.passwordModal.lengthError'))
      }
      return true
    }
  },
  confirmPassword: {
    required: true,
    trigger: ['blur', 'input'],
    validator: (_rule, value) => {
      if (value !== passwordForm.value.password) {
        return new Error(t('user.passwordModal.mismatchError'))
      }
      return true
    }
  }
}

// 头像裁剪弹窗状态（选图后先裁剪再上传，与创建圈子流程一致）
const cropperVisible = ref(false)
const cropperImageSrc = ref('')
// pendingCrop 缓存本次裁剪上下文：NUpload 的 file 与 onFinish/onError
const pendingCrop = ref(null)

// 性别选项
const genderOptions = computed(() => [
  { label: t('user.genders.notSet'), value: 0 },
  { label: t('user.genders.male'), value: 1 },
  { label: t('user.genders.female'), value: 2 },
  { label: t('user.genders.other'), value: 3 }
])

// 表单数据
const formData = ref({
  username: '',
  phone: '',
  gender: 0,
  birthdate: null,
  avatar_url: ''
})

// 表单验证规则
const rules = {
  username: {
    required: false,
    trigger: ['blur', 'input'],
    validator: (_rule, value) => {
      if (!value) return true
      if (value.length < 1 || value.length > 50) {
        return new Error(t('user.editModal.usernameLengthError'))
      }
      return true
    }
  },
  phone: {
    required: false,
    trigger: ['blur', 'input'],
    validator: (_rule, value) => {
      if (!value) return true
      const phoneReg = /^1[3-9]\d{9}$/
      if (!phoneReg.test(value)) {
        return new Error(t('user.editModal.phoneFormatError'))
      }
      return true
    }
  }
}

// 上传前校验
const beforeAvatarUpload = (data) => {
  const file = data.file
  const allowedTypes = ['image/jpeg', 'image/jpg', 'image/png', 'image/gif', 'image/webp', 'image/svg+xml']
  const isValidType = allowedTypes.includes(file.type)

  if (!isValidType) {
    message.error(t('user.editModal.avatarTypeError'))
    return false
  }

  const maxSize = 10 * 1024 * 1024
  if (file.size > maxSize) {
    message.error(t('user.editModal.avatarSizeError'))
    return false
  }

  return true
}

// 处理文件列表变化
const handleFileListChange = (fileList) => {
  avatarFileList.value = fileList
}

// 选图后不直接上传，先打开裁剪弹窗（1:1）
const handleAvatarUpload = ({ file, onFinish, onError }) => {
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
  }
  pendingCrop.value = { file, onFinish, onError }
  cropperImageSrc.value = URL.createObjectURL(file.file)
  cropperVisible.value = true
}

// 裁剪确认 → 在弹窗内上传裁剪结果（由 ImageCropperModal 调用）
const handleCropUpload = async (blob, { onProgress } = {}) => {
  const croppedFile = new File([blob], 'cropped-avatar.jpg', { type: 'image/jpeg' })
  try {
    const url = await uploadOne(croppedFile, { onProgress })
    message.success(t('user.editModal.avatarUploadSuccess'))
    return url
  } catch (error) {
    console.error('裁剪后上传失败:', error)
    message.error(t('user.editModal.avatarUploadFailed'))
    throw error // rethrow → 弹窗保持打开，供重试
  }
}

// 上传成功 → 回填头像 URL 并结束本次裁剪
const handleCropConfirm = (payload) => {
  const p = pendingCrop.value
  if (!p) return
  const url = payload?.url
  if (!url) return
  formData.value.avatar_url = url
  p.onFinish()
  pendingCrop.value = null
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
    cropperImageSrc.value = ''
  }
}

// 取消裁剪 → 放弃本次上传
const handleCropCancel = () => {
  const p = pendingCrop.value
  if (p) p.onError()
  pendingCrop.value = null
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
    cropperImageSrc.value = ''
  }
}

// 更新用户信息
const handleUpdateInfo = async () => {
  try {
    // 与 openEditModal 的种子值逐字段对比，只有真正变化才算修改
    // （此前 gender 恒有定义导致 hasChanges 永远为 true）
    const currentBirthdateTs = userInfo.value.birthdate
      ? new Date(userInfo.value.birthdate).getTime()
      : null
    const hasChanges =
      formData.value.username !== (userInfo.value.username || '') ||
      formData.value.phone !== (userInfo.value.phone || '') ||
      formData.value.avatar_url !== (userInfo.value.avatar_url || '') ||
      formData.value.gender !== (userInfo.value.gender ?? 0) ||
      formData.value.birthdate !== currentBirthdateTs

    if (!hasChanges) {
      message.warning(t('user.editModal.noChangesWarning'))
      return
    }

    updating.value = true

    const updateData = {}

    if (formData.value.username) {
      updateData.username = formData.value.username
    }
    if (formData.value.phone) {
      updateData.phone = formData.value.phone
    }
    if (formData.value.avatar_url) {
      updateData.avatar_url = formData.value.avatar_url
    }
    if (formData.value.gender !== undefined && formData.value.gender !== null) {
      updateData.gender = formData.value.gender
    }
    if (formData.value.birthdate) {
      updateData.birthdate = new Date(formData.value.birthdate).toISOString()
    }

    await updateUserInfo(updateData)
    message.success(t('user.editModal.updateSuccess'))
    showEditModal.value = false
    avatarFileList.value = []
    await fetchUserInfo()
  } catch (error) {
    console.error('更新个人信息失败:', error)
    message.error(error.message || t('user.editModal.updateFailed'))
  } finally {
    updating.value = false
  }
}

// 打开编辑模态框
const openEditModal = () => {
  let birthdateTimestamp = null
  if (userInfo.value.birthdate) {
    birthdateTimestamp = new Date(userInfo.value.birthdate).getTime()
  }

  formData.value = {
    username: userInfo.value.username || '',
    phone: userInfo.value.phone || '',
    gender: userInfo.value.gender ?? 0,
    birthdate: birthdateTimestamp,
    avatar_url: userInfo.value.avatar_url || ''
  }

  if (userInfo.value.avatar_url) {
    avatarFileList.value = [{
      id: 'avatar',
      name: 'avatar.jpg',
      status: 'finished',
      url: userInfo.value.avatar_url
    }]
  } else {
    avatarFileList.value = []
  }

  showEditModal.value = true
}

// 编辑按钮点击事件
const handleEditClick = () => {
  openEditModal()
}

// 打开修改密码弹窗
const openPasswordModal = () => {
  passwordForm.value = { password: '', confirmPassword: '' }
  showPasswordModal.value = true
}

// 提交重置密码（本地先校验减少 400 往返；不校验旧密码；成功后不踢登录）
const handleResetPassword = async () => {
  try {
    await passwordFormRef.value?.validate()
  } catch {
    return
  }
  if (passwordForm.value.password.length < 6) {
    message.error(t('user.passwordModal.lengthError'))
    return
  }
  if (passwordForm.value.password !== passwordForm.value.confirmPassword) {
    message.error(t('user.passwordModal.mismatchError'))
    return
  }
  passwordUpdating.value = true
  try {
    await resetPassword(passwordForm.value.password, passwordForm.value.confirmPassword)
    message.success(t('user.passwordModal.updateSuccess'))
    showPasswordModal.value = false
  } catch (error) {
    console.error('修改密码失败:', error)
    message.error(error.message || t('user.passwordModal.updateFailed'))
  } finally {
    passwordUpdating.value = false
  }
}

// 帖子操作
const handlePostEdit = (post) => {
  message.info(`编辑帖子: ${post.title}`)
}

const handlePostDelete = (post) => {
  message.warning(`删除帖子: ${post.title}`)
}

// 收藏操作
const handleFavoriteRemove = (item) => {
  message.info(`移除收藏: ${item.title}`)
}

const handleFavoriteClick = (item) => {
  message.info(`查看收藏: ${item.title}`)
}

// 格式化日期
const formatDate = (dateStr) => {
  if (!dateStr) return t('user.notSet')
  const date = new Date(dateStr)
  return date.toLocaleDateString('zh-CN')
}

// 获取性别文本
const getGenderText = (gender) => {
  const genderMap = {
    0: t('user.genders.notSet'),
    1: t('user.genders.male'),
    2: t('user.genders.female'),
    3: t('user.genders.other')
  }
  return genderMap[gender] || t('user.genders.notSet')
}

// 获取性别标签类型
const getGenderType = (gender) => {
  const typeMap = {
    0: 'default',
    1: 'info',
    2: 'error',
    3: 'warning'
  }
  return typeMap[gender] || 'default'
}

// 获取角色文本
const getRoleText = (role) => {
  const roleMap = {
    0: t('user.roles.user'),
    1: t('user.roles.admin'),
    2: t('user.roles.agentBot')
  }
  return roleMap[role] || t('user.roles.user')
}

// 获取角色标签类型
const getRoleType = (role) => {
  const typeMap = {
    0: 'default',
    1: 'warning',
    2: 'info'
  }
  return typeMap[role] || 'default'
}

onMounted(() => {
  if (!auth.isAuthenticated()) {
    message.warning(t('messages.pleaseLoginFirst'))
    router.push('/')
    return
  }
  fetchUserInfo()
})
</script>

<style scoped>
.user-profile-page {
  min-height: 100vh;
  position: relative;
  background: linear-gradient(135deg, #0f0f1a 0%, #1a1a2e 100%);
}

.main-content {
  margin-top: var(--header-height);
  padding: 24px;
  min-height: calc(100vh - var(--header-height));
  transition: margin-left 0.3s ease, width 0.3s ease;
  display: flex;
  gap: 24px;
  justify-content: center;
}

/* 左侧内容区域 */
.content-area {
  flex: 1;
  min-width: 25dvw;
  max-width: 55dvw;
}

/* 右侧边栏区域 */
.sidebar-area {
  width: 20dvw;
  flex-shrink: 0;
}

/* 右侧栏卡片 - 无边框设计 */
.profile-sidebar-card {
  position: sticky;
  background: transparent !important;
  border: none !important;
  box-shadow: none !important;
}

/* 侧边栏头部 */
.sidebar-header {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding-bottom: 24px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  margin-bottom: 24px;
}

.sidebar-avatar {
  border: 4px solid rgba(255, 255, 255, 0.12);
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
  margin-bottom: 16px;
}

.sidebar-username {
  font-size: 1.75rem;
  font-weight: 800;
  color: rgba(255, 255, 255, 0.95);
  margin: 0;
  background: rgba(78, 233, 158, 0.896);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  text-align: center;
}

/* 侧边栏区块 */
.sidebar-section {
  margin-bottom: 20px;
}

.sidebar-section:last-child {
  margin-bottom: 0;
}

/* 信息列表 */
.info-list {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.info-row {
  display: flex;
  /* padding: 5px 5px; */
  /* background: rgba(255, 255, 255, 0.02); */
  border-radius: 10px;
  transition: background 0.2s;
}

@media (hover: hover) {
  .info-row:hover {
    background: rgba(255, 255, 255, 0.04);
  }
}

/* 绑定列表 */
.binding-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.binding-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  background: rgba(255, 255, 255, 0.02);
  border-radius: 8px;
  transition: all 0.2s;
}

@media (hover: hover) {
  .binding-row:hover {
    background: rgba(255, 255, 255, 0.04);
  }
}

.binding-row.bound {
  background: rgba(66, 184, 131, 0.05);
}

@media (hover: hover) {
  .binding-row.bound:hover {
    background: rgba(66, 184, 131, 0.08);
  }
}

.binding-left {
  display: flex;
  align-items: center;
}

/* 标签页卡片 */
.tabs-card {
  background: rgba(255, 255, 255, 0.03) !important;
  border: 1px solid rgba(255, 255, 255, 0.08) !important;
  backdrop-filter: blur(20px);
}

.profile-tabs {
  padding: 0 8px;
}

.profile-tabs :deep(.n-tab-pane) {
  padding: 24px 0 0 0;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

.avatar-font{
  font-size: 10dvw;
  /* margin-bottom: 3dvh; */
  font-family: 'Gill Sans', 'Gill Sans MT', Calibri, 'Trebuchet MS', sans-serif;
}

/* 响应式 */
@media (max-width: 1024px) {
  .main-content {
    flex-direction: column;
  }

  .sidebar-area {
    width: 100%;
    order: -1;
  }

  .profile-sidebar-card {
    position: static;
  }
}

@media (max-width: 768px) {
  .main-content {
    margin-left: 0;
    padding: 16px;
  }
}

/* ===== 移动端「我的」 ===== */
@media (max-width: 768px) {
  /* 帖子列表左右通栏：页面与标签页卡片都不留左右边距，资料卡 / 设置列表自留 12px */
  .main-content {
    padding: 12px 0;
    gap: 12px;
  }

  .content-area {
    min-width: 0;
    max-width: none;
  }

  .mobile-profile-card,
  .mobile-privacy-settings,
  .mobile-settings {
    margin: 0 12px;
  }

  .tabs-card {
    border-radius: 0 !important;
    border-left: none !important;
    border-right: none !important;
  }

  .tabs-card :deep(.n-card__content) {
    padding: 0 0 8px;
  }

  .profile-tabs {
    padding: 0;
  }

  .profile-tabs :deep(.n-tabs-nav) {
    padding: 0 12px;
  }

  .profile-tabs :deep(.n-tab-pane) {
    padding: 12px 0 0;
  }
}

.mobile-profile-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 16px;
  border-radius: 16px;
  background: var(--glass-bg);
  border: 1px solid var(--glass-border);
}

.mobile-avatar-font {
  font-size: 26px;
  font-weight: 600;
}

.mobile-profile-text {
  min-width: 0;
}

.mobile-username {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mobile-email {
  margin-top: 4px;
  font-size: 13px;
  color: var(--text-tertiary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 关注数 / 粉丝数 / 互关入口 */
.sidebar-follow-stats {
  margin-top: 12px;
}

.sidebar-settings {
  margin-top: 24px;
}

.mobile-follow-stats {
  justify-content: flex-start;
  gap: 4px;
  margin: 6px 0 0 -8px;
}

.mutual-link {
  padding: 4px 8px;
  border-radius: 8px;
  font-size: 13px;
  color: var(--text-secondary);
  text-decoration: none;
  transition: background 0.2s ease, color 0.2s ease;
}

@media (hover: hover) {
  .mutual-link:hover {
    background: rgba(102, 234, 194, 0.14);
    color: #8af0d0;
  }
}

.mobile-settings {
  border-radius: 16px;
  background: var(--glass-bg);
  border: 1px solid var(--glass-border);
  overflow: hidden;
}

.settings-title {
  padding: 14px 16px 6px;
  font-size: 13px;
  color: var(--text-tertiary);
}

.settings-row {
  width: 100%;
  min-height: 52px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border: none;
  border-top: 1px solid var(--glass-border);
  background: transparent;
  color: var(--text-primary);
  font-size: 15px;
  text-decoration: none;
  cursor: pointer;
}

.settings-row:active {
  background: rgba(255, 255, 255, 0.04);
}

.logout-row {
  justify-content: center;
  color: #e5484d;
  font-family: inherit;
}
</style>

<!--
  编辑模态框样式：使用非 scoped 全局块。
  NModal 会将内容 teleport 到 body，scoped + class 前缀的 :deep 选择器无法命中
  teleport 出去的 DOM（class 与 data-v 落在不同元素上）。这里用 .edit-modal 前缀
  做范围限定，保证只影响本弹窗，且能稳定穿透 teleport。
-->
<style>
/* ===== 卡片整体：圆润 + 玻璃质感 ===== */
.edit-modal .n-card {
  border-radius: 24px !important;
  box-shadow: var(--shadow-lg) !important;
  background: var(--glass-bg) !important;
  border: 1px solid var(--glass-border) !important;
  overflow: hidden;
}

.edit-modal .n-card-header {
  border-radius: 24px 24px 0 0 !important;
}

.edit-modal .n-card-header__main,
.edit-modal .n-card-header__title {
  font-size: 26px !important;
  font-weight: 700 !important;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.edit-modal .n-form-item {
  margin-bottom: 5px;
}

.edit-modal .n-form-item-label {
  color: rgba(255, 255, 255, 0.8) !important;
  font-size: 14px !important;
}

/* ===== 输入框 / 选择框 / 日期：更高、更圆 ===== */
.edit-modal .n-input,
.edit-modal .n-base-selection {
  --n-border-radius: 14px !important;
  --n-height: 52px !important;
  font-size: 15px;
}

.edit-modal .n-base-selection-label {
  min-height: 52px !important;
}

.edit-modal .n-input__textarea-el {
  font-size: 15px;
  line-height: 1.7;
}

/* 聚焦态：绿色边框 + 柔光晕（与创建圈子一致） */
.edit-modal .n-input--focus,
.edit-modal .n-base-selection:focus-within {
  --n-border-focus: 1px solid #18a058 !important;
  --n-box-shadow-focus: 0 0 0 3px rgba(24, 160, 88, 0.18) !important;
}

/* ===== 按钮：圆角 + 高度，主按钮绿色渐变 ===== */
.edit-modal .n-button {
  --n-border-radius: 14px !important;
  --n-height: 46px !important;
  transition: all 0.2s ease;
}

.edit-modal .n-button--primary-type {
  background: var(--primary-gradient) !important;
  border: none !important;
  color: #07140d !important;
  font-weight: 600 !important;
}

@media (hover: hover) {
  .edit-modal .n-button--primary-type:hover {
    transform: translateY(-1px);
    box-shadow: 0 8px 20px rgba(34, 179, 106, 0.35);
    opacity: 0.95;
  }
}

/* ===== 图片上传卡片：圆润化 ===== */
.edit-modal .n-upload-trigger--image-card,
.edit-modal .n-upload-file-list__item {
  border-radius: 16px !important;
  transition: all 0.2s ease;
}

@media (hover: hover) {
  .edit-modal .n-upload-trigger--image-card:hover {
    border-color: #18a058 !important;
    color: #18a058 !important;
  }
}
</style>
