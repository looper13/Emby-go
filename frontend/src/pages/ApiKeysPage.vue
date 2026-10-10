<script setup lang="ts">
import { ref } from 'vue';
import { Plus, Trash2 } from '@lucide/vue';
import { adminApi } from '../api/admin';
import { useAdminRead } from '../composables/useAdminRead';
import { useToastStore } from '../stores/toasts';
import ReadState from '../components/common/ReadState.vue';
import EmptyState from '../components/common/EmptyState.vue';
const { data, loading, error, load, active } = useAdminRead(adminApi.keys); const toasts = useToastStore();
const name = ref(''); const submitting = ref(false); const deleting = ref(new Set<string>());
async function create() {
  if (submitting.value || !name.value.trim()) return;
  submitting.value = true;
  try { await adminApi.createKey(name.value.trim()); toasts.show('密钥已创建'); if (active()) { name.value = ''; await load(); } }
  catch (cause) { toasts.show(cause instanceof Error ? cause.message : '创建失败', 'error'); }
  finally { submitting.value = false; }
}
async function copy(key: string) {
  try { await navigator.clipboard.writeText(key); toasts.show('已复制密钥'); }
  catch { toasts.show('复制失败，请手动选择', 'error'); }
}
async function remove(key: string) {
  if (deleting.value.has(key) || !window.confirm('删除该 API 密钥？使用它的客户端将立即失效。')) return;
  deleting.value.add(key);
  try { await adminApi.deleteKey(key); toasts.show('密钥已删除'); if (active()) await load(); }
  catch (cause) { toasts.show(cause instanceof Error ? cause.message : '删除失败', 'error'); }
  finally { deleting.value.delete(key); }
}
</script>
<template>
  <section class="panel"><div class="panel-head"><h2>API 密钥</h2><span class="hint" style="margin:0">共 {{ data?.total ?? 0 }} 个 · 供脚本/第三方客户端直接调用 Emby API</span></div>
    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />
    <div v-if="data?.items.length" class="table-wrap"><table><thead><tr><th>名称</th><th>密钥</th><th>创建时间</th><th style="text-align:right">操作</th></tr></thead>
      <tbody><tr v-for="item in data.items" :key="item.key"><td><strong class="title">{{ item.name || '—' }}</strong></td><td class="mono" data-secret>{{ item.key }}</td><td class="mono">{{ item.created_at || '—' }}</td>
        <td><div class="row-actions"><button class="btn btn-sm" :data-copy="item.key" @click="copy(item.key)">复制</button><button class="icon-btn danger" :data-key-delete="item.key" title="删除密钥" :disabled="deleting.has(item.key)" @click="remove(item.key)"><Trash2 :size="15" :stroke-width="1.8" aria-hidden="true" /></button></div></td>
      </tr></tbody></table></div>
    <EmptyState v-else-if="data" title="还没有 API 密钥" hint="在下方创建后即可用 X-Emby-Token 调用接口。" />
  </section>
  <section class="panel"><h2>创建密钥</h2><form id="apikey-form" class="field-grid" @submit.prevent="create">
    <div class="field"><label for="key-name">名称</label><input id="key-name" v-model="name" name="name" placeholder="如 yamby / 脚本" required></div>
    <div class="form-foot" style="grid-column:1/-1;margin:2px 0 0"><button class="btn btn-accent" :disabled="submitting"><Plus :size="15" :stroke-width="1.8" aria-hidden="true" /><span>创建</span></button></div>
  </form><p class="hint">调用示例：<code>curl -H "X-Emby-Token: &lt;密钥&gt;" http://host:18080/Users/1/Views</code>，也可用 <code>?api_key=&lt;密钥&gt;</code>。密钥即凭据，请勿外泄。</p></section>
</template>
