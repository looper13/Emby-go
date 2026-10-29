<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { cloud115Api, type CloudConfig } from '../../api/cloud115';
import { usePageRequest } from '../../composables/usePageRequest';
const scope = usePageRequest(), config = ref<CloudConfig | null>(null);
const cookie = ref(''), busy = ref(false), error = ref(''), message = ref('');
async function load() {
  const request = scope.begin(); error.value = '';
  try { const next = await cloud115Api.config(request.signal); if (request.isCurrent()) config.value = next; }
  catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '115 配置加载失败'; }
}
async function save() {
  if (busy.value || !config.value) return;
  const request = scope.begin(); busy.value = true; error.value = ''; message.value = '';
  try {
    await cloud115Api.save({ cookie: cookie.value, revision: config.value.revision }, request.signal);
    if (!request.isCurrent()) return;
    cookie.value = ''; message.value = '115 连接已验证并保存，缓存目录已自动准备；需要重新授权的任务可在影片详情重试。';
    config.value = await cloud115Api.config(request.signal);
  } catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '115 配置保存失败'; }
  finally { if (request.isCurrent()) busy.value = false; }
}
onMounted(load);
</script>
<template>
  <section class="panel cloud-settings">
    <div class="panel-head"><h2>115 连接</h2><span class="hint">R18 离线下载与直链播放</span></div>
    <p class="hint">只需填写 115 Cookie（包含 UID、CID、SEID，可包含 KID），加密保存在本地，不回显。缓存目录自动创建，无需填写文件夹 ID。</p>
    <p v-if="config?.configured" class="hint">已配置账号 {{ config.account_id }}。留空 Cookie 保留既有凭证；旧 Token 配置需重新填写 Cookie。</p>
    <form class="cloud-form" @submit.prevent="save">
      <label for="cloud-cookie">115 Cookie<input id="cloud-cookie" v-model="cookie" type="password" autocomplete="new-password" :required="!config?.configured" maxlength="16384" :disabled="busy" placeholder="UID=…; CID=…; SEID=…" /></label>
      <button class="btn btn-accent" :disabled="busy || !config">{{ busy ? '正在验证连接…' : '验证并保存 115 连接' }}</button>
    </form>
    <p v-if="error" class="cloud-error" role="alert">{{ error }} <button v-if="!config" class="btn btn-sm" @click="load">重新加载</button></p>
    <p v-if="message" role="status" class="hint">{{ message }}</p>
    <p class="hint">缓存统一存放在网盘根目录的「缓存-24h自动删除」下。每个任务从创建缓存子目录起计时，24 小时后自动删除该任务缓存，即使未下载完成或已取消；到期播放源立即停用，服务重启后补做清理。请勿将需要永久保留的文件放进任务子目录。</p>
    <p class="hint">仅获取临时直链并 302 跳转，不转码。请同时备份 SQLite 和数据目录内的 .115.key；丢失密钥无法解密 Cookie。</p>
  </section>
</template>
<style scoped>
.cloud-settings { margin-top:24px; }.cloud-form { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }.cloud-form label { display:grid; gap:8px; font-size:13px; }.cloud-form input { width:100%; min-width:0; }.cloud-form button { align-self:end; min-height:44px; }.cloud-error { color:var(--danger); overflow-wrap:anywhere; }@media(max-width:720px) { .cloud-form { grid-template-columns:1fr; } }
</style>
