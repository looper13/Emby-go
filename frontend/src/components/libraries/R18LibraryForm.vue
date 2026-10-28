<script setup lang="ts">
import { reactive, ref, watch } from 'vue';
import { r18Api, type R18Connection } from '../../api/r18';
import type { LibraryDto } from '../../api/contracts';
import { usePageRequest } from '../../composables/usePageRequest';
import ReadState from '../common/ReadState.vue';

const props = defineProps<{ libraryId?: number }>();
const emit = defineEmits<{ saved: [library: LibraryDto]; cancel: [] }>();
const scope = usePageRequest(), loading = ref(false), saving = ref(false), error = ref(''), password = ref('');
const form = reactive<R18Connection>({ name: 'R18 资料库', host: '', port: 5432, database: '', username: '', ssl_mode: 'prefer' });
async function load() {
  const request = scope.begin(); error.value = ''; password.value = ''; loading.value = !!props.libraryId;
  if (!props.libraryId) return;
  try { const result = await r18Api.connection(props.libraryId, request.signal); if (request.isCurrent()) Object.assign(form, result.connection); }
  catch (cause) { if (request.isCurrent()) error.value = cause instanceof Error ? cause.message : '读取连接配置失败'; }
  finally { if (request.isCurrent()) loading.value = false; }
}
watch(() => props.libraryId, load, { immediate: true });
async function save() {
  if (saving.value || loading.value) return;
  saving.value = true; error.value = '';
  try {
    const payload: R18Connection = { ...form };
    if (!props.libraryId || password.value) payload.password = password.value;
    const library = await r18Api.saveConnection(payload, props.libraryId);
    password.value = ''; emit('saved', library);
  } catch (cause) { error.value = cause instanceof Error ? cause.message : '保存资料库失败'; }
  finally { saving.value = false; }
}
</script>

<template>
  <form class="field-grid r18-connection-form" @submit.prevent="save">
    <p class="hint r18-connection-note">仅只读查询 public.r18_video 和 public.article。连接信息保存在本机数据库，密码不回显；建议使用专用只读账号。</p>
    <ReadState :error="error" :loading="loading" :empty="loading" @retry="libraryId ? load() : save()" />
    <div class="field"><label for="r18-name">R18 媒体库名称</label><input id="r18-name" v-model="form.name" maxlength="120" :disabled="loading || saving" required></div>
    <div class="field"><label for="r18-host">PostgreSQL 主机</label><input id="r18-host" v-model="form.host" placeholder="数据库 IP 或域名" :disabled="loading || saving" required autocomplete="off"></div>
    <div class="field"><label for="r18-port">PostgreSQL 端口</label><input id="r18-port" v-model.number="form.port" type="number" min="1" max="65535" :disabled="loading || saving" required></div>
    <div class="field"><label for="r18-database">PostgreSQL 数据库</label><input id="r18-database" v-model="form.database" :disabled="loading || saving" required autocomplete="off"></div>
    <div class="field"><label for="r18-username">PostgreSQL 用户名</label><input id="r18-username" v-model="form.username" :disabled="loading || saving" required autocomplete="off"></div>
    <div class="field"><label for="r18-password">PostgreSQL 密码</label><input id="r18-password" v-model="password" type="password" :placeholder="libraryId ? '留空保留已保存密码' : '数据库密码'" :disabled="loading || saving" autocomplete="new-password"></div>
    <div class="field"><label for="r18-ssl">PostgreSQL SSL</label><select id="r18-ssl" v-model="form.ssl_mode" :disabled="loading || saving"><option value="prefer">优先 SSL</option><option value="require">要求 SSL</option><option value="verify-full">校验证书与主机</option><option value="disable">关闭 SSL（可信内网）</option></select></div>
    <div class="r18-connection-actions"><button class="btn btn-accent" :disabled="loading || saving">{{ saving ? '校验连接并保存…' : libraryId ? '保存连接配置' : '连接并添加 R18 资料库' }}</button><button v-if="libraryId" class="btn" type="button" :disabled="saving" @click="emit('cancel')">取消</button></div>
  </form>
</template>

<style scoped>
.r18-connection-note, .r18-connection-actions { grid-column:1/-1; }.r18-connection-actions { display:flex; flex-wrap:wrap; gap:10px; }.r18-connection-form :deep(.auth-error) { grid-column:1/-1; }
</style>
