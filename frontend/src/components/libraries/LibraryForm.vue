<script setup lang="ts">
import { computed, ref } from 'vue';
import { Plus, Save, X } from '@lucide/vue';
import { libraryDirectoryPaths, type LibraryContentType, type LibraryDto } from '../../api/contracts';
import type { DirectoryLibraryPayload } from '../../api/admin';
import LibraryPlaybackHeaders from './LibraryPlaybackHeaders.vue';

const props = defineProps<{ submitting: boolean; library?: LibraryDto }>();
const emit = defineEmits<{ submit: [payload: DirectoryLibraryPayload]; cancel: [] }>();
const name = ref(props.library?.Name ?? '');
const paths = ref(props.library ? libraryDirectoryPaths(props.library) : ['']);
const contentType = ref<LibraryContentType>(props.library?.Type || 'movies');
const proxyEnabled = ref(props.library?.ProxyEnabled === true);
const resolveRedirects = ref(props.library?.ResolveRedirects === true);
const headers = ref<InstanceType<typeof LibraryPlaybackHeaders>>();
const error = ref('');
const prefix = computed(() => props.library ? 'edit-lib' : 'lib');
function pathId(index: number): string { return index === 0 ? `${prefix.value}-path` : `${prefix.value}-path-${index + 1}`; }
function submit() {
  if (props.submitting) return;
  error.value = '';
  if (!name.value.trim()) { error.value = '请填写媒体库名称'; return; }
  if (!paths.value.length || paths.value.some(path => !path.trim())) { error.value = '请填写每个目录路径，或移除空目录'; return; }
  try {
    emit('submit', { Name: name.value.trim(), Paths: [...new Set(paths.value.map(path => path.trim()))], Type: contentType.value,
      ProxyEnabled: proxyEnabled.value, ResolveRedirects: resolveRedirects.value, ProxyHeaders: headers.value?.getHeaders() ?? {} });
  } catch (cause) { error.value = cause instanceof Error ? cause.message : '请求头格式不正确'; }
}
</script>
<template>
  <form :id="library ? 'library-edit-form' : 'library-form'" class="field-grid" @submit.prevent="submit">
    <p v-if="error" class="auth-error full-width" role="alert">{{ error }}</p>
    <div class="field"><label :for="`${prefix}-type`">内容类型</label><select :id="`${prefix}-type`" v-model="contentType" name="Type" :disabled="submitting || !!library"><option value="movies">影片</option><option value="tvshows">电视节目</option><option value="mixed">混合内容</option></select></div>
    <div class="field"><label :for="`${prefix}-name`">名称</label><input :id="`${prefix}-name`" v-model="name" name="Name" placeholder="如 电影 / 电视剧" :disabled="submitting" required></div>
    <div class="library-path-fields full-width">
      <div v-for="(_, index) in paths" :key="index" class="library-path-row" data-library-path-row>
        <div class="field"><label :for="pathId(index)">目录路径{{ paths.length > 1 ? ` ${index + 1}` : '' }}</label><input :id="pathId(index)" v-model="paths[index]" name="Paths" placeholder="服务器上的绝对路径" :disabled="submitting" required></div>
        <button v-if="paths.length > 1" class="icon-btn" type="button" :disabled="submitting" :aria-label="`移除目录 ${index + 1}`" @click="paths.splice(index, 1)"><X :size="16" aria-hidden="true" /></button>
      </div>
      <button class="btn btn-sm" type="button" :disabled="submitting" data-add-path @click="paths.push('')"><Plus :size="14" aria-hidden="true" /><span>添加目录</span></button>
      <p class="hint">同一媒体库中的所有目录会一起扫描和浏览。</p>
    </div>
    <label class="field checkbox-field"><input :id="`${prefix}-resolve`" v-model="resolveRedirects" type="checkbox" :disabled="submitting"><span>解析直链</span><small>从原始 STRM 地址获取上游 302 直链，客户端直接播放</small></label>
    <label class="field checkbox-field"><input :id="`${prefix}-proxy`" v-model="proxyEnabled" type="checkbox" :disabled="submitting"><span>启用播放代理</span><small>由 Emby-go 转发视频流，适合需要服务端出网或防盗链的源</small></label>
    <LibraryPlaybackHeaders v-show="proxyEnabled" ref="headers" class="full-width" :initial-headers="library?.ProxyHeaders" :disabled="submitting" :id-prefix="prefix" />
    <div class="form-foot full-width"><button class="btn btn-accent" :disabled="submitting"><Save v-if="library" :size="15" aria-hidden="true" /><Plus v-else :size="15" :stroke-width="1.8" aria-hidden="true" /><span>{{ submitting ? '保存中…' : library ? '保存修改' : '添加' }}</span></button><button v-if="library" class="btn" type="button" :disabled="submitting" @click="emit('cancel')">取消</button></div>
  </form>
</template>

<style scoped>
.full-width { grid-column: 1 / -1; }
.library-path-fields { display: grid; gap: 10px; }
.library-path-row { display: flex; align-items: end; gap: 10px; }
.library-path-row .field { flex: 1; min-width: 0; }
.library-path-row .icon-btn { margin-bottom: 4px; flex-shrink: 0; }
.library-path-fields > .btn { justify-self: start; }
.library-path-fields .hint { margin: 0; }
.form-foot { display: flex; flex-wrap: wrap; gap: 10px; margin: 2px 0 0; }
</style>
