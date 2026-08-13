export type RequestType = 'bug' | 'feature' | 'support'

export interface Product {
  id: string
  name: string
  description: string
  repository?: string
  diagnosticContentTypes: string[]
  diagnosticEntries: string[]
  diagnosticMaxBytes: number
  diagnosticMaxExpandedBytes: number
  retentionDays: number
}

export interface ProductResponse {
  contractVersion: 1
  products: Product[]
}

export interface ReportMetadata {
  contractVersion: 1
  productId: string
  requestType: RequestType
  title: string
  description: string
  contact: string
  source: 'web' | 'app'
  release: {
    version: string
    channel?: string
    platform: string
    osVersion?: string
    architecture?: string
  }
  privacyAccepted: boolean
}

export interface Receipt {
  contractVersion: 1
  supportCode: string
  status: string
  statusUrl: string
  deletionUrl: string
  createdAt: string
  retentionUntil: string
}

export interface PrivateStatus {
  contractVersion: 1
  supportCode: string
  productId: string
  requestType: RequestType
  status: string
  createdAt: string
  updatedAt: string
  retentionUntil: string
}

export interface AdminSession {
  contractVersion: 1
  username: string
  csrfToken: string
  expiresAt: string
}

export interface AdminReportSummary {
  id: string
  supportCode: string
  productId: string
  requestType: RequestType
  status: string
  source: 'web' | 'app'
  title: string
  hasDiagnostics: boolean
  createdAt: string
  updatedAt: string
  retentionUntil: string
}

export interface AdminReportDetail extends AdminReportSummary {
  description: string
  contact?: string
  release: ReportMetadata['release']
}

export interface AdminReportResponse {
  contractVersion: 1
  reports: AdminReportSummary[]
  total: number
  limit: number
  offset: number
}

let adminCSRFToken = ''

interface Problem {
  code?: string
  message?: string
}

export class SupportApiError extends Error {
  constructor(message: string, readonly code: string | undefined, readonly status: number) {
    super(message)
  }
}

export function randomIdempotencyKey(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(32))
  return btoa(String.fromCharCode(...bytes))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replaceAll('=', '')
}

export async function loadProducts(signal?: AbortSignal): Promise<Product[]> {
  const response = await fetch('/api/v1/products', { headers: { Accept: 'application/json' }, signal })
  if (!response.ok) throw await apiError(response)
  const body = (await response.json()) as ProductResponse
  if (body.contractVersion !== 1 || !Array.isArray(body.products)) throw new Error('The support product list is invalid.')
  return body.products
}

export function submitReport(
  metadata: ReportMetadata,
  diagnostics: File | undefined,
  idempotencyKey: string,
  onProgress: (progress: number | undefined) => void,
  signal: AbortSignal,
): Promise<Receipt> {
  return new Promise((resolve, reject) => {
    const body = new FormData()
    body.append('metadata', new Blob([JSON.stringify(metadata)], { type: 'application/json' }))
    if (diagnostics) body.append('diagnostics', diagnostics, 'diagnostics.zip')
    const request = new XMLHttpRequest()
    request.open('POST', '/api/v1/reports')
    request.setRequestHeader('Accept', 'application/json')
    request.setRequestHeader('Idempotency-Key', idempotencyKey)
    request.upload.onprogress = (event) => onProgress(event.lengthComputable ? event.loaded / event.total : undefined)
    request.onerror = () => reject(new TypeError('The support service could not be reached.'))
    request.onabort = () => reject(new DOMException('Submission cancelled.', 'AbortError'))
    request.onload = () => {
      const parsed = parseJSON(request.responseText)
      if (request.status < 200 || request.status >= 300) {
        const problem = parsed as Problem
        reject(new SupportApiError(problem.message || 'The report was rejected.', problem.code, request.status))
        return
      }
      resolve(parseReceipt(parsed))
    }
    signal.addEventListener('abort', () => request.abort(), { once: true })
    request.send(body)
  })
}

export async function reconcileReceipt(idempotencyKey: string, signal?: AbortSignal): Promise<Receipt | undefined> {
  const response = await fetch('/api/v1/receipts', {
    headers: { Accept: 'application/json', 'Idempotency-Key': idempotencyKey },
    signal,
  })
  if (response.status === 404) return undefined
  if (!response.ok) throw await apiError(response)
  return parseReceipt(await response.json())
}

export async function loadPrivateStatus(capability: string, signal?: AbortSignal): Promise<PrivateStatus> {
  const response = await fetch(`/api/v1/reports/${encodeURIComponent(capability)}`, {
    headers: { Accept: 'application/json' }, signal,
  })
  if (!response.ok) throw await apiError(response)
  return (await response.json()) as PrivateStatus
}

