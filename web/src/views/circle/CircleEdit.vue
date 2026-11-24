<template>
  <div class="circle-edit-page">
    <!-- 顶栏 -->
    <AppHeader />

    <!-- 侧边栏 -->
    <SideNav @collapsed="offset = 64" @expanded="offset = 260" />

    <!-- 主内容区域 -->
    <div class="main-content" :style="{ 'margin-left': `${offset}px`, width: `calc(100% - ${offset}px)` }">
      <!-- 页面级加载骨架 -->
      <div v-if="pageLoading" class="page-skeleton">
        <div class="sk-line sk-line--title"></div>
        <div class="sk-card" v-for="n in 2" :key="n">
          <div class="sk-line sk-line--full"></div>
          <div class="sk-line sk-line--full"></div>
          <div class="sk-line sk-line--short"></div>
        </div>
      </div>

      <template v-else>
        <div class="edit-container">
          <!-- 管理页共享页头：返回 + 圈子信息 + 角色 + 成员管理/编辑资料切换 -->
          <CircleAdminHeader active="edit" :circle="circle" />

          <NForm
            ref="formRef"
            :model="form"
            :rules="rules"
            label-placement="top"
            class="edit-form"
            size="large"
          >
            <!-- 圈主专属字段（仅 owner 渲染） -->
            <div v-if="isOwner(myRole)" class="edit-section">
              <div class="section-head">
                <h3 class="section-title">{{ t('circle.edit.ownerSection') }}</h3>
                <p class="section-desc">{{ t('circle.edit.ownerSectionDesc') }}</p>
              </div>

              <NFormItem :label="t('circle.form.name')" path="name">
                <NInput
                  v-model:value="form.name"
                  :placeholder="t('circle.form.namePlaceholder')"
                  maxlength="50"
                  show-count
                />
              </NFormItem>

              <NFormItem :label="t('circle.form.slug')" path="slug">
                <NInput v-model:value="form.slug" maxlength="60" :placeholder="t('circle.form.slugPlaceholder')" />
                <template #feedback>
                  <NText depth="3" style="font-size: 12px">{{ t('circle.edit.slugClearHint') }}</NText>
                </template>
              </NFormItem>

              <NFormItem :label="t('circle.form.category')" path="category_id">
                <NSelect
                  v-model:value="form.category_id"
                  :options="categoryOptions"
                  :loading="loadingCategories"
                  :placeholder="t('circle.form.categoryPlaceholder')"
                  clearable
                  filterable
                />
              </NFormItem>

              <NFormItem :label="t('circle.form.joinType')" path="join_type">
                <NRadioGroup v-model:value="form.join_type" name="join_type">
                  <NSpace vertical :size="12">
                    <NRadio :value="0">
                      <NText strong>{{ t('circle.form.joinTypeDirect') }}</NText>
                      <NText depth="3" style="margin-left: 8px">{{ t('circle.form.joinTypeDirectDesc') }}</NText>
                    </NRadio>
                    <NRadio :value="2">
                      <NText strong>{{ t('circle.form.joinTypePrivate') }}</NText>
                      <NText depth="3" style="margin-left: 8px">{{ t('circle.form.joinTypePrivateDesc') }}</NText>
                    </NRadio>
                  </NSpace>
                </NRadioGroup>
              </NFormItem>
            </div>

            <!-- 管理员可改字段 -->
            <div class="edit-section">
              <div class="section-head">
                <h3 class="section-title">{{ t('circle.edit.adminSection') }}</h3>
                <p class="section-desc">{{ t('circle.edit.adminSectionDesc') }}</p>
              </div>

              <NFormItem :label="t('circle.form.avatar')" path="avatar_url">
                <div class="image-field">
                  <!-- image-card 上传（参考 UserProfile 修改头像交互）：
                       图片即卡片，预览/删除为悬浮角标；max=1 时上传卡自动隐藏 -->
                  <NUpload
                    class="image-card-upload"
                    :max="1"
                    :file-list="avatarFileList"
                    list-type="image-card"
                    accept="image/*"
                    :custom-request="handleUploadAvatar"
                    @update:file-list="handleAvatarChange"
                  >
                    <div class="upload-trigger">
                      <NIcon :size="20"><UploadIcon /></NIcon>
                      <span>{{ t('circle.form.avatarUpload') }}</span>
                    </div>
                  </NUpload>
                  <NText depth="3" style="font-size: 12px">{{ t('circle.form.avatarTip') }}</NText>
                </div>
              </NFormItem>

              <NFormItem :label="t('circle.form.cover')" path="cover_url">
                <div class="image-field">
                  <NUpload
                    class="image-card-upload"
                    :max="1"
                    :file-list="coverFileList"
                    list-type="image-card"
                    accept="image/*"
                    :custom-request="handleUploadCover"
                    @update:file-list="handleCoverChange"
                  >
                    <div class="upload-trigger">
                      <NIcon :size="20"><UploadIcon /></NIcon>
                      <span>{{ t('circle.form.coverUpload') }}</span>
                    </div>
                  </NUpload>
                  <NText depth="3" style="font-size: 12px">{{ t('circle.form.coverTip') }}</NText>
                </div>
              </NFormItem>

              <NFormItem :label="t('circle.form.description')" path="description">
                <NInput
                  v-model:value="form.description"
                  type="textarea"
                  :placeholder="t('circle.form.descriptionPlaceholder')"
                  :autosize="{ minRows: 4, maxRows: 8 }"
                  maxlength="2000"
                  show-count
                />
              </NFormItem>

              <NFormItem :label="t('circle.form.rules')" path="rule">
                <NInput
                  v-model:value="form.rule"
                  type="textarea"
                  :placeholder="t('circle.form.rulesPlaceholder')"
                  :autosize="{ minRows: 3, maxRows: 8 }"
                  maxlength="2000"
                  show-count
                />
                <template #feedback>
                  <NText depth="3" style="font-size: 12px">{{ t('circle.edit.ruleClearHint') }}</NText>
                </template>
              </NFormItem>
            </div>

            <!-- 底部操作 -->
            <div class="edit-footer">
              <span class="changed-hint">
                {{ changedFields.length > 0
                  ? t('circle.edit.changedCount', { n: changedFields.length })
                  : t('circle.edit.noChanges') }}
              </span>
              <div class="footer-actions">
                <NButton quaternary round :disabled="saving" @click="router.push(`/circle/${circleId}`)">
                  {{ t('common.cancel') }}
                </NButton>
                <NButton
                  type="primary"
                  round
                  :disabled="changedFields.length === 0"
                  :loading="saving"
                  @click="handleSave"
                >
                  {{ t('common.save') }}
                </NButton>
              </div>
            </div>
            <p class="cache-hint">{{ t('circle.edit.cacheHint') }}</p>
          </NForm>
        </div>
      </template>
    </div>

    <!-- 图片裁剪弹窗（头像 1:1 / 封面 6:1） -->
    <ImageCropperModal
      v-model:show="cropperVisible"
      :image-src="cropperImageSrc"
      :aspect-ratio="cropperAspectRatio"
      :title="cropperTitle"
      :type="cropperType"
      :circle-name="form.name"
      :cover-url="form.cover_url"
      :avatar-url="form.avatar_url"
      :slug="form.slug"
      :cover-aspect="COVER_ASPECT_RATIO"
      :upload-handler="handleCropUpload"
      @confirm="handleCropConfirm"
      @cancel="handleCropCancel"
    />
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NForm, NFormItem, NInput, NButton, NText, NUpload, NIcon,
  NRadioGroup, NRadio, NSelect, NSpace, useMessage
} from 'naive-ui'
import { Upload as UploadIcon } from '@vicons/tabler'
import { useI18n } from 'vue-i18n'
import AppHeader from '@/components/layout/AppHeader.vue'
import SideNav from '@/components/layout/SideNav.vue'
import ImageCropperModal from '@/components/common/ImageCropperModal.vue'
import CircleAdminHeader from '@/components/circle/CircleAdminHeader.vue'
import { getCircleDetail, getCategories, updateCircle } from '@/api/circle'
import { useImageUpload } from '@/composables/useImageUpload'
import { usePageTitle } from '@/composables/usePageTitle'
import { EMPTY_CATEGORY_ID, isManager, isOwner, normalizeJoinType } from '@/constants/circle'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const { t } = useI18n()
const { setTitleData } = usePageTitle()

