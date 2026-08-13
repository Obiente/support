<script setup lang="ts">
import { RouterLink, RouterView } from 'vue-router'
</script>

<template>
  <a class="skip-link" href="#main-content">Skip to content</a>
  <header class="site-header">
    <RouterLink class="brand" to="/" aria-label="Obiente Support home">
      <strong>Obiente</strong>
      <span>Support</span>
    </RouterLink>
    <nav aria-label="Support navigation">
      <a href="https://obiente.org">Obiente.org</a>
      <a href="https://github.com/Obiente/support">Source code</a>
    </nav>
  </header>
  <RouterView />
  <footer>
    <span>Obiente Support</span>
    <span>Private support for every Obiente project.</span>
    <RouterLink to="/admin/login">Maintainer sign in</RouterLink>
    <a href="https://github.com/Obiente/support">Source code</a>
  </footer>
</template>
