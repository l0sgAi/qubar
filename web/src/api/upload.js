import request from '@/utils/request'

/**
 * 上传图片
 * @param {File} file - 图片文件
 * @param {object} [options]
 * @param {(e: ProgressEvent) => void} [options.onUploadProgress] - axios 上传进度回调，用于展示真实上传进度
 * @returns {Promise} 返回包含 URL 的响应
 */
export function uploadImage(file, { onUploadProgress } = {}) {
  const formData = new FormData()
  formData.append('file', file)

  return request({
    url: '/upload/image',
    method: 'post',
    data: formData,
    onUploadProgress,
    // 覆盖 request 实例的全局 timeout(10s)：大图/慢网上传易超 10s，
    // axios 抛 ECONNABORTED → 前端判失败，但后端此时往往已完整接收并保存成功，
    // 出现“后端成功、前端失败”的假象。上传已有真实进度条给用户反馈，
    // 故此处不设超时（0 = 无限等），由进度条 + 用户手动取消兜底。
    timeout: 0
    // 不手动设 Content-Type：axios 检测到 FormData 会自动加上
    // 'multipart/form-data; boundary=...'，手动设会丢 boundary 导致后端解析为空。
  })
}
