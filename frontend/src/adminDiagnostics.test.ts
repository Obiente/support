import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import AdminReportsView from './views/AdminReportsView.vue'
import { loadAdminReport, loadAdminReports, loadAdminSession, type AdminReportDetail } from './support'

vi.mock('./support', async (loadOriginal) => ({
  ...await loadOriginal<typeof import('./support')>(),
  loadAdminSession: vi.fn(), loadAdminReports: vi.fn(), loadAdminReport: vi.fn(),
}))

describe('private diagnostic availability', () => {
  it.each(['unavailable', 'unknown', 'available'] as const)('renders %s without losing report details', async (diagnosticsState) => {
    const report: AdminReportDetail = {
      id: '11111111-1111-4111-8111-111111111111', supportCode: 'OBI-TEST1-TEST2', productId: 'example',
      requestType: 'bug', status: 'accepted', source: 'app', title: 'Synthetic report',
      hasDiagnostics: true, diagnosticsState, createdAt: '2026-09-01T00:00:00Z',
      updatedAt: '2026-09-01T00:00:00Z', retentionUntil: '2026-10-01T00:00:00Z',
      description: 'Synthetic reproduction steps', release: { version: '1.0', platform: 'Linux' }, messages: [],
    }
    vi.mocked(loadAdminSession).mockResolvedValue({ contractVersion: 1, username: 'maintainer', csrfToken: 'synthetic', expiresAt: '2026-10-01T00:00:00Z' })
    vi.mocked(loadAdminReports).mockResolvedValue({ contractVersion: 1, reports: [report], total: 1, limit: 25, offset: 0 })
    vi.mocked(loadAdminReport).mockResolvedValue(report)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
    const wrapper = mount(AdminReportsView, { global: { plugins: [router] } })
    await flushPromises()
    await wrapper.get('.report-list button').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Synthetic reproduction steps')
    expect(wrapper.find('.diagnostic-download').exists()).toBe(diagnosticsState === 'available')
    if (diagnosticsState === 'unavailable') expect(wrapper.text()).toContain('diagnostic ZIP is unavailable')
    if (diagnosticsState === 'unknown') expect(wrapper.text()).toContain('Diagnostic storage could not be checked')
    wrapper.unmount()
  })
})
