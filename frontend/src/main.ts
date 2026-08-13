import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import '@fontsource-variable/alexandria'
import App from './App.vue'
import SupportIntakeView from './views/SupportIntakeView.vue'
import PrivateStatusView from './views/PrivateStatusView.vue'
import AdminLoginView from './views/AdminLoginView.vue'
import AdminReportsView from './views/AdminReportsView.vue'
import './styles.css'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'intake', component: SupportIntakeView },
    { path: '/r/:capability', name: 'private-status', component: PrivateStatusView },
    { path: '/admin/login', name: 'admin-login', component: AdminLoginView },
    { path: '/admin', name: 'admin-reports', component: AdminReportsView },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

createApp(App).use(router).mount('#app')
