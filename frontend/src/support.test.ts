import { afterEach, describe, expect, it, vi } from 'vitest'
import { deleteReceipt, loadProducts, randomIdempotencyKey, reconcileReceipt } from './support'

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
})
