import { ref } from 'vue'
import { uploadImage } from '@/api/upload'

/**
 * 统一图片上传逻辑：封装上传状态、真实进度、URL 归一。
 * 评论、回复、发帖、裁剪弹窗等多处复用。
 *
 * @param {object} [options]
 * @param {boolean} [options.withProgress=false] - uploadMany 是否采集真实上传进度（0-100）填入 progress
 * @returns {{ uploading: import('vue').Ref<boolean>, uploadingCount: import('vue').Ref<number>, progress: import('vue').Ref<number[]>, uploadOne: Function, uploadMany: Function }}
 */
export function useImageUpload({ withProgress = false } = {}) {
  const uploading = ref(false)
  const uploadingCount = ref(0)
  // 每个正在上传的槽位进度（0-100），与 uploadingCount 对齐；未采集进度时为空数组
  const progress = ref([])

  // 后端响应归一：兼容 { data: { url } } 与 { data: 'url' }
  const normalizeUrl = (res) => {
    const data = res?.data
    if (!data) return null
    if (typeof data === 'string') return data
    return data.url || null
  }

  /**
   * 上传单张图片。
   * @param {File} file
   * @param {object} [ctx]
   * @param {(pct:number)=>void} [ctx.onProgress] - 进度回调（0-100）
   * @returns {Promise<string>} 图片 URL；无 URL 时 reject
   */
  const uploadOne = async (file, { onProgress } = {}) => {
    const res = await uploadImage(file, {
      onUploadProgress: (e) => {
        if (e.total && onProgress) {
          onProgress(Math.round((e.loaded / e.total) * 100))
        }
      }
    })
    const url = normalizeUrl(res)
    if (!url) throw new Error('UPLOAD_NO_URL')
    return url
  }

  /**
   * 顺序上传多张图片，维护 uploading / uploadingCount / progress 状态。
   * 任一失败即中止整批（与历史行为一致：已上传的也不回填）。
   * @param {File[]} files
   * @returns {Promise<string[]>} 图片 URL 列表
   */
  const uploadMany = async (files) => {
    uploading.value = true
    uploadingCount.value = files.length
    progress.value = withProgress ? new Array(files.length).fill(0) : []

    try {
      const urls = []
      for (let i = 0; i < files.length; i++) {
        const url = await uploadOne(files[i], {
          onProgress: withProgress ? (pct) => { progress.value[i] = pct } : undefined
        })
        urls.push(url)
      }
      return urls
    } finally {
      uploading.value = false
      uploadingCount.value = 0
      progress.value = []
    }
  }

  return { uploading, uploadingCount, progress, uploadOne, uploadMany }
}
