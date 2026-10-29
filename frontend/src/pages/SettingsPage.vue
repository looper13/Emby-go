<script setup lang="ts">
import { computed, inject, onMounted, onScopeDispose, ref } from 'vue';
import { onBeforeRouteLeave, routerKey } from 'vue-router';
import { adminApi } from '../api/admin';
import { configLabels, systemConfigApi, type ConfigPatch } from '../api/system-config';
import SystemConfigForm from '../components/settings/SystemConfigForm.vue';
import Cloud115Settings from '../components/settings/Cloud115Settings.vue';
import { useAdminRead } from '../composables/useAdminRead';
import ReadState from '../components/common/ReadState.vue';
const { data, loading, error, load } = useAdminRead(adminApi.settings);
const config = useAdminRead(systemConfigApi.read);
const dirty = ref(false), saving = ref(false), saveError = ref(''), savedMessage = ref('');
async function save(patch: ConfigPatch) {
  if (saving.value || !config.data.value) return;
  saving.value = true; saveError.value = ''; savedMessage.value = '';
  try {
    const next = await systemConfigApi.save(config.data.value.revision, patch);
    if (config.active()) {
      config.data.value = next;
      const live = Object.keys(patch).filter(k => !['port', 'debug', 'db_path', 'actors_dir', 'redis_addr', 'redis_db', 'redis_password', 'clear_redis_password'].includes(k));
      const probeNextTask = live.some(k => k === 'ffprobe_path' || k === 'probe_concurrency');
      savedMessage.value = `配置已保存。${live.length ? `已应用：${live.map(k => configLabels[k] || k).join('、')}。` : ''}${probeNextTask ? '探测路径与并发用于下一次任务。' : ''}${next.restart_fields.length ? '部分配置等待重启，详见上方提示。' : '无需重启。'}`;
      void load();
    }
  } catch (cause) { if (config.active()) saveError.value = cause instanceof Error ? cause.message : '保存失败，请重试'; }
  finally { saving.value = false; }
}
async function reload() {
  if (dirty.value && !window.confirm('重新加载会丢弃未保存的修改，是否继续？')) return;
  saveError.value = ''; savedMessage.value = ''; await config.load();
}
if (inject(routerKey, null)) onBeforeRouteLeave(() => !dirty.value || window.confirm('有未保存的配置修改，确定离开？'));
const preventUnload = (event: BeforeUnloadEvent) => { if (dirty.value) { event.preventDefault(); event.returnValue = ''; } };
onMounted(() => window.addEventListener('beforeunload', preventUnload));
onScopeDispose(() => window.removeEventListener('beforeunload', preventUnload));
const fields = computed(() => {
  const s = data.value; if (!s) return [];
  const stats = Object.entries(s.cache_stats).filter(([kind]) => kind !== 'token' && kind !== 'apikey').map(([, value]) => value);
  const hits = stats.reduce((sum, v) => sum + v.hits, 0); const misses = stats.reduce((sum, v) => sum + v.misses, 0);
  const shared = stats.reduce((sum, v) => sum + v.shared, 0);
  return [
    ['监听地址', s.listen], ['数据库', s.db_path], ['缓存后端', s.cache + (s.redis_addr ? ` · ${s.redis_addr}/${s.redis_db}` : '')],
    ['Redis 在线', (s.redis_online ? '是' : '否') + (s.redis_failures ? ` · 累计失败 ${s.redis_failures} 次（按未命中处理）` : '')],
    ['响应缓存命中率', hits + misses ? `${(hits / (hits + misses) * 100).toFixed(1)}% · 命中 ${hits} / 未命中 ${misses}` : '暂无请求'],
    ['合并重复加载', `${shared} 次（统计自本次启动）`],
    ['媒体库监控', s.disable_library_monitor ? '已关闭' : s.library_monitor_mode === 'polling' ? '兼容模式（每 30 秒检查文件树）' : '实时监听'],
  ];
});
</script>
<template>
  <ReadState :error="config.error.value" :loading="config.loading.value" :empty="!config.data.value" @retry="reload" />
  <SystemConfigForm v-if="config.data.value" :state="config.data.value" :saving="saving || config.loading.value" @save="save" @dirty="dirty = $event" @reload="reload" />
  <p v-if="saveError" class="auth-error" role="alert">{{ saveError }}（草稿已保留）</p>
  <p v-if="savedMessage" class="hint" role="status">{{ savedMessage }}</p>
  <section class="panel"><div class="panel-head"><h2>当前运行状态</h2></div>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <div v-if="data" class="table-wrap"><table><tbody><tr v-for="[label, value] in fields" :key="label"><th style="width:160px">{{ label }}</th><td class="mono" :data-private-path="label === '数据库' ? '' : undefined">{{ value }}</td></tr></tbody></table></div>
    <p class="hint">此处显示当前进程使用的配置。MetaTube、GFriends 与翻译选项请前往 <a href="#/scrape">刮削配置</a>。</p>
  </section>
  <Cloud115Settings />
</template>
