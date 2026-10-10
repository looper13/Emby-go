<script setup lang="ts">
import { watch } from 'vue';
import { Film } from '@lucide/vue';
import { adminApi } from '../api/admin';
import { useAdminRead } from '../composables/useAdminRead';
import { useTasksStore } from '../stores/tasks';
import { fmtTime, taskTypes, runStatuses } from '../lib/format';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
const { data, error, loading, load } = useAdminRead(adminApi.tasks); const tasks = useTasksStore();
watch(() => tasks.revision, () => { void load(); });
</script>
<template>
  <section class="panel"><div class="panel-head"><h2>任务日志</h2><div class="panel-actions">
    <button id="task-refresh" class="btn btn-sm" :disabled="loading" @click="load">刷新</button><button id="task-scan" class="btn btn-accent btn-sm" :disabled="tasks.mutationBusy" @click="tasks.run()"><Film :size="15" :stroke-width="1.8" aria-hidden="true" /><span>立即扫描</span></button>
  </div></div><ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <p v-if="data?.running || tasks.busy" class="hint" style="color:var(--accent)">有任务正在进行…</p>
    <div v-if="data?.items.length" class="table-wrap"><table><thead><tr><th>类型</th><th>状态</th><th>开始时间</th><th>结束时间</th><th>错误</th></tr></thead>
      <tbody><tr v-for="item in data.items" :key="item.id"><td>{{ taskTypes[item.type] || item.type }}</td><td><span class="badge" :class="item.status">{{ runStatuses[item.status] || item.status }}</span></td><td class="mono">{{ fmtTime(item.started_at) }}</td><td class="mono">{{ fmtTime(item.ended_at) }}</td><td class="mono">{{ item.error || '—' }}</td></tr></tbody>
    </table></div><EmptyState v-else-if="data" title="暂无任务" hint="点击「立即扫描」开始索引。" />
  </section>
</template>
