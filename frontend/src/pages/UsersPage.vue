<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import { Plus, RefreshCw, Save, RotateCcw, Trash2, UserRound, Shield } from '@lucide/vue';
import { usersApi, type ManagedUser } from '../api/users';
import { useAdminRead } from '../composables/useAdminRead';
import { useAuthStore } from '../stores/auth';
import { useToastStore } from '../stores/toasts';
import { useMediaWallStore } from '../stores/media-wall';
import ReadState from '../components/common/ReadState.vue';

const { data, loading, error, load, active } = useAdminRead(usersApi.list);
const auth = useAuthStore(), toasts = useToastStore(), wall = useMediaWallStore(), router = useRouter();
const selectedId = ref(''), creating = ref(false), busy = ref(false), saveError = ref('');
const username = ref(''), password = ref(''), confirmation = ref(''), disabled = ref(false);
const allFolders = ref(false), folders = ref<string[]>([]);
const selected = computed(() => data.value?.users.find(user => user.Id === selectedId.value));
const credentialDirty = computed(() => creating.value ? Boolean(username.value || password.value || confirmation.value)
  : Boolean(selected.value && (username.value !== selected.value.Name || disabled.value !== selected.value.Policy.IsDisabled || password.value || confirmation.value)));
const policyDirty = computed(() => Boolean(!creating.value && selected.value && (allFolders.value !== selected.value.Policy.EnableAllFolders
  || JSON.stringify([...folders.value].sort()) !== JSON.stringify([...selected.value.Policy.EnabledFolders].sort()))));
const dirty = computed(() => credentialDirty.value || policyDirty.value);

function resetCredentials() {
  username.value = creating.value ? '' : selected.value?.Name ?? '';
  disabled.value = creating.value ? false : selected.value?.Policy.IsDisabled ?? false;
  password.value = ''; confirmation.value = '';
}
function resetPolicy() {
  allFolders.value = selected.value?.Policy.EnableAllFolders ?? false;
  folders.value = [...(selected.value?.Policy.EnabledFolders ?? [])];
}
function reset() { resetCredentials(); resetPolicy(); saveError.value = ''; }
function canLeave() { return !auth.authenticated || (!busy.value && (!dirty.value || window.confirm('放弃未保存的用户设置？'))); }
onBeforeRouteLeave(canLeave);
function select(id: string, isNew = false) {
  if (busy.value || !canLeave()) return;
  creating.value = isNew; selectedId.value = id; reset();
}
watch(data, value => {
  if (!value || creating.value || value.users.some(user => user.Id === selectedId.value)) return;
  selectedId.value = value.users[0]?.Id ?? ''; reset();
});
async function refresh() {
  if (busy.value || !canLeave()) return;
  await load(); if (active() && !error.value) reset();
}
function replaceUser(user: ManagedUser) {
  if (data.value) data.value = { ...data.value, users: data.value.users.map(current => current.Id === user.Id ? user : current) };
}
async function saveCredentials() {
  if (busy.value || !data.value || !auth.isAdministrator) return;
  saveError.value = '';
  const name = username.value.trim();
  if (Array.from(name).length < 3 || Array.from(name).length > 64) { saveError.value = '用户名须为 3-64 个字符'; return; }
  if (password.value !== confirmation.value) { saveError.value = '两次输入的密码不一致'; return; }
  if ((creating.value || password.value) && (Array.from(password.value).length < 9 || new TextEncoder().encode(password.value).length > 72)) {
    saveError.value = '密码至少 9 个字符且不超过 72 字节'; return;
  }
  const user = selected.value, isNew = creating.value, token = auth.token;
  if (!isNew && (!user || !window.confirm(`保存后，${user.Name} 的已登录会话将失效。继续？`))) return;
  busy.value = true;
  try {
    if (isNew) {
      const created = await usersApi.create(name, password.value);
      if (!active()) return;
      data.value = { ...data.value, users: [...data.value.users, created] };
      creating.value = false; selectedId.value = created.Id; reset();
      toasts.show('用户已创建');
    } else if (user) {
      const payload = { Username: name, NewPassword: password.value, IsDisabled: disabled.value };
      await usersApi.update(user.Id, payload);
      if (!active()) return;
      replaceUser({ ...user, Name: name, Policy: { ...user.Policy, IsDisabled: payload.IsDisabled } });
      resetCredentials();
      if (user.Id === auth.user?.Id) {
        auth.clearSession(token); toasts.show('账号已更新，请重新登录'); await router.replace('/login');
      } else toasts.show('用户已保存');
    }
  } catch (cause) { if (active()) saveError.value = cause instanceof Error ? cause.message : '保存失败'; }
  finally { busy.value = false; }
}
async function savePolicy() {
  const user = selected.value;
  if (busy.value || !user || !auth.isAdministrator) return;
  const policy = { EnableAllFolders: allFolders.value, EnabledFolders: [...folders.value] };
  busy.value = true; saveError.value = '';
  try {
    await usersApi.savePolicy(user.Id, policy);
    if (!active()) return;
    replaceUser({ ...user, Policy: { ...user.Policy, ...policy } });
    if (user.Id === auth.user?.Id) wall.clear();
    toasts.show('媒体库权限已保存');
  } catch (cause) { if (active()) saveError.value = cause instanceof Error ? cause.message : '保存失败'; }
  finally { busy.value = false; }
}
async function remove() {
  const user = selected.value;
  if (busy.value || !user || user.Id === '1' || !auth.isAdministrator) return;
  if (!window.confirm(`删除用户 ${user.Name} 及其收藏和播放记录？媒体文件不会删除。`)) return;
  busy.value = true; saveError.value = '';
  try {
    await usersApi.remove(user.Id);
    if (!active() || !data.value) return;
    data.value = { ...data.value, users: data.value.users.filter(current => current.Id !== user.Id) };
    selectedId.value = data.value.users[0]?.Id ?? ''; reset(); toasts.show('用户已删除');
  } catch (cause) { if (active()) saveError.value = cause instanceof Error ? cause.message : '删除失败'; }
  finally { busy.value = false; }
}
</script>