const offset = ref(260)
const circleId = computed(() => route.params.id)

const circle = ref({})
const myRole = computed(() => circle.value.member_role || 0)
const pageLoading = ref(true)
const saving = ref(false)

// 初始快照：diff 基准，只提交与快照不同的字段
const initial = reactive({
  name: '',
  slug: '',
  join_type: 0,
  category_id: null,
  avatar_url: '',
  cover_url: '',
  description: '',
  rule: ''
})

const form = reactive({
  name: '',
  slug: '',
  join_type: 0,
  category_id: null,
  avatar_url: '',
  cover_url: '',
  description: '',
  rule: ''
})

const formRef = ref(null)

// 各角色可编辑字段（与后端 §5.9 权限矩阵一致）
const OWNER_FIELDS = ['name', 'slug', 'join_type', 'category_id']
const ADMIN_FIELDS = ['avatar_url', 'cover_url', 'description', 'rule']
const editableFields = computed(() =>
  isOwner(myRole.value) ? [...OWNER_FIELDS, ...ADMIN_FIELDS] : ADMIN_FIELDS
)

// 变更字段：清空分类时 form.category_id 为 null，提交时转全零 UUID
const changedFields = computed(() =>
  editableFields.value.filter((field) => {
    const a = form[field]
    const b = initial[field]
    if (field === 'category_id') return (a || null) !== (b || null)
    return (a ?? '') !== (b ?? '')
  })
)

