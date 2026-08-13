import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import SupportIntakeView from './views/SupportIntakeView.vue'
import PrivateStatusView from './views/PrivateStatusView.vue'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'intake', component: SupportIntakeView },
    { path: '/r/:capability', name: 'private-status', component: PrivateStatusView },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

createApp(App).use(router).mount('#app')
