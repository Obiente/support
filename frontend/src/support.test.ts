import { afterEach, describe, expect, it, vi } from 'vitest'
import { adminLogin, deleteReceipt, loadProducts, randomIdempotencyKey, reconcileReceipt, updateAdminReportStatus } from './support'

afterEach(() => vi.unstubAllGlobals())

describe('support contract client', () => {
  it('creates a bounded URL-safe idempotency key', () => {
    const key = randomIdempotencyKey()
    expect(key).toMatch(/^[A-Za-z0-9_-]{43}$/)
  })

  it('loads a product-neutral registry response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      contractVersion: 1,
      products: [{
        id: 'synthetic-product', name: 'Synthetic product', description: 'Fixture',
        diagnosticContentTypes: [], diagnosticEntries: [], diagnosticMaxBytes: 0,
        diagnosticMaxExpandedBytes: 0, retentionDays: 7,
      }],
    }), { status: 200, headers: { 'Content-Type': 'application/json' } })))

    const products = await loadProducts()
    expect(products.map((product) => product.id)).toEqual(['synthetic-product'])
  })

  it('treats a missing idempotent receipt as safely retryable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 'not_found' }), {
      status: 404, headers: { 'Content-Type': 'application/json' },
    })))

    await expect(reconcileReceipt('A'.repeat(43))).resolves.toBeUndefined()
  })

  it('deletes only through a same-origin private capability receipt', async () => {
    const fetch = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetch)
    const capability = 'abcdefghijklmnopqrstuvwxyzABCDEFGH_12345678'

    await deleteReceipt({
      contractVersion: 1,
      supportCode: 'OBI-ABCDE-23456',
      status: 'new',
      statusUrl: `${window.location.origin}/r/${capability}`,
      deletionUrl: `${window.location.origin}/r/${capability}`,
      createdAt: '2026-08-13T12:00:00Z',
      retentionUntil: '2026-09-12T12:00:00Z',
    })

    expect(fetch).toHaveBeenCalledWith(`/api/v1/reports/${capability}`, expect.objectContaining({ method: 'DELETE' }))
  })

  it('rejects a cross-origin deletion receipt', async () => {
    const capability = 'abcdefghijklmnopqrstuvwxyzABCDEFGH_12345678'
    await expect(deleteReceipt({
      contractVersion: 1,
      supportCode: 'OBI-ABCDE-23456',
      status: 'new',
      statusUrl: `${window.location.origin}/r/${capability}`,
      deletionUrl: `https://untrusted.example/r/${capability}`,
      createdAt: '2026-08-13T12:00:00Z',
      retentionUntil: '2026-09-12T12:00:00Z',
    })).rejects.toThrow('invalid deletion link')
  })

  it('keeps the admin session in an HTTP-only cookie and sends CSRF only on mutations', async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({
        contractVersion: 1,
        username: 'maintainer',
        csrfToken: 'A'.repeat(43),
        expiresAt: '2026-08-14T00:00:00Z',
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        id: '11111111-1111-4111-8111-111111111111',
        supportCode: 'OBI-ABCDE-23456',
        productId: 'synthetic-product',
        requestType: 'bug',
        status: 'accepted',
        source: 'app',
        title: 'Synthetic failure',
        hasDiagnostics: true,
        createdAt: '2026-08-13T12:00:00Z',
        updatedAt: '2026-08-13T13:00:00Z',
        retentionUntil: '2026-09-12T12:00:00Z',
        description: 'Synthetic diagnostic report.',
        release: { version: '1.0.0', platform: 'linux' },
      }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetch)

    await adminLogin('maintainer', 'secret')
    await updateAdminReportStatus('11111111-1111-4111-8111-111111111111', 'accepted')

    expect(fetch).toHaveBeenNthCalledWith(1, '/api/v1/admin/login', expect.objectContaining({
      credentials: 'same-origin',
      method: 'POST',
    }))
    expect(fetch).toHaveBeenNthCalledWith(2, '/api/v1/admin/reports/11111111-1111-4111-8111-111111111111', expect.objectContaining({
      credentials: 'same-origin',
      method: 'PATCH',
      headers: expect.objectContaining({ 'X-CSRF-Token': 'A'.repeat(43) }),
    }))
  })
})
