<script setup lang="ts">
import { RouterLink, RouterView } from 'vue-router'
</script>

<template>
  <a class="skip-link" href="#main-content">Skip to content</a>
  <header class="site-header">
    <RouterLink class="brand" to="/" aria-label="Obiente Support home">
      <span class="brand-mark" aria-hidden="true">O</span>
      <span><strong>Obiente</strong><small>Support</small></span>
    </RouterLink>
    <nav aria-label="Support navigation">
      <RouterLink to="/#how-it-works">How it works</RouterLink>
      <a href="https://obiente.org">Obiente.org</a>
    </nav>
  </header>
  <RouterView />
  <footer>
    <span>Obiente Support</span>
    <span>Private, human-reviewed product support.</span>
    <a href="https://github.com/Obiente/support">Source code</a>
  </footer>
</template>
