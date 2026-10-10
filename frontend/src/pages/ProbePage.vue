<script setup lang="ts">
import { ref } from 'vue';
import { adminApi } from '../api/admin';
import { useAdminRead } from '../composables/useAdminRead';
import { useToastStore } from '../stores/toasts';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
const { data, error, loading, load, active } = useAdminRead(adminApi.probes); const toasts = useToastStore(); const clearing = ref(false);
async function clear() {
  if (clearing.value) return; clearing.value = true;
  try { await adminApi.clearProbes(); toasts.show('探针记录已清空'); if (active()) await load(); }
  catch (cause) { toasts.show(cause instanceof Error ? cause.message : '清空失败', 'error'); }
  finally { clearing.value = false; }
}
</script>
<template>
  <section class="panel"><div class="panel-head"><h2>未知接口探针</h2><div class="panel-actions"><button id="clear-probes" class="btn btn-sm" :disabled="clearing" @click="clear">清空记录</button></div></div>
    <p class="hint">记录客户端发来但本服务未注册的 Emby 请求，用于补齐端点。上限 1000 条。</p>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <div v-if="data?.items.length" class="table-wrap"><table><thead><tr><th>方法</th><th>路径</th><th>时间</th></tr></thead><tbody>
      <tr v-for="item in data.items" :key="item.id"><td><span class="protocol">{{ item.method }}</span></td><td class="mono">{{ item.path }}</td><td class="mono">{{ item.created_at }}</td></tr>
    </tbody></table></div><EmptyState v-else-if="data" title="暂无探针记录" hint="当客户端请求了未注册的 Emby 接口后会显示在这里。" />
  </section>
</template>
