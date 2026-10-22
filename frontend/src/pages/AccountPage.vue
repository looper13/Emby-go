<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import { Save, RotateCcw } from '@lucide/vue';
import { accountApi } from '../api/account';
import { useAdminRead } from '../composables/useAdminRead';
import { useAuthStore } from '../stores/auth';
import { useMediaWallStore } from '../stores/media-wall';
import { useToastStore } from '../stores/toasts';
import ReadState from '../components/common/ReadState.vue';

const { data, loading, error, load, active } = useAdminRead(accountApi.get);
const auth = useAuthStore(), wall = useMediaWallStore(), toasts = useToastStore(), router = useRouter();
const username = ref(''), currentPassword = ref(''), newPassword = ref(''), confirmation = ref('');
const allFolders = ref(true), folders = ref<string[]>([]);
const saving = ref(false), credentialSaving = ref(false), policyError = ref(''), credentialError = ref('');
const busy = computed(() => saving.value || credentialSaving.value);
const policyDirty = computed(() => Boolean(data.value && (allFolders.value !== data.value.policy.EnableAllFolders
  || JSON.stringify([...folders.value].sort()) !== JSON.stringify([...data.value.policy.EnabledFolders].sort()))));
const credentialsDirty = computed(() => Boolean(data.value && (username.value !== data.value.user.Name || currentPassword.value || newPassword.value || confirmation.value)));

function resetPolicy() {
  if (!data.value) return;
  allFolders.value = data.value.policy.EnableAllFolders;
  folders.value = [...data.value.policy.EnabledFolders];
  policyError.value = '';
}
watch(() => data.value?.user.Name, value => { if (value) username.value = value; });
watch(data, resetPolicy);
onBeforeRouteLeave(() => !auth.authenticated || !(policyDirty.value || credentialsDirty.value) || window.confirm('放弃未保存的账号设置？'));

async function savePolicy() {
  if (busy.value || !data.value || !auth.isAdministrator) return;
  saving.value = true; policyError.value = '';
  try {
    const policy = { EnableAllFolders: allFolders.value, EnabledFolders: [...folders.value] };
    await accountApi.savePolicy(data.value.user.Id, policy);
    if (!active()) return;
    data.value = { ...data.value, policy };
    wall.clear();
    toasts.show('媒体库权限已保存');
  } catch (cause) { policyError.value = cause instanceof Error ? cause.message : '保存失败'; }
  finally { saving.value = false; }
}

async function saveCredentials() {
  if (busy.value || !data.value) return;
  credentialError.value = '';
  if (policyDirty.value) { credentialError.value = '请先保存或恢复媒体库权限'; return; }
  if (newPassword.value !== confirmation.value) { credentialError.value = '两次输入的新密码不一致'; return; }
  if (newPassword.value && (Array.from(newPassword.value).length < 9 || new TextEncoder().encode(newPassword.value).length > 72)) {
    credentialError.value = '新密码至少 9 个字符且不超过 72 字节'; return;
  }
  if (!window.confirm('保存账号后，所有已登录设备都需要重新登录。继续？')) return;
  const token = auth.token;
  credentialSaving.value = true;
  try {
    await accountApi.update({ Username: username.value.trim(), CurrentPassword: currentPassword.value, NewPassword: newPassword.value });
    if (!active() || auth.token !== token) return;
    if (data.value) data.value = { ...data.value, user: { ...data.value.user, Name: username.value.trim() } };
    currentPassword.value = ''; newPassword.value = ''; confirmation.value = '';
    auth.clearSession(token);
    toasts.show('账号已更新，请重新登录');
    await router.replace('/login');
  } catch (cause) { credentialError.value = cause instanceof Error ? cause.message : '保存失败'; }
  finally { credentialSaving.value = false; }
}
</script>

