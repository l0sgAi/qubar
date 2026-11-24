import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import ModerationReportSummary from '@/components/circle/ModerationReportSummary.vue'
import { createTestI18n } from '../helpers/plugins'

const now = new Date().toISOString()
const report = (reason_type, reason = '') => ({ reason_type, reason, create_time: now })

const mountSummary = (post) => mount(ModerationReportSummary, {
  props: { post: { id: 'r1', status: 1, report_count: 2, first_report_time: now, reports: [], ...post } },
  global: { plugins: [createTestI18n()] }
})

describe('ModerationReportSummary', () => {
  it('举报数、首次举报时间、理由标签；reason 为空时不显示补充说明', async () => {
    const wrapper = mountSummary({ reports: [report(1, '评论区引流加微信'), report(3)] })
    expect(wrapper.find('.report-count').text()).toBe('被举报 2 次')
    expect(wrapper.find('.report-time').text()).toBe('首次举报于 刚刚')
    // 逐条明细默认折叠
    expect(wrapper.find('.report-list').exists()).toBe(false)
    await wrapper.find('.report-toggle').trigger('click')
    const rows = wrapper.findAll('.report-row')
    expect(rows.map(r => r.find('.report-type').text())).toEqual(['垃圾广告 / 引流', '辱骂骚扰'])
    expect(rows[0].find('.report-reason').text()).toBe('评论区引流加微信')
    expect(rows[1].find('.report-reason').exists()).toBe(false)
    expect(rows[0].find('.report-row-time').text()).toBe('刚刚')
    expect(wrapper.find('.report-more').exists()).toBe(false)
    expect(wrapper.find('.report-hidden-hint').exists()).toBe(false)
  })

  it('1 次用单数文案；未知 reason_type 显示「其他」', () => {
    const wrapper = mountSummary({ report_count: 1, reports: [report(42)] })
    expect(wrapper.find('.report-count').text()).toBe('被举报 1 次')
    expect(wrapper.find('.report-group').text()).toBe('其他')
  })

  it('按 reason_type 聚合计数，次数多的在前，单次不带 ×1；展开 / 收起明细', async () => {
    const wrapper = mountSummary({ report_count: 4, reports: [report(1), report(3, 'a'), report(3, 'b'), report(3)] })
    expect(wrapper.findAll('.report-group').map(w => w.text())).toEqual(['辱骂骚扰 ×3', '垃圾广告 / 引流'])
    const toggle = wrapper.find('.report-toggle')
    expect(toggle.text()).toBe('查看举报详情')
    await toggle.trigger('click')
    expect(wrapper.findAll('.report-row')).toHaveLength(4)
    expect(toggle.text()).toBe('收起举报详情')
    await toggle.trigger('click')
    expect(wrapper.find('.report-list').exists()).toBe(false)
  })

  it('report_count 超过 10 时注明仅显示最近 10 条', async () => {
    const reports = Array.from({ length: 10 }, () => report(2))
    const wrapper = mountSummary({ report_count: 13, reports })
    expect(wrapper.find('.report-count').text()).toBe('被举报 13 次')
    expect(wrapper.find('.report-group').text()).toBe('色情低俗 ×10')
    await wrapper.find('.report-toggle').trigger('click')
    expect(wrapper.findAll('.report-row')).toHaveLength(10)
    expect(wrapper.find('.report-more').text()).toBe('仅显示最近 10 条')
  })

  it('status=4 显示自动屏蔽说明', () => {
    const wrapper = mountSummary({ status: 4, reports: [report(1)] })
    expect(wrapper.find('.report-hidden-hint').text()).toBe('仅作者可见，保留后恢复公开')
  })
})
