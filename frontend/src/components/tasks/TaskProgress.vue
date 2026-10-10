<script setup lang="ts">
import { computed } from 'vue';
import { useTasksStore } from '../../stores/tasks';
const tasks = useTasksStore();
const walking = computed(() => tasks.progress?.running && tasks.progress.phase === 'walk');
const title = computed(() => {
  const p = tasks.progress;
  if (!p) return tasks.kind === 'reindex' ? '正在重建索引…' : '正在扫描…';
  const lib = p.libraries > 1 ? `${p.library_name || '媒体库'}（${p.library_index}/${p.libraries}）` : p.library_name || '媒体库';
  return p.running ? `正在扫描 ${lib}` : `${p.cancelled ? '扫描已取消' : '扫描完成'} ${lib}`;
});
const details = computed(() => {
  const p = tasks.progress; if (!p) return '';
  const parts = [`新增 ${p.added}`, `更新 ${p.updated}`, `跳过 ${p.skipped}`, `删除 ${p.deleted}`];
  if (p.success) parts.push(`已入库 ${p.success}`); if (p.pending) parts.push(`待补录 ${p.pending}`);
  if (p.incompatible) parts.push(`不兼容 ${p.incompatible}`); if (p.failed) parts.push(`失败 ${p.failed}`);
  if (p.running && p.current) parts.push(`当前 ${p.current.split(/[\\/]/u).filter(Boolean).pop()}`);
  if (p.error && !p.cancelled) parts.push(`错误：${p.error}`); return parts.join(' · ');
});
</script>
<template>
  <div id="scan-progress" class="scan-progress" :hidden="!tasks.visible" aria-live="polite">
    <div class="scan-progress-head"><strong id="scan-progress-title">{{ title }}</strong><span id="scan-progress-count">{{ walking ? (tasks.progress?.total ? `已发现 ${tasks.progress.total}` : '遍历中') : tasks.progress?.total ? `${tasks.progress.done}/${tasks.progress.total}` : tasks.progress?.done ?? '' }}</span></div>
    <div class="scan-progress-bar"><i id="scan-progress-fill" :class="{ 'is-indeterminate': walking || (!tasks.progress?.total && tasks.busy) }" :style="{ width: tasks.progress?.total ? `${Math.min(100, Math.round(tasks.progress.done / tasks.progress.total * 100))}%` : '100%' }"></i></div>
    <small id="scan-progress-detail">{{ details }}{{ tasks.unavailable ? ' · 状态暂不可用，正在重试…' : '' }}</small>
  </div>
</template>
