<script setup lang="ts">
import { computed, onMounted, ref, shallowRef, watch } from 'vue';
import { Archive, RefreshCw } from '@lucide/vue';
import { catalogApi } from '../api/catalog';
import { isAbortError } from '../api/client';
import { usePageRequest } from '../composables/usePageRequest';
import type { LibrariesDto, StatusDto } from '../api/contracts';
import { useTasksStore } from '../stores/tasks';

interface Overview { libraries: LibrariesDto; successTotal: number; status: StatusDto }
const data = shallowRef<Overview | null>(null);
const loading = ref(false);
const error = ref('');
const requests = usePageRequest();
const tasks = useTasksStore();
watch(() => tasks.revision, () => { void load(); });
const labels = { success: '已入库', manual: '手动录入', pending: '待补录', incompatible: '不兼容' };
const statusKeys: (keyof StatusDto)[] = ['success', 'manual', 'pending', 'incompatible'];
const rows = computed(() => statusKeys.flatMap((key) => {
  const count = data.value?.status[key] ?? 0;
  return count > 0 ? [{ key, label: labels[key], count }] : [];
}));
const cards = computed(() => data.value ? [
  { label: '媒体库', value: data.value.libraries.total, sub: 'Collection Folder' },
  { label: '已入库影片', value: data.value.successTotal, sub: 'NFO 真源 · 可见于 Emby' },
  { label: '待补录', value: data.value.status.pending, sub: '缺少 NFO 元数据' },
  { label: '不兼容源', value: data.value.status.incompatible, sub: '旧索引状态，扫描后重新归类' },
] : []);

async function load() {
  const request = requests.begin();
  loading.value = true;
  error.value = '';
  try {
    const [libraries, successTotal, status] = await Promise.all([
      catalogApi.libraries(request.signal), catalogApi.successTotal(request.signal), catalogApi.status(request.signal),
    ]);
    if (request.isCurrent()) data.value = { libraries, successTotal, status };
  } catch (cause) {
    if (request.isCurrent() && !isAbortError(cause)) {
      error.value = cause instanceof Error ? cause.message : '无法连接服务';
    }
  } finally {
    if (request.isCurrent()) loading.value = false;
  }
}

onMounted(() => { void load(); });
</script>

<template>
  <div v-if="data" class="cards">
    <div v-for="card in cards" :key="card.label" class="card">
      <small>{{ card.label }}</small><strong>{{ card.value }}</strong><span>{{ card.sub }}</span>
    </div>
  </div>
  <section class="panel" :aria-busy="loading">
    <div class="panel-head">
      <h2>档案状态</h2>
      <div class="panel-actions">
        <button id="reindex" class="btn" title="忽略变化记录，重新读取所有影片" :disabled="tasks.mutationBusy" @click="tasks.run('reindex')"><RefreshCw :size="15" :stroke-width="1.8" aria-hidden="true" /><span>全量重建索引</span></button>
        <button class="icon-btn" title="刷新" aria-label="刷新" :disabled="loading" @click="load">
          <RefreshCw :size="16" :stroke-width="1.8" aria-hidden="true" />
        </button>
      </div>
    </div>
    <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
    <p v-if="loading && !data" role="status">加载中…</p>
    <template v-if="data">
      <div v-if="rows.length" class="table-wrap">
        <table>
          <thead><tr><th>状态</th><th style="width:80px">数量</th></tr></thead>
          <tbody>
            <tr v-for="row in rows" :key="row.key">
              <td><span class="badge" :class="row.key">{{ row.label }}</span></td><td class="num">{{ row.count }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">
        <Archive :size="40" :stroke-width="1.8" aria-hidden="true" /><p>档案为空</p><small>添加媒体库并开始扫描。</small>
      </div>
      <p class="hint">NFO 决定影片入库；播放时读取当前 .strm，仅支持 http/https 地址。</p>
    </template>
  </section>
</template>