// 分类选项
const categoryOptions = ref([])
const loadingCategories = ref(false)

const loadCategories = async () => {
  try {
    loadingCategories.value = true
    const res = await getCategories()
    if (res.code === 200 && res.data) {
      categoryOptions.value = res.data.map(cat => ({ label: cat.name, value: cat.id }))
    }
  } catch {
    // 分类加载失败不阻塞编辑，仅下拉为空
  } finally {
    loadingCategories.value = false
  }
}

// 校验规则（提交前本地校验，后端 201 仅兜底；name 长度按文档 1-50）
const rules = computed(() => ({
  name: [
    { required: true, message: t('circle.form.validation.nameRequired'), trigger: 'blur' },
    { min: 1, max: 50, message: t('circle.edit.validation.nameLength'), trigger: 'blur' }
  ],
  slug: [
    { pattern: /^[a-z0-9-]*$/, message: t('circle.form.validation.slugPattern'), trigger: 'blur' },
    { max: 60, message: t('circle.form.validation.slugMaxLength'), trigger: 'blur' }
  ],
  description: [
    { required: true, message: t('circle.form.validation.descriptionRequired'), trigger: 'blur' },
    { max: 2000, message: t('circle.edit.validation.descriptionMax'), trigger: 'blur' }
  ],
  rule: [
    { max: 2000, message: t('circle.edit.validation.ruleMax'), trigger: 'blur' }
  ]
}))

// ---------- 图片上传（与 CreateCircleModal 同一套裁剪链路） ----------

const { uploadOne } = useImageUpload()

// 封面裁剪比例与 CircleDetail 头部一致（≈7:1，取 6 兼顾可裁剪性）
const COVER_ASPECT_RATIO = 6

const avatarFileList = ref([])
const coverFileList = ref([])

const cropperVisible = ref(false)
const cropperImageSrc = ref('')
const pendingCrop = ref(null)
const cropperAspectRatio = computed(() => (pendingCrop.value?.type === 'cover' ? COVER_ASPECT_RATIO : 1))
const cropperTitle = computed(() =>
  pendingCrop.value?.type === 'cover'
    ? t('circle.form.cropCoverTitle')
    : t('circle.form.cropAvatarTitle')
)
const cropperType = computed(() => pendingCrop.value?.type || 'avatar')

// image-card 列表与表单字段同步：列表被清空（悬浮角标删除 / 裁剪取消）即清空字段，
// 回填 finished 项则由 fillForm / handleCropConfirm 负责
const handleAvatarChange = (options) => {
  avatarFileList.value = options
  if (options.length === 0) form.avatar_url = ''
}

const handleCoverChange = (options) => {
  coverFileList.value = options
  if (options.length === 0) form.cover_url = ''
}

// 选图后先裁剪再上传
const openCropper = (type, file, onFinish, onError) => {
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
  }
  pendingCrop.value = { type, file, onFinish, onError }
  cropperImageSrc.value = URL.createObjectURL(file.file)
  cropperVisible.value = true
}

const handleUploadAvatar = ({ file, onFinish, onError }) => {
  openCropper('avatar', file, onFinish, onError)
}

const handleUploadCover = ({ file, onFinish, onError }) => {
  openCropper('cover', file, onFinish, onError)
}

const handleCropUpload = async (blob, { onProgress } = {}) => {
  const p = pendingCrop.value
  const croppedFile = new File([blob], `cropped-${p.type}.jpg`, { type: 'image/jpeg' })
  try {
    return await uploadOne(croppedFile, { onProgress })
  } catch (error) {
    console.error('裁剪后上传失败:', error)
    message.error(t('circle.form.messages.uploadFailedRetry'))
    throw error // rethrow → 裁剪弹窗保持打开，供重试
  }
}

