<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { AdminReportDetail, AdminReportSummary, AdminSession } from '../support'
import {
  adminDiagnosticsURL,
  adminLogout,
  loadAdminReport,
  loadAdminReports,
  loadAdminSession,
  sendAdminMessage,
  SupportApiError,
  updateAdminReportStatus,
} from '../support'

const router = useRouter()
const session = ref<AdminSession>()
const reports = ref<AdminReportSummary[]>([])
const selected = ref<AdminReportDetail>()
const statusFilter = ref('')
const loading = ref(true)
const loadingDetail = ref(false)
const saving = ref(false)
const message = ref('')
const reply = ref('')

const statuses = ['new', 'needs_information', 'accepted', 'duplicate', 'resolved', 'rejected']

onMounted(async () => {
  try {
    session.value = await loadAdminSession()
    await refreshReports()
  } catch (error) {
    await handleError(error)
  } finally {
    loading.value = false
  }
})

async function refreshReports() {
  const response = await loadAdminReports(statusFilter.value)
  reports.value = response.reports
  if (selected.value && !reports.value.some((report) => report.id === selected.value?.id)) selected.value = undefined
}

async function chooseReport(report: AdminReportSummary) {
  loadingDetail.value = true
  message.value = ''
  try {
    selected.value = await loadAdminReport(report.id)
  } catch (error) {
    await handleError(error)
  } finally {
    loadingDetail.value = false
  }
}

async function changeStatus(event: Event) {
  if (!selected.value) return
  saving.value = true
  message.value = ''
  try {
    selected.value = await updateAdminReportStatus(selected.value.id, (event.target as HTMLSelectElement).value)
    await refreshReports()
    message.value = 'Report status updated.'
  } catch (error) {
    await handleError(error)
  } finally {
    saving.value = false
  }
}

async function askReporter() {
  if (!selected.value || !reply.value.trim()) return
  saving.value = true
  message.value = ''
  try {
    selected.value = await sendAdminMessage(selected.value.id, reply.value)
    reply.value = ''
    await refreshReports()
    message.value = 'Message sent. The report now needs information.'
  } catch (error) {
    await handleError(error)
  } finally {
    saving.value = false
  }
}

async function signOut() {
  try {
    await adminLogout()
  } finally {
    await router.replace('/admin/login')
  }
}

async function handleError(error: unknown) {
  if (error instanceof SupportApiError && error.status === 401) {
    await router.replace('/admin/login')
    return
  }
  message.value = error instanceof Error ? error.message : 'The admin request could not be completed.'
}

function formatDate(value: string, includeTime = false): string {
  return new Intl.DateTimeFormat(undefined, includeTime
    ? { dateStyle: 'medium', timeStyle: 'short' }
    : { dateStyle: 'medium' }).format(new Date(value))
}

function readable(value: string): string {
  return value.replaceAll('_', ' ')
}
</script>

<template>
  <main id="main-content" class="admin-main">
    <header class="admin-heading">
      <div><div class="eyebrow">Maintainer console</div><h1>Private support queue</h1></div>
      <div class="admin-account"><span v-if="session">Signed in as {{ session.username }}</span><button class="secondary" type="button" @click="signOut">Sign out</button></div>
    </header>

    <p v-if="loading" class="form-status" role="status">Loading private support requests...</p>
    <template v-else>
      <div class="admin-toolbar">
        <label class="field"><span>Show reports</span><select v-model="statusFilter" @change="refreshReports"><option value="">All statuses</option><option v-for="status in statuses" :key="status" :value="status">{{ readable(status) }}</option></select></label>
        <span>{{ reports.length }} shown</span>
      </div>

      <div class="admin-workspace">
        <section class="report-list" aria-label="Private support reports">
          <button v-for="report in reports" :key="report.id" type="button" :class="{ selected: selected?.id === report.id }" @click="chooseReport(report)">
            <span class="report-list-meta"><b>{{ report.supportCode }}</b><span>{{ readable(report.status) }}</span></span>
            <strong>{{ report.title }}</strong>
            <span>{{ report.productId }} · {{ report.source }} · {{ formatDate(report.createdAt) }}</span>
            <small v-if="report.hasDiagnostics">Diagnostic ZIP attached</small>
          </button>
          <p v-if="reports.length === 0">No reports match this status.</p>
        </section>

        <section class="report-detail" aria-live="polite">
          <p v-if="loadingDetail">Loading the private report...</p>
          <template v-else-if="selected">
            <div class="report-detail-heading">
              <div><span>{{ selected.supportCode }}</span><h2>{{ selected.title }}</h2></div>
              <label class="field"><span>Status</span><select :value="selected.status" :disabled="saving" @change="changeStatus"><option v-for="status in statuses" :key="status" :value="status">{{ readable(status) }}</option></select></label>
            </div>
            <dl>
              <div><dt>Product</dt><dd>{{ selected.productId }}</dd></div>
              <div><dt>Request</dt><dd>{{ readable(selected.requestType) }}</dd></div>
              <div><dt>Source</dt><dd>{{ selected.source }}</dd></div>
              <div><dt>Release</dt><dd>{{ selected.release.version || 'Not provided' }} · {{ selected.release.platform }}</dd></div>
              <div><dt>Submitted</dt><dd>{{ formatDate(selected.createdAt, true) }}</dd></div>
              <div><dt>Expires</dt><dd>{{ formatDate(selected.retentionUntil) }}</dd></div>
            </dl>
            <div class="private-copy"><h3>Description</h3><p>{{ selected.description }}</p></div>
            <div v-if="selected.contact" class="private-copy"><h3>Private contact</h3><p>{{ selected.contact }}</p></div>
            <div class="private-copy"><h3>Private conversation</h3>
              <p v-if="selected.messages.length === 0">No messages yet.</p>
              <article v-for="entry in selected.messages" :key="entry.id" class="support-message">
                <b>{{ entry.author === 'maintainer' ? 'Maintainer' : 'Reporter' }}</b>
                <small>{{ formatDate(entry.createdAt, true) }}</small>
                <p>{{ entry.body }}</p>
              </article>
              <form class="message-composer" @submit.prevent="askReporter">
                <label class="field"><span>Ask for more details</span><textarea v-model="reply" maxlength="8192" rows="4" required /></label>
                <button class="primary" type="submit" :disabled="saving || !reply.trim()">{{ saving ? 'Sending...' : 'Send private message' }}</button>
              </form>
            </div>
            <a v-if="selected.hasDiagnostics" class="secondary diagnostic-download" :href="adminDiagnosticsURL(selected.id)">Download diagnostic ZIP</a>
            <p class="private-warning">Private report data must not be copied into a public issue without reviewing and removing personal information.</p>
          </template>
          <p v-else>Select a report to review its private details.</p>
        </section>
      </div>
    </template>
    <p class="form-status" role="status" aria-live="polite">{{ message }}</p>
  </main>
</template>
