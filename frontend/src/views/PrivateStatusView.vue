<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import type { PrivateStatus } from '../support'
import { deletePrivateReport, loadPrivateStatus } from '../support'

const route = useRoute()
const capability = computed(() => String(route.params.capability ?? ''))
const report = ref<PrivateStatus>()
const loading = ref(true)
const deleting = ref(false)
const deleted = ref(false)
const message = ref('')
const controller = new AbortController()

onMounted(async () => {
  try {
    report.value = await loadPrivateStatus(capability.value, controller.signal)
  } catch (error) {
    message.value = error instanceof Error ? error.message : 'This private report link is not available.'
  } finally {
    loading.value = false
  }
})

onBeforeUnmount(() => controller.abort())

function formatDate(value: string, includeTime = false): string {
  return new Intl.DateTimeFormat(undefined, includeTime
    ? { dateStyle: 'long', timeStyle: 'short', timeZone: 'UTC' }
    : { dateStyle: 'long', timeZone: 'UTC' }).format(new Date(value))
}

async function removeReport() {
  if (!window.confirm('Permanently delete this private support request and its diagnostic attachment?')) return
  deleting.value = true
  message.value = 'Deleting your private request...'
  try {
    await deletePrivateReport(capability.value)
    deleted.value = true
    message.value = 'Your private request and diagnostic attachment were deleted.'
  } catch (error) {
    message.value = error instanceof Error ? error.message : 'The request could not be deleted.'
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <main id="main-content" class="status-main">
    <section class="status-card">
      <template v-if="loading"><div class="eyebrow">Private support request</div><h1>Loading...</h1></template>
      <template v-else-if="report">
        <div class="eyebrow">Private support request</div>
        <h1>{{ report.supportCode }}</h1>
        <div class="status-pill">{{ deleted ? 'deleted' : report.status.replaceAll('_', ' ') }}</div>
        <dl v-if="!deleted">
          <div><dt>Product</dt><dd>{{ report.productId }}</dd></div>
          <div><dt>Request</dt><dd>{{ report.requestType }}</dd></div>
          <div><dt>Submitted</dt><dd>{{ formatDate(report.createdAt, true) }}</dd></div>
          <div><dt>Private data expires</dt><dd>{{ formatDate(report.retentionUntil) }}</dd></div>
        </dl>
        <p v-if="!deleted">This link is private. Anyone who has it can view or delete this request. Do not post it publicly.</p>
        <button v-if="!deleted" class="danger" type="button" :disabled="deleting" @click="removeReport">{{ deleting ? 'Deleting...' : 'Delete private request' }}</button>
      </template>
      <p v-if="message" class="form-status" role="status" aria-live="polite">{{ message }}</p>
    </section>
  </main>
</template>
