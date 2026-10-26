<script setup lang="ts">
import { watch } from 'vue';
import { Film } from '@lucide/vue';
import { adminApi } from '../api/admin';
import { useAdminRead } from '../composables/useAdminRead';
import { useTasksStore } from '../stores/tasks';
import { fmtTime, taskTypes, runStatuses } from '../lib/format';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
import TaskError from '../components/tasks/TaskError.vue';
const { data, error, loading, load } = useAdminRead(adminApi.tasks); const tasks = useTasksStore();
watch(() => tasks.revision, () => { void load(); });
</script>
<template>
  <section class="panel"><div class="panel-head"><h2>任务日志</h2><div class="panel-actions">
    <button id="task-refresh" class="btn btn-sm" :disabled="loading" @click="load">刷新</button><button id="task-scan" class="btn btn-accent btn-sm" :disabled="tasks.mutationBusy" @click="tasks.run()"><Film :size="15" :stroke-width="1.8" aria-hidden="true" /><span>立即扫描</span></button>
  </div></div><ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <p v-if="data?.running || tasks.busy" class="hint" style="color:var(--accent)">有任务正在进行…</p>
    <div v-if="data?.items.length" class="table-wrap"><table><thead><tr><th>类型</th><th>状态</th><th>开始时间</th><th>结束时间</th><th>错误</th></tr></thead>
      <tbody><tr v-for="item in data.items" :key="item.id"><td data-label="类型">{{ taskTypes[item.type] || item.type }}</td><td data-label="状态"><span class="badge" :class="item.status">{{ runStatuses[item.status] || item.status }}</span></td><td class="mono" data-label="开始时间">{{ fmtTime(item.started_at) }}</td><td class="mono" data-label="结束时间">{{ fmtTime(item.ended_at) }}</td><td class="mono" data-label="错误"><TaskError :error="item.error" /></td></tr></tbody>
    </table></div><EmptyState v-else-if="data" title="暂无任务" hint="点击「立即扫描」开始索引。" />
  </section>
</template>

<style scoped>
tbody td { vertical-align: top; }
@media (max-width: 720px) {
  .table-wrap { margin: 0; overflow: visible; }
  table, tbody, tr { display: block; width: 100%; }
  thead { display: none; }
  tr { margin-top: 12px; border: 1px solid var(--line); border-radius: 8px; overflow: hidden; }
  tbody td { display: grid; grid-template-columns: 76px minmax(0, 1fr); gap: 10px; align-items: start; padding: 9px 12px; min-width: 0; }
  tbody td::before { content: attr(data-label); color: var(--dim); font: 600 11px var(--font-mono); }
  tbody td:last-child { display: block; border-bottom: 0; }
  tbody td:last-child::before { display: block; margin-bottom: 6px; }
}
</style>
