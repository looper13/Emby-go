import { createApp } from 'vue';
import { createPinia } from 'pinia';
import App from './App.vue';
import { createAppRouter } from './router';
import { replaceLegacyUrl } from './router/legacy-url';
import { viewHistories, viewHistoryKey } from './router/view-history';
import './styles/legacy.css';

replaceLegacyUrl();
window.history.scrollRestoration = 'manual';
const pinia = createPinia();
const router = createAppRouter(pinia);
createApp(App).provide(viewHistoryKey, viewHistories.get(router)!).use(pinia).use(router).mount('#app');
