/**
 * 帖子举报理由（POST /post/report 的 reason_type，1–8）。
 * 文案见 locales 的 report.reasons.<key>。
 */
export const REPORT_REASON_TYPES = [
  { value: 1, key: 'spam' },
  { value: 2, key: 'sexual' },
  { value: 3, key: 'harassment' },
  { value: 4, key: 'illegal' },
  { value: 5, key: 'violence' },
  { value: 6, key: 'privacy' },
  { value: 7, key: 'selfHarm' },
  { value: 8, key: 'other' }
]

// 举报补充说明上限（≤200 字符）
export const REPORT_REASON_MAX = 200

/** reason_type → 文案 key；未知值落到 other */
export function reportReasonKey(type) {
  return REPORT_REASON_TYPES.find(r => r.value === type)?.key || 'other'
}