const handleCropConfirm = (payload) => {
  const p = pendingCrop.value
  if (!p) return
  const url = payload?.url
  if (!url) return
  // 以 finished 项回填列表：image-card 直接展示服务端图片，上传卡随 max=1 隐藏
  if (p.type === 'avatar') {
    form.avatar_url = url
    avatarFileList.value = [{ id: `avatar-${Date.now()}`, name: 'avatar.jpg', status: 'finished', url }]
  } else {
    form.cover_url = url
    coverFileList.value = [{ id: `cover-${Date.now()}`, name: 'cover.jpg', status: 'finished', url }]
  }
  p.onFinish()
  pendingCrop.value = null
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
    cropperImageSrc.value = ''
  }
}

const handleCropCancel = () => {
  const p = pendingCrop.value
  if (p) {
    p.onError()
    // onError 会把本次文件标记为 error 留在列表里，image-card 下会残留失败卡片；
    // 用户是主动取消，直接从列表移除，恢复到裁剪前的状态
    avatarFileList.value = avatarFileList.value.filter((f) => f.id !== p.file.id)
    coverFileList.value = coverFileList.value.filter((f) => f.id !== p.file.id)
  }
  pendingCrop.value = null
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
    cropperImageSrc.value = ''
  }
}

// ---------- 初始化与保存 ----------

// 用详情数据填充表单与快照（后端 category_id 为全零 UUID 时归一为 null）
const fillForm = () => {
  const d = circle.value
  const next = {
    name: d.name || '',
    slug: d.slug || '',
    // 已下线的「需审核」(1) 按私密回填，初始快照同值，不会被当作改动提交
    join_type: normalizeJoinType(d.join_type),
    category_id: (d.category_id && d.category_id !== EMPTY_CATEGORY_ID) ? d.category_id : null,
    avatar_url: d.avatar_url || '',
    cover_url: d.cover_url || '',
    description: d.description || '',
    rule: d.rule || ''
  }
  Object.keys(next).forEach((key) => {
    initial[key] = next[key]
    form[key] = next[key]
  })
  // 已有图片以 finished 项回填 image-card；删除走卡片悬浮角标（handleAvatarChange 同步字段）
  avatarFileList.value = form.avatar_url
    ? [{ id: 'avatar', name: 'avatar.jpg', status: 'finished', url: form.avatar_url }]
    : []
  coverFileList.value = form.cover_url
    ? [{ id: 'cover', name: 'cover.jpg', status: 'finished', url: form.cover_url }]
    : []
}

// 世代守卫：同组件复用切换圈子时，旧请求的响应直接丢弃，
// 不得覆盖 circle、触发跳转/填表或提前结束骨架屏
let initPageGen = 0

const initPage = async () => {
  const gen = ++initPageGen
  pageLoading.value = true
  try {
    const res = await getCircleDetail(circleId.value)
    if (gen !== initPageGen) return
    circle.value = res.data || {}
    // 页面内自查角色：非管理侧退回详情页
    if (!isManager(circle.value.member_role)) {
      message.error(t('circle.manage.noPermissionVisit'))
      router.replace(`/circle/${circleId.value}`)
      return
    }
    fillForm()
    loadCategories()
    // 通过角色校验后用圈子名覆盖标签页标题
    if (circle.value.name) {
      setTitleData('title.circleEditName', { name: circle.value.name })
    }
  } catch (error) {
    if (gen !== initPageGen) return
    console.error('加载圈子信息失败:', error)
    message.error(t('messages.getDetailFailed', { error: error.message || t('common.unknownError') }))
    router.replace(`/circle/${circleId.value}`)
  } finally {
    if (gen === initPageGen) {
      pageLoading.value = false
    }
  }
}