<template>
  <div class="users-toolbar">
    <span>{{ data?.users.length ?? 0 }} 个用户</span>
    <div class="users-actions">
      <button class="icon-btn" type="button" title="刷新用户" aria-label="刷新用户" :disabled="busy || loading" @click="refresh"><RefreshCw :size="18" /></button>
      <button class="btn btn-accent" type="button" :disabled="busy || !data" @click="select('', true)"><Plus :size="17" /><span>新增用户</span></button>
    </div>
  </div>
  <ReadState :loading="loading" :error="error" :empty="!data" @retry="refresh" />
  <div v-if="data" class="users-workspace">
    <nav class="users-list" aria-label="用户列表">
      <button v-for="user in data.users" :key="user.Id" type="button" class="user-row" :class="{ selected: !creating && selectedId === user.Id }"
        :aria-pressed="!creating && selectedId === user.Id" :disabled="busy" :data-user-id="user.Id" @click="select(user.Id)">
        <component :is="user.Policy.IsAdministrator ? Shield : UserRound" :size="20" aria-hidden="true" />
        <span><strong>{{ user.Name }}</strong><small>{{ user.Policy.IsAdministrator ? '管理员' : '普通用户' }} · {{ user.Policy.IsDisabled ? '已禁用' : '启用' }}</small></span>
      </button>
    </nav>
    <div v-if="creating || selected" class="user-editor">
      <div class="user-heading"><h2>{{ creating ? '新增用户' : selected?.Name }}</h2><span v-if="!creating" class="user-id">ID {{ selectedId }}</span></div>
      <p v-if="saveError" class="user-error" role="alert">{{ saveError }}</p>
      <form class="user-section" data-form="credentials" @submit.prevent="saveCredentials">
        <div class="user-fields">
          <label>用户名<input v-model="username" name="username" required minlength="3" maxlength="64" autocomplete="off" :disabled="busy" /></label>
          <label v-if="!creating && selectedId !== '1'" class="user-choice"><input v-model="disabled" name="disabled" type="checkbox" :disabled="busy" />禁用账号</label>
          <label>{{ creating ? '密码' : '重置密码（可选）' }}<input v-model="password" name="password" type="password" autocomplete="new-password" minlength="9" :required="creating" :disabled="busy" /></label>
          <label>确认密码<input v-model="confirmation" name="confirmation" type="password" autocomplete="new-password" :required="creating || Boolean(password)" :disabled="busy" /></label>
        </div>
        <div class="users-actions"><button class="btn btn-accent" :disabled="busy || (!creating && !credentialDirty)"><Save :size="16" /><span>{{ creating ? '创建用户' : '保存账号' }}</span></button><button class="icon-btn" type="button" title="恢复账号设置" aria-label="恢复账号设置" :disabled="busy || !credentialDirty" @click="resetCredentials"><RotateCcw :size="17" /></button></div>
      </form>
      <form v-if="!creating && selected" class="user-section" data-form="policy" @submit.prevent="savePolicy">
        <h3>媒体库访问权限</h3>
        <label class="user-choice"><input v-model="allFolders" type="checkbox" name="allFolders" :disabled="busy" />允许访问所有媒体库</label>
        <fieldset :disabled="busy || allFolders"><legend>指定媒体库</legend><label v-for="library in data.libraries" :key="library.Id" class="user-choice"><input v-model="folders" type="checkbox" :value="library.Id" /><span>{{ library.Name }}</span></label><p v-if="!data.libraries.length">暂无媒体库</p></fieldset>
        <p v-if="!allFolders && !folders.length" class="user-warning">未授权任何媒体库</p>
        <div class="users-actions"><button class="btn btn-accent" :disabled="busy || !policyDirty"><Save :size="16" /><span>保存权限</span></button><button class="icon-btn" type="button" title="恢复媒体库权限" aria-label="恢复媒体库权限" :disabled="busy || !policyDirty" @click="resetPolicy"><RotateCcw :size="17" /></button></div>
      </form>
      <div v-if="!creating && selectedId !== '1'" class="user-section"><button class="btn danger" type="button" :disabled="busy" @click="remove"><Trash2 :size="16" /><span>删除用户</span></button></div>
    </div>
  </div>
