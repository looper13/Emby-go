<script setup lang="ts">
import { ref } from 'vue';
import { adminApi, type ManualPayload } from '../api/admin';
import { useAdminRead } from '../composables/useAdminRead';
import { useToastStore } from '../stores/toasts';
import ManualForm from '../components/manual/ManualForm.vue';
import ReadState from '../components/common/ReadState.vue';
const { data, error, loading, load, active } = useAdminRead(adminApi.libraries);
const submitting = ref(false); const formKey = ref(0); const toasts = useToastStore();
async function submit(payload: ManualPayload) {
  if (submitting.value) return; submitting.value = true;
  try { await adminApi.manual(payload); toasts.show(`「${payload.title}」已入库`); if (active()) formKey.value += 1; }
  catch (cause) { toasts.show(cause instanceof Error ? cause.message : '补录失败', 'error'); }
  finally { submitting.value = false; }
}
</script>
<template>
  <section class="panel"><div class="panel-head"><h2>手动补录</h2><span class="hint" style="margin:0">写入 .strm + 生成 NFO，即时进入 Emby</span></div>
    <p>为一条 <code>http(s)</code> 直链登记影片：系统会在此服务器上写入源文件、同目录 NFO，并将影片元数据入库。源地址必须为 http/https。</p>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <ManualForm :key="formKey" :submitting="submitting" :library-hint="data?.items?.[0]?.Name ?? ''" @submit="submit" />
  </section>
</template>
