<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { cloud115Api, type CloudConfig } from '../../api/cloud115';
import { usePageRequest } from '../../composables/usePageRequest';
const scope = usePageRequest(), config = ref<CloudConfig | null>(null);
const access = ref(''), refresh = ref(''), folder = ref(''), busy = ref(false), error = ref(''), message = ref('');
async function load() {
  const request = scope.begin(); error.value = '';
  try { const next = await cloud115Api.config(request.signal); if (request.isCurrent()) { config.value = next; folder.value = next.folder_id; } }
  catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '115 配置加载失败'; }
}
async function save() {
  if (busy.value || !config.value) return;
  const request = scope.begin(); busy.value = true; error.value = ''; message.value = '';
  try {
    await cloud115Api.save({ access_token: access.value, refresh_token: refresh.value, folder_id: folder.value, revision: config.value.revision }, request.signal);
    if (!request.isCurrent()) return;
    access.value = ''; refresh.value = ''; message.value = '115 连接已验证并保存；需要重新授权的任务可在影片详情重试。';
    config.value = await cloud115Api.config(request.signal);
  } catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '115 配置保存失败'; }
  finally { if (request.isCurrent()) busy.value = false; }
}
onMounted(load);
</script>
<template>
  <section class="panel cloud-settings">
    <div class="panel-head"><h2>115 连接</h2><span class="hint">R18 离线下载与直链播放</span></div>
    <p class="hint">管理员提交下载，已授权用户播放就绪文件。Token 加密保存在本地，不回显；工作目录必须是现有的非根文件夹。</p>
    <p v-if="config?.configured" class="hint">已配置账号 {{ config.account_id }}。留空 Token 保留既有凭证。</p>
    <form class="cloud-form" @submit.prevent="save">
      <label for="cloud-access">Access Token<input id="cloud-access" v-model="access" type="password" autocomplete="new-password" :required="!config?.configured" maxlength="8192" :disabled="busy" /></label>
      <label for="cloud-refresh">Refresh Token<input id="cloud-refresh" v-model="refresh" type="password" autocomplete="new-password" :required="!config?.configured" maxlength="8192" :disabled="busy" /></label>
      <label for="cloud-folder">工作文件夹 ID<input id="cloud-folder" v-model="folder" inputmode="numeric" pattern="[1-9][0-9]*" required maxlength="32" :disabled="busy" /></label>
      <button class="btn btn-accent" :disabled="busy || !config">{{ busy ? '正在验证连接…' : '验证并保存 115 连接' }}</button>
    </form>
    <p v-if="error" class="cloud-error" role="alert">{{ error }} <button v-if="!config" class="btn btn-sm" @click="load">重新加载</button></p>
    <p v-if="message" role="status" class="hint">{{ message }}</p>
    <p class="hint">仅获取临时直链并 302 跳转，不转码、不自动删除网盘文件。请同时备份 SQLite 和数据目录内的 .115.key；丢失密钥无法解密 Token。</p>
  </section>
</template>
<style scoped>
.cloud-settings { margin-top:24px; }.cloud-form { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }.cloud-form label { display:grid; gap:8px; font-size:13px; }.cloud-form input { width:100%; min-width:0; }.cloud-form button { align-self:end; min-height:44px; }.cloud-error { color:var(--danger); overflow-wrap:anywhere; }@media(max-width:720px) { .cloud-form { grid-template-columns:1fr; } }
</style>
