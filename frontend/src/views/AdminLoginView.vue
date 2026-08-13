<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { adminLogin, loadAdminSession } from '../support'

const router = useRouter()
const username = ref('')
const password = ref('')
const checking = ref(true)
const signingIn = ref(false)
const message = ref('')

onMounted(async () => {
  try {
    await loadAdminSession()
    await router.replace('/admin')
  } catch {
    checking.value = false
  }
})

async function signIn() {
  if (signingIn.value) return
  signingIn.value = true
  message.value = ''
  try {
    await adminLogin(username.value, password.value)
    password.value = ''
    await router.replace('/admin')
  } catch (error) {
    message.value = error instanceof Error ? error.message : 'Sign-in is temporarily unavailable.'
  } finally {
    signingIn.value = false
  }
}
</script>

<template>
  <main id="main-content" class="admin-login-main">
    <section class="admin-login" aria-labelledby="admin-login-title">
      <div class="eyebrow">Maintainer access</div>
      <h1 id="admin-login-title">Sign in to support</h1>
      <p>Review private requests and diagnostic reports. Access is recorded for the support audit log.</p>
      <form :aria-busy="checking" @submit.prevent="signIn">
        <label class="field"><span>Username</span><input v-model="username" required autocomplete="username" :disabled="checking || signingIn"></label>
        <label class="field"><span>Password</span><input v-model="password" required type="password" autocomplete="current-password" :disabled="checking || signingIn"></label>
        <button class="primary" type="submit" :disabled="checking || signingIn">{{ checking ? 'Checking session...' : signingIn ? 'Signing in...' : 'Sign in' }}</button>
        <p class="form-status" role="status" aria-live="polite">{{ message }}</p>
      </form>
    </section>
  </main>
</template>
