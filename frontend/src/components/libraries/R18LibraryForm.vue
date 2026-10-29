<script setup lang="ts">
import { reactive, ref, watch } from 'vue';
import { r18Api, type R18Connection } from '../../api/r18';
import type { LibraryDto } from '../../api/contracts';
import { usePageRequest } from '../../composables/usePageRequest';
import ReadState from '../common/ReadState.vue';

const props = defineProps<{ libraryId?: number }>();
const emit = defineEmits<{ saved: [library: LibraryDto]; cancel: [] }>();
const scope = usePageRequest(), loading = ref(false), saving = ref(false), error = ref(''), apiKey = ref(''), hasApiKey = ref(false);
const form = reactive<R18Connection>({ name: 'JavDB 在线资料库', base_url: '' });
async function load() {
  const request = scope.begin(); error.value = ''; apiKey.value = ''; hasApiKey.value = false; loading.value = !!props.libraryId;
  if (!props.libraryId) return;
  try { const result = await r18Api.connection(props.libraryId, request.signal); if (request.isCurrent()) { Object.assign(form, result.connection); hasApiKey.value = result.hasApiKey; } }
  catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '读取连接配置失败'; }
  finally { if (request.isCurrent()) loading.value = false; }
}
watch(() => props.libraryId, load, { immediate: true });
async function save() {
  if (saving.value || loading.value) return;
  if (!form.name.trim()) { error.value = '请填写 JavDB 资料库名称'; return; }
  if (!form.base_url.trim()) { error.value = '请填写 AVdb-Only API 服务地址'; return; }
  if ((!props.libraryId || !hasApiKey.value) && !apiKey.value.trim()) { error.value = '请填写 API Key'; return; }
  saving.value = true; error.value = '';
  try {
    const payload: R18Connection = { ...form, name: form.name.trim(), base_url: form.base_url.trim() };
    if (!props.libraryId || apiKey.value.trim()) payload.api_key = apiKey.value.trim();
    const library = await r18Api.saveConnection(payload, props.libraryId);
    apiKey.value = ''; emit('saved', library);
  } catch (cause) { error.value = cause instanceof Error ? cause.message : '保存资料库失败'; }
  finally { saving.value = false; }
}
</script>

<template>
  <form class="field-grid r18-connection-form" @submit.prevent="save">
    <p class="hint r18-connection-note">通过 AVdb-Only API 浏览 JavDB 在线影片和磁力资源，不导入整库。API Key 仅保存在服务端，不回显到浏览器。</p>
    <ReadState :error="error" :loading="loading" :empty="loading" @retry="libraryId ? load() : save()" />
    <div class="field"><label for="r18-name">JavDB 媒体库名称</label><input id="r18-name" v-model="form.name" maxlength="120" :disabled="loading || saving" required></div>
    <div class="field"><label for="r18-base-url">AVdb-Only API 地址</label><input id="r18-base-url" v-model="form.base_url" type="url" placeholder="输入 AVdb-Only 服务地址" :disabled="loading || saving" required autocomplete="off"></div>
    <div class="field"><label for="r18-api-key">API Key</label><input id="r18-api-key" v-model="apiKey" type="password" :placeholder="hasApiKey ? '留空保留已保存 API Key' : '输入 API Key'" :required="!libraryId || !hasApiKey" :disabled="loading || saving" autocomplete="new-password"></div>
    <div class="r18-connection-actions"><button class="btn btn-accent" :disabled="loading || saving">{{ saving ? '校验连接并保存…' : libraryId ? '保存连接配置' : '连接并添加 JavDB 资料库' }}</button><button v-if="libraryId" class="btn" type="button" :disabled="saving" @click="emit('cancel')">取消</button></div>
  </form>
</template>

<style scoped>
.r18-connection-note, .r18-connection-actions { grid-column:1/-1; }.r18-connection-actions { display:flex; flex-wrap:wrap; gap:10px; }.r18-connection-form :deep(.auth-error) { grid-column:1/-1; }
</style>
