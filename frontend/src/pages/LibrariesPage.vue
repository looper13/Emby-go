<script setup lang="ts">
import { ref, watch } from 'vue';
import { RefreshCw, Trash2 } from '@lucide/vue';
import { adminApi } from '../api/admin';
import { useAdminRead } from '../composables/useAdminRead';
import { useTasksStore } from '../stores/tasks';
import { useToastStore } from '../stores/toasts';
import LibraryForm from '../components/libraries/LibraryForm.vue';
import EmptyState from '../components/common/EmptyState.vue';
import ReadState from '../components/common/ReadState.vue';
const { data, loading, error, load, active } = useAdminRead(adminApi.libraries);
const tasks = useTasksStore(); const toasts = useToastStore();
const submitting = ref(false); const formKey = ref(0); const deleting = ref(new Set<number>());
watch(() => tasks.revision, () => { void load(); });
async function create(payload: { Name: string; Path: string }) {
  if (submitting.value || !payload.Name.trim() || !payload.Path.trim()) return;
  submitting.value = true;
  try {
    const lib = await adminApi.createLibrary(payload); toasts.show(`已添加媒体库「${lib.Name}」`);
    if (active()) { formKey.value += 1; await load(); }
  } catch (cause) { toasts.show(cause instanceof Error ? cause.message : '添加失败', 'error'); }
  finally { submitting.value = false; }
}
async function remove(id: number) {
  if (deleting.value.has(id) || !window.confirm('删除该媒体库及其影片索引？不会删除磁盘文件，但该库影片的播放进度/收藏会一并清除。')) return;
  deleting.value.add(id);
  try { await adminApi.deleteLibrary(id); toasts.show('媒体库已删除'); if (active()) await load(); }
  catch (cause) { toasts.show(cause instanceof Error ? cause.message : '删除失败', 'error'); }
  finally { deleting.value.delete(id); }
}
</script>
<template>
  <section class="panel">
    <div class="panel-head"><h2>媒体库</h2><span class="hint" style="margin:0">共 {{ data?.total ?? 0 }} 个 · 扫描/浏览均以库为单位</span></div>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <div v-if="data?.items?.length" class="table-wrap"><table>
      <thead><tr><th>名称</th><th>路径</th><th class="lib-id">ID</th><th style="text-align:right">操作</th></tr></thead>
      <tbody><tr v-for="lib in data.items" :key="lib.Id">
        <td><strong class="title">{{ lib.Name }}</strong></td><td class="mono"><span class="lib-path">{{ lib.Path }}</span></td><td class="num lib-id">{{ lib.Id }}</td>
        <td><div class="row-actions"><button class="btn btn-sm" :data-lib-scan="lib.Id" title="仅更新该媒体库中新增或变化的影片" :disabled="tasks.mutationBusy" @click="tasks.run('scan', lib.Id)"><RefreshCw :size="15" :stroke-width="1.8" aria-hidden="true" /><span>增量扫描</span></button>
          <button class="icon-btn danger" :data-lib-delete="lib.Id" title="删除媒体库索引（不删文件）" :disabled="deleting.has(lib.Id)" @click="remove(lib.Id)"><Trash2 :size="15" :stroke-width="1.8" aria-hidden="true" /></button></div></td>
      </tr></tbody></table></div>
    <EmptyState v-else-if="data" title="还没有媒体库" hint="先在下方登记一个存放 .strm 的目录。" />
  </section>
  <section class="panel"><h2>添加媒体库</h2><LibraryForm :key="formKey" :submitting="submitting" @submit="create" /></section>
</template>
