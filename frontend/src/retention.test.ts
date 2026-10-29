import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import AdminReportsView from './views/AdminReportsView.vue'
import PrivateStatusView from './views/PrivateStatusView.vue'
import SupportIntakeView from './views/SupportIntakeView.vue'
import {
  deleteAdminReport,
  loadAdminReport,
  loadAdminReports,
  loadAdminSession,
  loadPrivateStatus,
  loadProducts,
  type AdminReportDetail,
} from './support'

vi.mock('./support', async (loadOriginal) => ({
  ...await loadOriginal<typeof import('./support')>(),
  loadAdminSession: vi.fn(),
  loadAdminReports: vi.fn(),
  loadAdminReport: vi.fn(),
  deleteAdminReport: vi.fn(),
  loadPrivateStatus: vi.fn(),
  loadProducts: vi.fn(),
}))

const legacyRetentionUntil = '2026-10-01T00:00:00Z'

function adminReport(): AdminReportDetail {
  return {
    id: '11111111-1111-4111-8111-111111111111', supportCode: 'OBI-TEST1-TEST2', productId: 'example',
    requestType: 'bug', status: 'resolved', source: 'app', title: 'Synthetic report',
    hasDiagnostics: true, diagnosticsState: 'available', createdAt: '2026-09-01T00:00:00Z',
    updatedAt: '2026-09-01T00:00:00Z', retentionUntil: legacyRetentionUntil,
    description: 'Synthetic reproduction steps', release: { version: '1.0', platform: 'Linux' }, messages: [],
  }
}

async function mountAdmin() {
  const report = adminReport()
  vi.mocked(loadAdminSession).mockResolvedValue({ contractVersion: 1, username: 'maintainer', csrfToken: 'synthetic', expiresAt: '2026-10-01T00:00:00Z' })
  vi.mocked(loadAdminReports).mockResolvedValue({ contractVersion: 1, reports: [report], total: 1, limit: 25, offset: 0 })
  vi.mocked(loadAdminReport).mockResolvedValue(report)
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  const wrapper = mount(AdminReportsView, { global: { plugins: [router] } })
  await flushPromises()
  await wrapper.get('.report-list button').trigger('click')
  await flushPromises()
  return { wrapper, report }
}

function deleteButton(wrapper: Awaited<ReturnType<typeof mountAdmin>>['wrapper']) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text() === 'Delete report')
  if (!button) throw new Error('Delete report button is missing')
  return button
}

afterEach(() => {
  vi.restoreAllMocks()
  vi.mocked(deleteAdminReport).mockReset()
})

describe('indefinite report retention', () => {
  it('shows reporters that their private request is kept until deleted', async () => {
    vi.mocked(loadPrivateStatus).mockResolvedValue({
      contractVersion: 1, supportCode: 'OBI-TEST1-TEST2', productId: 'example', requestType: 'bug',
      status: 'new', createdAt: '2026-09-01T00:00:00Z', updatedAt: '2026-09-01T00:00:00Z',
      retentionUntil: legacyRetentionUntil, messages: [],
    })
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/r/:capability', component: PrivateStatusView }] })
    await router.push('/r/' + 'A'.repeat(43))
    const wrapper = mount(PrivateStatusView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Kept until deleted')
    expect(wrapper.text()).not.toMatch(/expires/i)
    expect(wrapper.text()).not.toContain('October 1, 2026')
    wrapper.unmount()
  })

  it('states the retention policy on the intake form', async () => {
    vi.mocked(loadProducts).mockResolvedValue([{
      id: 'example', name: 'Example', description: 'Synthetic product', diagnosticContentTypes: ['application/zip'],
      diagnosticEntries: ['report.json'], diagnosticMaxBytes: 4096, diagnosticMaxExpandedBytes: 8192,
    }])
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: SupportIntakeView }] })
    const wrapper = mount(SupportIntakeView, { global: { plugins: [router] } })
    await flushPromises()
    expect(wrapper.text()).toContain('Your report and diagnostic attachment are kept until you delete them or Obiente Support deletes them after your request is resolved.')
    expect(wrapper.text()).not.toMatch(/expire/i)
    wrapper.unmount()
  })

  it('shows maintainers that a report is kept until deleted', async () => {
    const { wrapper } = await mountAdmin()
    expect(wrapper.text()).toContain('Kept until deleted')
    expect(wrapper.text()).not.toMatch(/expires/i)
    wrapper.unmount()
  })

  it('does not delete a report when the maintainer cancels the confirmation', async () => {
    const { wrapper } = await mountAdmin()
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await deleteButton(wrapper).trigger('click')
    await flushPromises()
    expect(confirm).toHaveBeenCalledOnce()
    expect(deleteAdminReport).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('Synthetic reproduction steps')
    wrapper.unmount()
  })

  it('deletes a confirmed report and removes it from the queue', async () => {
    const { wrapper, report } = await mountAdmin()
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.mocked(deleteAdminReport).mockResolvedValue()
    vi.mocked(loadAdminReports).mockResolvedValue({ contractVersion: 1, reports: [], total: 0, limit: 25, offset: 0 })
    await deleteButton(wrapper).trigger('click')
    await flushPromises()
    expect(deleteAdminReport).toHaveBeenCalledWith(report.id)
    expect(wrapper.text()).toContain(`${report.supportCode} was deleted.`)
    expect(wrapper.text()).not.toContain('Synthetic reproduction steps')
    expect(wrapper.text()).toContain('No reports match this status.')
    wrapper.unmount()
  })
})