// 只携带变更字段；一个字段都不传后端返回 400，故无变更时按钮已禁用
const handleSave = async () => {
  try {
    await formRef.value?.validate()
  } catch {
    message.warning(t('circle.form.validation.pleaseComplete'))
    return
  }

  const payload = { circle_id: circleId.value }
  changedFields.value.forEach((field) => {
    if (field === 'category_id') {
      payload.category_id = form.category_id || EMPTY_CATEGORY_ID
    } else if (field === 'join_type') {
      payload.join_type = form.join_type
    } else if (typeof form[field] === 'string') {
      payload[field] = form[field].trim()
    } else {
      payload[field] = form[field]
    }
  })

  saving.value = true
  // 与 initPage 同一世代守卫：保存期间路由切换到别的圈子时，丢弃本次重拉结果
  const gen = initPageGen
  try {
    await updateCircle(payload)
    message.success(t('circle.edit.saveSuccess'))
    // 保存后重拉详情，以服务端数据为准刷新表单与快照
    const res = await getCircleDetail(circleId.value)
    if (gen !== initPageGen) return
    if (res.data) circle.value = res.data
    fillForm()
    // 圈子名可能已修改，同步刷新标签页标题
    if (circle.value.name) {
      setTitleData('title.circleEditName', { name: circle.value.name })
    }
  } catch (error) {
    if (gen !== initPageGen) return
    handleSaveError(error)
  } finally {
    saving.value = false
  }
}

// 按 code 分支渲染自有文案，不展示后端英文 message
const handleSaveError = (error) => {
  const code = error?.code
  if (code === 409) {
    // name/slug 与其他圈子重名
    message.error(t('circle.edit.nameConflict'))
    return
  }
  if (code === 403) {
    // 角色可能已变更（如被免去圈主/管理员）
    message.error(t('circle.edit.noPermission'))
    initPage()
    return
  }
  if (code === 201) {
    message.error(t('circle.edit.validationFailed'))
    return
  }
  message.error(t('circle.edit.saveFailed'))
}

onMounted(initPage)

// 同组件复用（/circle/A/edit → /circle/B/edit）时整体重置
watch(() => route.params.id, (newId, oldId) => {
  if (!newId || newId === oldId) return
  circle.value = {}
  initPage()
})
</script>

<style scoped>
.circle-edit-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  padding: 24px;
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.edit-container {
  max-width: 760px;
  margin: 0 auto;
}

.edit-form {
  display: flex;
  flex-direction: column;
  gap: 24px;
  margin-top: 20px;
}

.edit-section {
  padding: 20px 24px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
}

.section-head {
  margin-bottom: 16px;
}

.section-title {
  margin: 0 0 4px 0;
  font-size: 1.05rem;
  font-weight: 600;
  color: var(--text-primary, #fff);
}

.section-desc {
  margin: 0;
  font-size: 0.82rem;
  color: rgba(255, 255, 255, 0.45);
}

/* ===== 输入/选择：圆润 + 聚焦绿光晕（与 AgentFormModal 同一范式，圆角 12 < 分区 16） ===== */
.edit-form :deep(.n-input),
.edit-form :deep(.n-base-selection) {
  --n-border-radius: 12px !important;
}

.edit-form :deep(.n-input--focus),
.edit-form :deep(.n-base-selection:focus-within) {
  --n-border-focus: 1px solid rgba(102, 234, 194, 0.65) !important;
  --n-box-shadow-focus: 0 0 0 3px rgba(102, 234, 194, 0.15) !important;
}

/* ===== 图片字段：image-card 上传（参考 UserProfile 头像上传交互） ===== */
.image-field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

/* 上传触发卡内容：图标 + 文案，hover 提亮为主题绿 */
.upload-trigger {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  width: 100%;
  height: 100%;
  color: rgba(255, 255, 255, 0.45);
  font-size: 12px;
  transition: color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.image-card-upload:hover .upload-trigger {
  color: #8af0d0;
}

/* 底部操作 */
.edit-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 4px 4px 0;
}

.changed-hint {
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.55);
}

.footer-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.cache-hint {
  margin: 12px 4px 0;
  font-size: 0.78rem;
  color: rgba(255, 255, 255, 0.35);
}

/* 骨架屏 */
.page-skeleton {
  max-width: 760px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.sk-line {
  height: 14px;
  border-radius: 7px;
}

.sk-line--title {
  width: 30%;
  height: 22px;
}

.sk-line--full {
  width: 100%;
}

.sk-line--short {
  width: 45%;
}

.sk-card {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 24px;
  border-radius: 16px;
}

.sk-line,
.sk-card {
  background: linear-gradient(
    90deg,
    rgba(255, 255, 255, 0.05) 25%,
    rgba(255, 255, 255, 0.1) 50%,
    rgba(255, 255, 255, 0.05) 75%
  );
  background-size: 200% 100%;
  animation: edit-shimmer 1.5s ease-in-out infinite;
}

@keyframes edit-shimmer {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}

@media (max-width: 640px) {
  .edit-footer {
    flex-direction: column;
    align-items: stretch;
  }

  .footer-actions {
    justify-content: flex-end;
  }
}
</style>