<template>
  <div class="account-settings">
    <ReadState :loading="loading" :error="error" :empty="!data" @retry="load" />
    <template v-if="data">
      <section class="account-section">
        <h2>媒体库访问权限</h2>
        <form v-if="auth.isAdministrator" @submit.prevent="savePolicy">
          <label class="account-choice"><input v-model="allFolders" type="checkbox" :disabled="busy"><span>允许访问所有媒体库</span></label>
          <fieldset class="account-libraries" :disabled="allFolders || busy">
            <legend>指定媒体库</legend>
            <label v-for="library in data.libraries" :key="library.Id" class="account-choice">
              <input v-model="folders" type="checkbox" :value="library.Id"><span>{{ library.Name }}</span>
            </label>
            <p v-if="!data.libraries.length" class="hint">暂无媒体库</p>
          </fieldset>
          <p v-if="!allFolders && !folders.length" class="account-warning" role="status">未授权任何媒体库</p>
          <p v-if="policyError" class="account-error" role="alert">{{ policyError }}</p>
          <div class="account-actions">
            <button class="btn btn-accent" :disabled="busy || !policyDirty"><Save :size="15" /><span>{{ saving ? '保存中' : '保存权限' }}</span></button>
            <button type="button" class="btn" :disabled="busy || !policyDirty" @click="resetPolicy"><RotateCcw :size="15" /><span>恢复</span></button>
          </div>
        </form>
        <template v-else>
          <ul v-if="data.libraries.length"><li v-for="library in data.libraries" :key="library.Id">{{ library.Name }}</li></ul>
          <p v-else>未授权任何媒体库</p>
        </template>
      </section>
      <section class="account-section">
        <h2>登录账号</h2>
        <form class="account-credentials" @submit.prevent="saveCredentials">
          <div class="field"><label for="account-name">用户名</label><input id="account-name" v-model="username" autocomplete="username" required minlength="3" maxlength="64" :disabled="busy"></div>
          <div class="field"><label for="account-current">当前密码</label><input id="account-current" v-model="currentPassword" type="password" autocomplete="current-password" required :disabled="busy"></div>
          <div class="field"><label for="account-new">新密码（可选）</label><input id="account-new" v-model="newPassword" type="password" autocomplete="new-password" minlength="9" :disabled="busy"></div>
          <div class="field"><label for="account-confirm">确认新密码</label><input id="account-confirm" v-model="confirmation" type="password" autocomplete="new-password" :required="Boolean(newPassword)" :disabled="busy"></div>
          <p v-if="credentialError" class="account-error" role="alert">{{ credentialError }}</p>
          <div class="account-actions"><button class="btn btn-accent" :disabled="busy || !currentPassword"><Save :size="15" /><span>{{ credentialSaving ? '保存中' : '保存账号' }}</span></button></div>
        </form>
      </section>
    </template>
  </div>
</template>

<style scoped>
.account-settings { max-width: 860px; }
.account-section { padding: 22px 24px; margin-bottom: 24px; border: 1px solid var(--line); border-radius: 8px; background: var(--panel); box-shadow: var(--shadow); }
.account-section h2 { font-size: 18px; font-weight: 600; margin: 0 0 18px; color: var(--text); border-bottom: 1px solid var(--line); padding-bottom: 12px; }
.account-choice { display: flex; align-items: center; gap: 10px; min-height: 38px; font-size: 13.5px; color: var(--text); cursor: pointer; }
.account-choice input { flex: 0 0 17px; width: 17px; height: 17px; accent-color: var(--accent); }
.account-choice span { overflow-wrap: anywhere; }
.account-libraries { margin: 16px 0; padding: 14px 18px; border: 1px solid var(--line); border-radius: 8px; background: var(--panel-2); }
.account-libraries:disabled { opacity: .55; }
.account-libraries legend { font-size: 12px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; color: var(--dim); padding: 0 6px; }
.account-credentials { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.account-credentials .field { display: flex; flex-direction: column; gap: 6px; }
.account-credentials label { font-size: 13px; font-weight: 500; color: var(--muted); }
.account-credentials input { width: 100%; min-width: 0; }
.account-actions { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 18px; grid-column: 1 / -1; }
.account-error { color: var(--danger); background: var(--danger-soft); border: 1px solid rgba(250, 82, 82, 0.25); border-radius: 6px; padding: 10px 14px; font-size: 13px; margin: 12px 0; grid-column: 1 / -1; overflow-wrap: anywhere; }
.account-warning { color: var(--warn); background: rgba(250, 176, 5, 0.12); border: 1px solid rgba(250, 176, 5, 0.25); border-radius: 6px; padding: 10px 14px; font-size: 13px; margin: 12px 0; }
@media (max-width: 620px) { .account-credentials { grid-template-columns: minmax(0, 1fr); } }
</style>
