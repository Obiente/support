<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { Product, Receipt, ReportMetadata, RequestType } from '../support'
import { deleteReceipt, loadProducts, randomIdempotencyKey, reconcileReceipt, submitReport, SupportApiError } from '../support'

const products = ref<Product[]>([])
const loadingProducts = ref(true)
const requestType = ref<RequestType>('bug')
const productId = ref('')
const version = ref('')
const title = ref('')
const description = ref('')
const contact = ref('')
const diagnostics = ref<File>()
const privacyAccepted = ref(false)
const sending = ref(false)
const progress = ref<number>()
const message = ref('')
const receipt = ref<Receipt>()
let controller: AbortController | undefined
const pendingKeyName = 'obiente-support-pending-key'

const selectedProduct = computed(() => products.value.find((product) => product.id === productId.value))
const diagnosticsSupported = computed(() => (selectedProduct.value?.diagnosticMaxBytes ?? 0) > 0)
const progressLabel = computed(() => progress.value === undefined ? 'Sending securely...' : `Sending securely... ${Math.round(progress.value * 100)}%`)

onMounted(async () => {
  controller = new AbortController()
  try {
    products.value = await loadProducts(controller.signal)
    productId.value = products.value[0]?.id ?? ''
  } catch (error) {
    message.value = error instanceof Error ? error.message : 'Support intake is temporarily unavailable.'
  } finally {
    loadingProducts.value = false
  }
  await recoverPendingReceipt()
})

onBeforeUnmount(() => controller?.abort())

function chooseDiagnostics(event: Event) {
  diagnostics.value = (event.target as HTMLInputElement).files?.[0]
}

async function send() {
  if (sending.value || !selectedProduct.value) return
  if (sessionStorage.getItem(pendingKeyName)) {
    await recoverPendingReceipt()
    if (sessionStorage.getItem(pendingKeyName) || receipt.value) return
  }
  if (diagnostics.value && diagnostics.value.size > selectedProduct.value.diagnosticMaxBytes) {
    message.value = 'The diagnostic report is larger than this product accepts.'
    return
  }
  sending.value = true
  progress.value = 0
  message.value = ''
  controller = new AbortController()
  const idempotencyKey = randomIdempotencyKey()
  sessionStorage.setItem(pendingKeyName, idempotencyKey)
  const metadata: ReportMetadata = {
    contractVersion: 1,
    productId: productId.value,
    requestType: requestType.value,
    title: title.value,
    description: description.value,
    contact: contact.value,
    source: 'web',
    release: { version: version.value, platform: 'web' },
    privacyAccepted: privacyAccepted.value,
  }
  try {
    receipt.value = await submitReport(metadata, diagnostics.value, idempotencyKey, (value) => { progress.value = value }, controller.signal)
    sessionStorage.removeItem(pendingKeyName)
  } catch (error) {
    const cancelled = error instanceof DOMException && error.name === 'AbortError'
    if (error instanceof SupportApiError && error.status >= 400 && error.status < 500) {
      sessionStorage.removeItem(pendingKeyName)
      message.value = error.message
    } else {
      try {
        const reconciled = await reconcileReceipt(idempotencyKey)
        if (reconciled && cancelled) {
          await deleteReceipt(reconciled)
          sessionStorage.removeItem(pendingKeyName)
          message.value = 'Submission cancelled. The received private copy was deleted.'
        } else if (reconciled) {
          receipt.value = reconciled
          sessionStorage.removeItem(pendingKeyName)
        } else {
          sessionStorage.removeItem(pendingKeyName)
          message.value = cancelled
            ? 'Submission cancelled before it reached support.'
            : error instanceof Error ? error.message : 'Your request could not be sent.'
        }
      } catch {
        message.value = cancelled
          ? 'Cancellation could not be confirmed. Reopen this page when your connection returns so it can be reconciled.'
          : 'The result is uncertain. Reopen this page when your connection returns so it can be reconciled.'
      }
    }
  } finally {
    sending.value = false
    progress.value = undefined
  }
}

async function recoverPendingReceipt() {
  const idempotencyKey = sessionStorage.getItem(pendingKeyName)
  if (!idempotencyKey) return
  try {
    const reconciled = await reconcileReceipt(idempotencyKey, controller?.signal)
    if (reconciled) {
      receipt.value = reconciled
      message.value = 'Your previous submission was received.'
    } else {
      message.value = 'Your previous submission did not reach support. You can send it again.'
    }
    sessionStorage.removeItem(pendingKeyName)
  } catch (error) {
    if (!(error instanceof DOMException && error.name === 'AbortError')) {
      message.value = 'A previous submission still has an uncertain result. This page will check it again before sending another.'
    }
  }
}

function cancel() {
  controller?.abort()
}

async function copyCode() {
  if (!receipt.value) return
  await navigator.clipboard.writeText(receipt.value.supportCode)
  message.value = 'Support code copied.'
}
</script>

