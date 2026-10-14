<script setup lang="ts">
import { computed, ref } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';
import { Save, RotateCcw, RefreshCw } from '@lucide/vue';
import { tmdbApi } from '../../api/anime-artwork';
import { useAdminRead } from '../../composables/useAdminRead';
import ReadState from '../common/ReadState.vue';
const { data, error, loading, load, active } = useAdminRead(tmdbApi.config);
const apiKey = ref(''), clear = ref(false), busy = ref(false), message = ref('');
const dirty = computed(() => apiKey.value !== '' || clear.value);
onBeforeRouteLeave(() => !dirty.value || window.confirm('TMDB 设置尚未保存，确定离开？'));
function restore() { apiKey.value = ''; clear.value = false; message.value = ''; }
async function test() {
  if (busy.value) return;
  busy.value=true;message.value='';
  try { await tmdbApi.test(); if(active()) message.value='TMDB 连接成功'; }
  catch(e) { if(active()) message.value=e instanceof Error?e.message:'连接失败'; }
  finally { busy.value=false; }
}
async function save() {
  if (!data.value || busy.value) return;
  busy.value = true; message.value = '';
  try { const value = await tmdbApi.saveConfig(apiKey.value, clear.value, data.value.revision); if (active()) { data.value = value; restore(); message.value = 'TMDB 设置已保存'; } }
  catch (e) { message.value = e instanceof Error ? e.message : '保存失败'; }
  finally { busy.value = false; }
}
</script>
<template>
  <section class="panel">
    <h2>TMDB 剧照 <small v-if="dirty">· 未保存</small></h2>
    <ReadState :loading="loading" :error="error" :empty="!data" @retry="load" />
    <form v-if="data" class="field-grid" @submit.prevent="save">
      <div class="field full"><label for="tmdb-api-key">API Key · {{ data.configured ? '已配置' : '未配置' }}</label><input id="tmdb-api-key" v-model="apiKey" type="password" autocomplete="new-password" maxlength="256" :disabled="busy || clear" placeholder="保持空白则不更改" /></div>
      <label v-if="data.configured" class="check"><input v-model="clear" type="checkbox" :disabled="busy" @change="apiKey=''" />清除已保存 API Key</label>
      <div class="form-foot"><button type="button" class="btn" :disabled="busy || !data.configured" @click="test"><RefreshCw :size="16" />测试已保存连接</button><button type="button" class="btn" :disabled="busy || !dirty" @click="restore"><RotateCcw :size="16" />恢复</button><button class="btn btn-accent" :disabled="busy || !dirty"><Save :size="16" />保存 TMDB 设置</button></div>
      <p v-if="message" role="status">{{ message }}</p>
    </form>
    <p class="tmdb-credit">This product uses the TMDB API but is not endorsed or certified by TMDB.</p>
    <a href="https://www.themoviedb.org" target="_blank" rel="noopener noreferrer">The Movie Database</a>
  </section>
</template>
<style scoped>
.btn{display:inline-flex;align-items:center;gap:6px}.form-foot{grid-column:1/-1}.tmdb-credit{font-size:12px;line-height:1.6}
</style>