</template>

<style scoped>
.users-toolbar,.users-actions,.user-heading { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.users-toolbar { justify-content: space-between; margin-bottom: 20px; color: var(--dim); font-size: 13.5px; }
.users-workspace { display: grid; grid-template-columns: minmax(200px, 260px) minmax(0, 1fr); gap: 24px; max-width: 1100px; }
.users-list { border-right: 1px solid var(--line); padding-right: 18px; display: flex; flex-direction: column; gap: 6px; }
.user-row { display: flex; align-items: center; gap: 12px; width: 100%; min-height: 56px; padding: 10px 14px; text-align: left; background: transparent; border: 1px solid transparent; border-radius: 8px; color: inherit; cursor: pointer; transition: all 0.15s ease; }
.user-row:hover { background: var(--panel-2); }
.user-row.selected { background: var(--accent) !important; color: #ffffff !important; box-shadow: 0 2px 8px rgba(43, 122, 254, 0.3); }
.user-row.selected strong { color: #ffffff !important; }
.user-row.selected small { color: rgba(255, 255, 255, 0.85) !important; }
.user-row.selected svg { color: #ffffff !important; }
.user-row > span { min-width: 0; }
.user-row strong { display: block; font-size: 13.5px; font-weight: 600; color: var(--text); overflow-wrap: anywhere; }
.user-row small { display: block; margin-top: 4px; color: var(--dim); font-size: 12px; overflow-wrap: anywhere; }
.user-row svg { flex-shrink: 0; color: var(--muted); }
.user-editor { min-width: 0; }
.user-heading { margin-bottom: 18px; }
.user-heading h2 { font-size: 20px; font-weight: 700; margin: 0; color: var(--text); overflow-wrap: anywhere; }
.user-id { color: var(--dim); font-size: 12px; font-family: var(--font-mono); background: var(--panel-2); padding: 3px 8px; border-radius: 4px; border: 1px solid var(--line); }
.user-section { padding: 22px 24px; border: 1px solid var(--line); border-radius: 8px; background: var(--panel); box-shadow: var(--shadow); margin-bottom: 20px; }
.user-section h3 { margin: 0 0 16px; font-size: 16px; font-weight: 600; color: var(--text); }
.user-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-bottom: 20px; }
.user-fields > label { display: flex; flex-direction: column; gap: 6px; font-size: 13px; font-weight: 500; color: var(--muted); }
.user-fields input:not([type=checkbox]) { width: 100%; min-width: 0; }
.user-choice, .user-fields > .user-choice { display: flex; flex-direction: row; align-items: center; gap: 10px; min-height: 38px; font-size: 13.5px; color: var(--text); overflow-wrap: anywhere; cursor: pointer; }
.user-choice input { width: 17px; height: 17px; flex: 0 0 17px; accent-color: var(--accent); }
fieldset { border: 1px solid var(--line); border-radius: 8px; padding: 14px 18px; margin: 16px 0; background: var(--panel-2); min-width: 0; }
fieldset:disabled { opacity: .55; }
legend { font-size: 12px; font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; color: var(--dim); padding: 0 6px; }
.user-error { color: var(--danger); background: var(--danger-soft); border: 1px solid rgba(250, 82, 82, 0.25); border-radius: 6px; padding: 10px 14px; font-size: 13px; margin: 0 0 16px; overflow-wrap: anywhere; }
.user-warning { color: var(--warn); background: rgba(250, 176, 5, 0.12); border: 1px solid rgba(250, 176, 5, 0.25); border-radius: 6px; padding: 10px 14px; font-size: 13px; margin: 12px 0; }
.users-actions .btn, .user-section > .btn { display: inline-flex; align-items: center; gap: 7px; }
@media(max-width:760px) {
  .users-workspace { grid-template-columns: minmax(0, 1fr); gap: 20px; }
  .users-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); border-right: 0; border-bottom: 1px solid var(--line); padding: 0 0 16px; }
  .user-fields { grid-template-columns: minmax(0, 1fr); }
  .user-row { padding: 10px 8px; min-height: auto; }
  .user-heading h2 { font-size: 18px; }
}
</style>
