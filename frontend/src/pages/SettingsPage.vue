<script setup lang="ts">
import { computed } from 'vue';
import { adminApi } from '../api/admin';
import { useAdminRead } from '../composables/useAdminRead';
import ReadState from '../components/common/ReadState.vue';
const { data, loading, error, load } = useAdminRead(adminApi.settings);
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
  <section class="panel"><div class="panel-head"><h2>服务设置</h2></div>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <div v-if="data" class="table-wrap"><table><tbody><tr v-for="[label, value] in fields" :key="label"><th style="width:160px">{{ label }}</th><td class="mono" :data-private-path="label === '数据库' ? '' : undefined">{{ value }}</td></tr></tbody></table></div>
    <p class="hint">配置修改后需重启服务生效。浏览海报墙请通过 Emby 客户端连接本服务（System/Info/Public 的 ServerId 用于标识实例）。</p>
  </section>
</template>
