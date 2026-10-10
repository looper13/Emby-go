<script setup lang="ts">
import { computed } from 'vue';
import type { BackgroundProgress } from '../../api/background';
const props = defineProps<{ kind: 'probe' | 'scrape'; progress: BackgroundProgress | null; visible: boolean; unavailable: boolean; busy: boolean; cancelling: boolean }>();
const emit = defineEmits<{ cancel: [] }>();
const label = computed(() => props.kind === 'probe' ? '探测' : props.progress?.kind === 'scrape_avatars' ? '头像任务' : '刮削');
const title = computed(() => props.progress?.running ? `正在${label.value === '头像任务' ? '补演员头像' : label.value}` : props.busy ? `正在启动${label.value}…` : `${label.value}${props.progress?.cancelled ? '已中止' : props.progress?.aborted ? '已因连续失败中止' : '结束'}`);
const detail = computed(() => { const p = props.progress; return p ? [`成功 ${p.success}`, `跳过 ${p.skipped}`, `失败 ${p.failed}`, p.running && p.current ? `当前 ${p.current}` : '', p.error ? `错误：${p.error}` : ''].filter(Boolean).join(' · ') : ''; });
</script>
<template>
  <div :id="kind + '-progress'" class="probe-progress" :hidden="!visible" aria-live="polite">
    <div class="scan-progress-head"><strong :id="kind + '-progress-title'">{{ title }}</strong><span :id="kind + '-progress-count'">{{ progress?.total ? `${progress.done}/${progress.total}` : progress?.done || '' }}</span><button v-if="busy" :id="kind + '-cancel-global'" class="btn btn-sm" :disabled="cancelling || !progress?.running" @click="emit('cancel')">{{ cancelling ? '正在中止…' : '中止' }}</button></div>
    <div class="scan-progress-bar"><i :id="kind + '-progress-fill'" :class="{ 'is-indeterminate': !progress?.total && busy }" :style="{width: progress?.total ? Math.min(100, Math.round(progress.done / progress.total * 100)) + '%' : '100%'}"></i></div>
    <small :id="kind + '-progress-detail'">{{ detail }}{{ unavailable ? ' · 状态暂不可用，正在重试…' : '' }}</small>
    <details v-if="progress?.failures.length" class="scrape-fail-samples"><summary>失败/待确认样例（{{ progress.failures.length }}）</summary><ul><li v-for="(line, index) in progress.failures" :key="index" class="mono" data-private-path>{{ line }}</li></ul></details>
  </div>
</template>
