/**
 * 字符串清洗工具
 *
 * 背景：PostgreSQL 的 text/varchar 字段不允许包含 NULL 字节（U+0000），
 * 否则会报 "invalid byte sequence for encoding UTF8: 0x00" (SQLSTATE 22021)。
 * 这些非法字节常来自富文本/Markdown 编辑器粘贴、跨应用复制等场景。
 * MySQL 的 utf8 允许该字节，因此 MySQL→PG 迁移后容易出现此问题。
 */

// NULL 字节常量，等价于 U+0000。
// 故意用 String.fromCharCode(0) 构造，避免在源码中直接写入不可见字节
// （直接写  字面量会让 git diff / lint / 编辑器都难以审视）。
const NULL_BYTE = String.fromCharCode(0)

/**
 * 移除字符串中的 NULL 字节（U+0000）。
 *
 * 只删 NULL 字节——这是 PG text 类型唯一拒绝的控制字符；
 * 其它控制字符（\t \n \r 等）PG 均接受，保留它们以避免破坏正文。
 * @param {string} str
 * @returns {string}
 */
export function sanitizeString(str) {
  if (typeof str !== 'string') return str
  return str.split(NULL_BYTE).join('')
}

/**
 * 递归清洗 payload 中所有字符串字段（含嵌套对象与数组），非字符串原样返回。
 * 返回新结构，不修改入参。
 * @param {*} data
 * @returns {*}
 */
export function sanitizePayload(data) {
  if (typeof data === 'string') {
    return sanitizeString(data)
  }
  if (Array.isArray(data)) {
    return data.map(sanitizePayload)
  }
  if (data !== null && typeof data === 'object') {
    const result = {}
    for (const key of Object.keys(data)) {
      result[key] = sanitizePayload(data[key])
    }
    return result
  }
  return data
}
