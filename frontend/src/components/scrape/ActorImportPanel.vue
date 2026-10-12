<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue';
import { scrapeApi, type ActorImportPreview } from '../../api/scrape';
import { useTasksStore } from '../../stores/tasks';
const props = defineProps<{ source: string; disabled: boolean }>();
const tasks = useTasksStore(), preview = ref<ActorImportPreview | null>(null), checking = ref(false), error = ref('');
let controller: AbortController | undefined;
watch(() => [props.source, props.disabled], () => { controller?.abort(); preview.value = null; });
onScopeDispose(() => controller?.abort());
async function check() {
  controller?.abort(); const current = new AbortController(); controller = current; checking.value = true; error.value = ''; preview.value = null;
  try { const value = await scrapeApi.importPreview(current.signal); if (!current.signal.aborted) preview.value = value; }
  catch (cause) { if (!current.signal.aborted) error.value = cause instanceof Error ? cause.message : '检查失败'; }
  finally { if (controller === current) checking.value = false; }
}
async function start() { if (await tasks.scrape.run({}, 'import')) preview.value = null; }
</script>
<template>
  <section class="panel"><div class="panel-head"><h2>旧头像导入</h2></div>
    <p class="hint">按当前演员姓名检查旧目录，复制有效头像到资料目录；原文件与影片 NFO 保留。</p>
    <p class="mono import-path" data-private-path>{{ source }}</p>
    <div class="filters"><button id="actor-import-preview" class="btn" :disabled="disabled || checking || tasks.mutationBusy" @click="check">{{ checking ? '正在检查…' : '检查可导入头像' }}</button><span v-if="disabled" class="hint">请先保存或恢复设置。</span></div>
    <p v-if="error" role="alert">{{ error }}</p>
    <template v-if="preview"><p id="actor-import-summary" role="status">待导入 {{ preview.ready }} · 已存在 {{ preview.existing }} · 冲突 {{ preview.conflicts }} · 损坏 {{ preview.damaged }} · 无旧图 {{ preview.missing }}</p><ul v-if="preview.samples.length"><li v-for="sample in preview.samples" :key="sample">{{ sample }}</li></ul><button id="actor-import-start" class="btn btn-accent" :disabled="disabled || !preview.ready || tasks.mutationBusy" @click="start">开始复制导入</button></template>
  </section>
</template>
<style scoped>.import-path { overflow-wrap:anywhere; } li { overflow-wrap:anywhere; }</style>