export async function deletePrivateReport(capability: string): Promise<void> {
  const response = await fetch(`/api/v1/reports/${encodeURIComponent(capability)}`, {
    method: 'DELETE', headers: { Accept: 'application/json' },
  })
  if (!response.ok) throw await apiError(response)
}

export async function deleteReceipt(receipt: Receipt): Promise<void> {
  const deletionUrl = new URL(receipt.deletionUrl, window.location.origin)
  const match = /^\/r\/([A-Za-z0-9_-]{43})$/.exec(deletionUrl.pathname)
  if (deletionUrl.origin !== window.location.origin || deletionUrl.search || deletionUrl.hash || !match) {
    throw new Error('The support service returned an invalid deletion link.')
  }
  await deletePrivateReport(match[1])
}

export async function adminLogin(username: string, password: string): Promise<AdminSession> {
  const session = await adminJSON<AdminSession>('/api/v1/admin/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  adminCSRFToken = session.csrfToken
  return session
}

export async function loadAdminSession(): Promise<AdminSession> {
  const session = await adminJSON<AdminSession>('/api/v1/admin/session')
  adminCSRFToken = session.csrfToken
  return session
}

export async function adminLogout(): Promise<void> {
  await adminJSON('/api/v1/admin/logout', {
    method: 'POST', headers: { 'X-CSRF-Token': adminCSRFToken },
  }, true)
  adminCSRFToken = ''
}

export async function loadAdminReports(status = ''): Promise<AdminReportResponse> {
  const query = status ? `?status=${encodeURIComponent(status)}` : ''
  return adminJSON<AdminReportResponse>(`/api/v1/admin/reports${query}`)
}

export async function loadAdminReport(id: string): Promise<AdminReportDetail> {
  return adminJSON<AdminReportDetail>(`/api/v1/admin/reports/${encodeURIComponent(id)}`)
}

export async function updateAdminReportStatus(id: string, status: string): Promise<AdminReportDetail> {
  return adminJSON<AdminReportDetail>(`/api/v1/admin/reports/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': adminCSRFToken },
    body: JSON.stringify({ status }),
  })
}

export function adminDiagnosticsURL(id: string): string {
  return `/api/v1/admin/reports/${encodeURIComponent(id)}/diagnostics`
}

async function adminJSON<T = unknown>(path: string, init: RequestInit = {}, noContent = false): Promise<T> {
  const response = await fetch(path, { ...init, credentials: 'same-origin', headers: { Accept: 'application/json', ...init.headers } })
  if (!response.ok) throw await apiError(response)
  if (noContent) return undefined as T
  return (await response.json()) as T
}

async function apiError(response: Response): Promise<SupportApiError> {
  const problem = (await response.json().catch(() => ({}))) as Problem
  return new SupportApiError(problem.message || 'Support intake is temporarily unavailable.', problem.code, response.status)
}

function parseJSON(value: string): unknown {
  try {
    return JSON.parse(value)
  } catch {
    throw new Error('The support service returned an invalid response.')
  }
}

function parseReceipt(value: unknown): Receipt {
  if (!value || typeof value !== 'object') throw new Error('The support service returned an invalid receipt.')
  const receipt = value as Partial<Receipt>
  const statusUrl = typeof receipt.statusUrl === 'string' ? new URL(receipt.statusUrl, window.location.origin) : undefined
  const deletionUrl = typeof receipt.deletionUrl === 'string' ? new URL(receipt.deletionUrl, window.location.origin) : undefined
  if (
    receipt.contractVersion !== 1 ||
    typeof receipt.supportCode !== 'string' ||
    !/^OBI-[A-HJ-KM-NP-Z2-9]{5}-[A-HJ-KM-NP-Z2-9]{5}$/.test(receipt.supportCode) ||
    typeof receipt.status !== 'string' ||
    !statusUrl || statusUrl.origin !== window.location.origin || !/^\/r\/[A-Za-z0-9_-]{43}$/.test(statusUrl.pathname) || statusUrl.search || statusUrl.hash ||
    !deletionUrl || deletionUrl.origin !== window.location.origin || deletionUrl.pathname !== statusUrl.pathname || deletionUrl.search || deletionUrl.hash ||
    typeof receipt.createdAt !== 'string' ||
    typeof receipt.retentionUntil !== 'string'
  ) {
    throw new Error('The support service returned an invalid receipt.')
  }
  return receipt as Receipt
}
