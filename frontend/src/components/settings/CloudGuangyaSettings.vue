<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { guangyaApi } from '../../api/guangya';
import type { CloudConfig } from '../../api/cloud115';
import { usePageRequest } from '../../composables/usePageRequest';

const scope = usePageRequest(), config = ref<CloudConfig | null>(null);
const accessToken = ref(''), refreshToken = ref(''), deviceId = ref('');
const busy = ref(false), error = ref(''), message = ref('');
async function load() {
  const request = scope.begin(); error.value = '';
  try { const next = await guangyaApi.config(request.signal); if (request.isCurrent()) config.value = next; }
  catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '光鸭配置加载失败'; }
}
async function save() {
  if (busy.value || !config.value) return;
  const request = scope.begin(); busy.value = true; error.value = ''; message.value = '';
  try {
    await guangyaApi.save({ access_token: accessToken.value.trim(), refresh_token: refreshToken.value.trim(), device_id: deviceId.value.trim(), revision: config.value.revision }, request.signal);
    if (!request.isCurrent()) return;
    accessToken.value = ''; refreshToken.value = ''; deviceId.value = '';
    message.value = '光鸭连接已验证并保存，专属缓存目录已准备。可在 JavDB 影片详情选择光鸭网盘播放。';
    const next = await guangyaApi.config(request.signal);
    if (request.isCurrent()) config.value = next;
  } catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '光鸭配置保存失败'; }
  finally { if (request.isCurrent()) busy.value = false; }
}
onMounted(load);
</script>
<template>
  <section class="panel cloud-guangya-settings">
    <div class="panel-head"><h2>光鸭网盘连接</h2><span class="hint">JavDB 磁力离线下载与直链播放</span></div>
    <p class="hint">填写光鸭登录会话中的访问令牌（access_token）与刷新令牌（refresh_token）。凭据加密保存在服务端，不回显；刷新令牌用于登录续期。</p>
    <p v-if="config?.configured" class="hint">已配置账号 {{ config.account_id }}。令牌和设备 ID 留空保留既有凭据。</p>
    <form class="cloud-guangya-form" @submit.prevent="save">
      <label for="guangya-access-token">访问令牌<input id="guangya-access-token" v-model="accessToken" type="password" autocomplete="new-password" :required="!config?.configured" maxlength="16384" :disabled="busy" :placeholder="config?.configured ? '留空保留已保存令牌' : 'access_token'" /></label>
      <label for="guangya-refresh-token">刷新令牌<input id="guangya-refresh-token" v-model="refreshToken" type="password" autocomplete="new-password" maxlength="16384" :disabled="busy" placeholder="refresh_token（可选）" /></label>
      <label for="guangya-device-id">设备 ID<input id="guangya-device-id" v-model="deviceId" type="password" autocomplete="new-password" maxlength="256" :disabled="busy" placeholder="与登录会话一致的 device_id（可选）" /></label>
      <button class="btn btn-accent" :disabled="busy || !config">{{ busy ? '正在验证连接…' : '验证并保存光鸭连接' }}</button>
    </form>
    <p class="hint">设备 ID 需与获取令牌时的光鸭登录会话一致；首次配置留空时自动生成设备 ID。无需填写网盘文件夹 ID。</p>
    <p v-if="error" class="cloud-guangya-error" role="alert">{{ error }} <button v-if="!config" class="btn btn-sm" @click="load">重新加载</button></p>
    <p v-if="message" role="status" class="hint">{{ message }}</p>
    <p class="hint">磁力下载使用本服务专属的「缓存-24h自动删除」目录，创建任务缓存 24 小时后自动删除。请勿将需要永久保留的文件放进任务子目录。</p>
    <p class="hint">仅获取临时直链并 302 跳转，不转码。请同时备份 SQLite 和数据目录中的网盘凭证密钥。</p>
  </section>
</template>
<style scoped>
.cloud-guangya-settings { margin-top:24px; }.cloud-guangya-form { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }.cloud-guangya-form label { display:grid; gap:8px; font-size:13px; }.cloud-guangya-form input { width:100%; min-width:0; }.cloud-guangya-form button { align-self:end; min-height:44px; }.cloud-guangya-error { color:var(--danger); overflow-wrap:anywhere; }@media(max-width:720px) { .cloud-guangya-form { grid-template-columns:1fr; } }
</style>
