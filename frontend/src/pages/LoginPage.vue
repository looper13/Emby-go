<script setup lang="ts">
import { computed, nextTick, onMounted, onScopeDispose, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { RefreshCw } from '@lucide/vue';
import { useAuthStore } from '../stores/auth';
import { resolveLoginTarget } from '../router/legacy-url';
import AuthLayout from '../layouts/AuthLayout.vue';

const auth = useAuthStore();
const route = useRoute();
const router = useRouter();
const username = ref('');
const password = ref('');
const confirmation = ref('');
const message = ref('');
const checking = ref(true);
const needsRetry = ref(false);
const usernameInput = ref<HTMLInputElement | null>(null);
const initializing = computed(() => auth.initialized === false);
const busy = computed(() => checking.value || auth.submitting);
let active = true;
onScopeDispose(() => { active = false; });

async function boot(retry = false) {
  if (!active) return;
  checking.value = true;
  needsRetry.value = false;
  message.value = '';
  try {
    if (auth.token) {
      if (!retry && auth.phase === 'unavailable') {
        needsRetry.value = true;
        message.value = auth.error;
        return;
      }
      if (await auth.checkSession()) {
        if (active) await router.replace(resolveLoginTarget(route.query.returnTo));
        return;
      }
    }
    await auth.loadStatus();
    if (active && auth.error) message.value = auth.error;
  } catch {
    if (active) {
      message.value = '无法连接服务，请重试';
      needsRetry.value = true;
    }
  } finally {
    if (active) {
      checking.value = false;
      // Wait for disabled to be removed before focusing the unchanged input.
      void nextTick(() => {
        if (active) usernameInput.value?.focus();
      });
    }
  }
}

async function submit() {
  if (busy.value || needsRetry.value) return;
  message.value = '';
  if (Array.from(username.value.trim()).length < 3 || Array.from(password.value).length < 9) {
    message.value = '账号至少 3 个字符，密码至少 9 个字符';
    return;
  }
  if (initializing.value && password.value !== confirmation.value) {
    message.value = '两次输入的密码不一致';
    return;
  }
  const credentials = { Username: username.value, Pw: password.value };
  try {
    if (initializing.value) {
      await auth.initialize(credentials);
      if (active) message.value = '初始化完成，请使用新账户登录';
    } else if (await auth.login(credentials)) {
      if (active) await router.replace(resolveLoginTarget(route.query.returnTo));
    }
  } catch (error) {
    if (active) message.value = error instanceof Error ? error.message : '网络错误';
  }
}

onMounted(() => { void boot(); });
</script>

<template>
  <AuthLayout>
    <section class="auth-card">
      <p class="eyebrow">Emby-go · Archive Console</p>
      <h1>{{ initializing ? '建立管理员账户' : '欢迎回来' }}</h1>
      <p class="auth-copy">{{ initializing
        ? '首次运行需要创建唯一管理员账户。账户信息仅保存在本机 SQLite 数据库中，不外传、不可恢复。'
        : '使用管理员账户进入媒体库控制台。浏览海报墙请使用 Yamby / iPlay 等 Emby 客户端。' }}</p>
      <form id="auth-form" class="auth-form" @submit.prevent="submit">
        <div class="field">
          <label for="auth-user">管理员账号</label>
          <input
            id="auth-user" ref="usernameInput" v-model="username" name="Username" autocomplete="username"
            minlength="3" required :disabled="busy" autofocus placeholder="至少 3 个字符"
          >
        </div>
        <div class="field">
          <label for="auth-pw">密码</label>
          <input
            id="auth-pw" v-model="password" name="Pw" type="password"
            :autocomplete="initializing ? 'new-password' : 'current-password'"
            minlength="9" required :disabled="busy" placeholder="至少 9 个字符"
          >
        </div>
        <div v-if="initializing" class="field">
          <label for="auth-confirm">确认密码</label>
          <input
            id="auth-confirm" v-model="confirmation" name="confirm" type="password"
            autocomplete="new-password" required :disabled="busy" placeholder="再次输入密码"
          >
        </div>
        <p class="auth-error" role="alert">{{ message }}</p>
        <button class="btn btn-accent" type="submit" :disabled="busy || needsRetry">
          {{ busy ? '请稍候…' : (initializing ? '完成初始化' : '登录管理后台') }}
        </button>
        <button v-if="needsRetry" class="btn" type="button" :disabled="busy" @click="boot(true)">
          <RefreshCw :size="15" :stroke-width="1.8" aria-hidden="true" /><span>重试</span>
        </button>
      </form>
      <p class="auth-note">NFO 为元数据真源 · 仅 http/https .strm 进入 Emby</p>
    </section>
  </AuthLayout>
</template>
