<script setup lang="ts">
import { onMounted, onScopeDispose, ref } from 'vue';
import { ApiError } from '../../api/client';
import { duplicatesApi, type DuplicateMovie, type DuplicateDeletePreview } from '../../api/duplicates';
import { usePageRequest } from '../../composables/usePageRequest';

const props = defineProps<{ movie:DuplicateMovie; libraryId:number; groupKey:string }>();
const emit = defineEmits<{close:[]; deleted:[]}>();
const preview = ref<DuplicateDeletePreview|null>(null), loading = ref(false), deleting = ref(false), error = ref(''), confirmed = ref(false), removed = ref<string[]>([]);
const scope = usePageRequest(), shell = ref<HTMLDialogElement>(), closeButton = ref<HTMLButtonElement>();
let active = true;
async function load() {
  const current = scope.begin(); loading.value = true; preview.value = null; error.value = ''; confirmed.value = false;
  try { const result = await duplicatesApi.previewDelete(props.movie.id,props.libraryId,props.groupKey,current.signal); if(current.isCurrent()) preview.value = result; }
  catch(cause) { if(current.isCurrent()) error.value = (cause instanceof Error || cause instanceof DOMException) && cause.name === 'TimeoutError' ? '核验请求超时，请检查当前影片的 STRM 与 AList 连接后重新预览。' : cause instanceof Error || cause instanceof DOMException ? cause.message : '无法定位文件'; }
  finally { if(current.isCurrent()) loading.value = false; }
}
async function remove() {
  if(!preview.value || !confirmed.value || deleting.value) return;
  deleting.value = true; error.value = ''; removed.value = [];
  try { await duplicatesApi.deleteCopy(props.movie.id,props.libraryId,props.groupKey,preview.value); if(active) emit('deleted'); }
  catch(cause) {
    if(active) {
      error.value = cause instanceof Error ? cause.message : '删除失败';
      if(cause instanceof ApiError && cause.payload && typeof cause.payload === 'object' && 'removed_remote_paths' in cause.payload && Array.isArray(cause.payload.removed_remote_paths)) removed.value = cause.payload.removed_remote_paths.filter((v):v is string=>typeof v === 'string');
      preview.value = null; confirmed.value = false;
    }
  } finally { if(active) deleting.value = false; }
}
function close() { if(!deleting.value) emit('close'); }
onMounted(()=>{if(shell.value?.showModal) shell.value.showModal();else shell.value?.setAttribute('open','');closeButton.value?.focus();void load();});
onScopeDispose(()=>{active=false;shell.value?.close?.();});
</script>

<template>
  <dialog ref="shell" class="duplicate-delete-dialog" aria-labelledby="duplicate-delete-title" @cancel.prevent="close">
    <div class="duplicate-delete-head"><h3 id="duplicate-delete-title">删除此副本</h3><button ref="closeButton" class="btn btn-sm" :disabled="deleting" @click="close">取消</button></div>
    <p><strong>{{ movie.title || movie.number }}</strong> · {{ movie.library_name }}</p>
    <p class="hint">将删除该副本的远程原文件、本地 STRM（含 CD 分段）和影片索引。请确认保留的副本可以播放。NFO、封面和其他目录文件保留。</p>
    <p v-if="loading" role="status">正在读取当前副本的 STRM，并按路径查询 AList 文件信息…（最多等待约 60 秒）</p>
    <p v-if="error" class="auth-error" role="alert">{{ error }} <RouterLink to="/settings#alist-config">AList 设置</RouterLink> <button class="btn btn-sm" :disabled="loading || deleting" @click="load">重新预览</button></p>
    <p v-for="file in removed" :key="file" class="duplicate-path">远程已删除：{{ file }}</p>
    <template v-if="preview">
      <p class="hint">AList：{{ preview.alist_url }}</p>
      <div class="duplicate-delete-files">
        <div v-for="file in preview.files" :key="file.local_path">
          <p class="mono duplicate-path" data-private-path>本地 STRM：{{ file.local_path }}</p>
          <p class="mono duplicate-path" data-private-path>远程文件：{{ file.remote_path }}</p>
          <p class="hint">{{ file.removed ? '远程已删除，本次继续清理' : `${(file.remote.size / 1024 / 1024).toFixed(1)} MiB` }}</p>
        </div>
      </div>
      <label class="check duplicate-delete-check"><input id="duplicate-delete-confirmed" v-model="confirmed" type="checkbox" :disabled="deleting">我已核对路径，确认删除远程原文件和本地 STRM</label>
      <button id="duplicate-delete-confirm" class="btn danger" :disabled="!confirmed || deleting" @click="remove">{{ deleting ? '正在删除，请等待…' : '确认删除远程文件与 STRM' }}</button>
    </template>
  </dialog>
</template>