<template>
  <main id="main-content">
    <section class="hero">
      <div class="eyebrow">Help for every Obiente project</div>
      <h1>Tell us what happened.<br><span>GitHub is optional.</span></h1>
      <p class="hero-copy">Report a problem, request a feature, or ask for help. Your submission stays private until a maintainer reviews anything that could become public.</p>
      <div class="trust-row" aria-label="Privacy commitments">
        <span>Private by default</span><span>No account required</span><span>Delete with your private link</span>
      </div>
    </section>

    <section class="intake-layout" aria-labelledby="form-title">
      <div class="form-intro">
        <div class="eyebrow">Start a request</div>
        <h2 id="form-title">What can we help with?</h2>
        <p>Only the details you enter are submitted. Diagnostic files are never attached by the website unless you choose one.</p>
        <ol id="how-it-works" class="steps">
          <li><span>1</span><div><strong>Send privately</strong><small>Your request enters a private moderation queue.</small></div></li>
          <li><span>2</span><div><strong>Keep your support code</strong><small>The private status link lets you follow or delete it.</small></div></li>
          <li><span>3</span><div><strong>Public only after review</strong><small>Diagnostics and contact details are never published.</small></div></li>
        </ol>
      </div>

      <div class="form-shell">
        <form v-if="!receipt" id="support-form" @submit.prevent="send">
          <fieldset class="request-types" :disabled="sending">
            <legend>Request type</legend>
            <label><input v-model="requestType" type="radio" value="bug"><span><b>Report a bug</b><small>Something is not working</small></span></label>
            <label><input v-model="requestType" type="radio" value="feature"><span><b>Request a feature</b><small>Suggest an improvement</small></span></label>
            <label><input v-model="requestType" type="radio" value="support"><span><b>Ask for help</b><small>Get general assistance</small></span></label>
          </fieldset>

          <div class="field-grid">
            <label class="field"><span>Product</span><select v-model="productId" required :disabled="loadingProducts || sending"><option v-if="loadingProducts" value="">Loading products...</option><option v-for="product in products" :key="product.id" :value="product.id">{{ product.name }}</option></select></label>
            <label class="field"><span>Version <em>optional</em></span><input v-model="version" maxlength="80" autocomplete="off" :disabled="sending" placeholder="For example 0.1.0-alpha.2"></label>
          </div>
          <label class="field"><span>Short summary</span><input v-model="title" required minlength="4" maxlength="160" :disabled="sending" placeholder="What needs attention?"></label>
          <label class="field"><span>Details</span><textarea v-model="description" required minlength="10" maxlength="8000" rows="7" :disabled="sending" placeholder="What did you do, what did you expect, and what happened instead?"></textarea><small>Do not include passwords, access tokens, or private file contents.</small></label>
          <label class="field"><span>Contact method <em>optional</em></span><input v-model="contact" maxlength="320" autocomplete="email" :disabled="sending" placeholder="Email address or another way to reach you"><small>Anonymous requests are welcome, but we cannot ask follow-up questions.</small></label>
          <label v-if="diagnosticsSupported" class="field diagnostic-field"><span>Diagnostic report <em>optional</em></span><input type="file" accept="application/zip,.zip" :disabled="sending" @change="chooseDiagnostics"><small>Maximum 4 MiB. Diagnostic files stay private and expire automatically.</small></label>
          <label class="consent"><input v-model="privacyAccepted" type="checkbox" required :disabled="sending"><span>I understand this request is sent privately to Obiente Support. I have removed secrets and information I do not want to share.</span></label>
          <div class="submit-actions">
            <button class="primary" type="submit" :disabled="sending || loadingProducts"><span>{{ sending ? progressLabel : 'Send private request' }}</span><span aria-hidden="true">&#8594;</span></button>
            <button v-if="sending" class="secondary" type="button" @click="cancel">Cancel</button>
          </div>
          <progress v-if="sending && progress !== undefined" :value="progress" max="1"><span>{{ Math.round(progress * 100) }}%</span></progress>
          <p class="form-status" role="status" aria-live="polite">{{ message }}</p>
        </form>

        <section v-else class="receipt" tabindex="-1">
          <div class="receipt-icon" aria-hidden="true">&#10003;</div>
          <div class="eyebrow">Request received</div>
          <h2>Keep this support code</h2>
          <output class="support-code">{{ receipt.supportCode }}</output>
          <p>Your private link is the only way to check or delete an anonymous request. Save it somewhere safe.</p>
          <div class="receipt-actions"><a class="primary" :href="receipt.statusUrl">Open private status</a><button class="secondary" type="button" @click="copyCode">Copy code</button></div>
          <p class="form-status" role="status" aria-live="polite">{{ message }}</p>
        </section>
      </div>
    </section>
  </main>
</template>
