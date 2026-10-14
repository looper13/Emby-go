<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue';
import { Download, Square } from '@lucide/vue';
import { artworkApi } from '../../api/anime-artwork';
import { useAdminRead } from '../../composables/useAdminRead';
const props = defineProps<{ subjects: number[]; people?: boolean }>();
const emit = defineEmits<{ updated: [] }>();
const { data, load, error } = useAdminRead(artworkApi.progress);
const busy = ref(false), message = ref('');
const running = computed(() => data.value?.job?.running === true);
let timer: ReturnType<typeof setInterval> | undefined;
watch(running, (value, previous) => {
  clearInterval(timer);
  if (value) timer = setInterval(() => void load(), 1500);
  else if (previous) emit('updated');
});
onScopeDispose(() => clearInterval(timer));
async function run(cancel = false) {
  if (busy.value) return;
  busy.value = true; message.value = '';
  try { if (cancel) await artworkApi.cancel(); else await artworkApi.start(props.subjects, props.people === true); await load(); }
  catch (e) { message.value = e instanceof Error ? e.message : '图片任务失败'; }
  finally { busy.value = false; }
}
</script>
<template>
  <div class="artwork-job" aria-live="polite">
    <button v-if="!running" class="btn" :disabled="busy || !subjects.length || !data?.available" @click="run()"><Download :size="16" />{{ people ? '归档本作封面与头像' : '归档本页封面' }}</button>
    <button v-else class="btn" :disabled="busy" @click="run(true)"><Square :size="16" />取消图片任务</button>
    <span v-if="data?.job">{{ data.job.completed }}/{{ data.job.total }} · 缺图 {{ data.job.missing }} · 失败 {{ data.job.failed }}<span v-if="data.job.status==='cancelled'"> · 已取消</span></span>
    <p v-if="message || error" role="alert">{{ message || error }}</p>
  </div>
</template>
<style scoped>
.artwork-job{display:flex;align-items:center;flex-wrap:wrap;gap:12px;margin:12px 0}.btn{display:inline-flex;align-items:center;gap:6px}.artwork-job span{font-size:13px}.artwork-job p{flex-basis:100%;margin:0}
</style>
