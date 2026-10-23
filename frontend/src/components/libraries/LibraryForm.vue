<script setup lang="ts">
import { ref } from 'vue';
import { Plus } from '@lucide/vue';
import type { LibraryContentType } from '../../api/contracts';
defineProps<{ submitting: boolean }>();
const emit = defineEmits<{ submit: [payload: { Name: string; Path: string; Type: LibraryContentType; ProxyEnabled: boolean; ResolveRedirects: boolean; }] }>();
const name = ref(''); const path = ref(''); const contentType = ref<LibraryContentType>('movies'); const proxyEnabled = ref(false); const resolveRedirects = ref(false);
</script>
<template>
  <form id="library-form" class="field-grid" @submit.prevent="!submitting && emit('submit', { Name: name, Path: path, Type: contentType, ProxyEnabled: proxyEnabled, ResolveRedirects: resolveRedirects })">
    <div class="field"><label for="lib-type">内容类型</label><select id="lib-type" v-model="contentType" name="Type" :disabled="submitting"><option value="movies">影片</option><option value="tvshows">电视节目</option><option value="mixed">混合内容</option></select></div>
    <div class="field"><label for="lib-name">名称</label><input id="lib-name" v-model="name" name="Name" placeholder="如 电影 / 电视剧" :disabled="submitting" required></div>
    <div class="field"><label for="lib-path">目录路径</label><input id="lib-path" v-model="path" name="Path" placeholder="服务器上的绝对路径" :disabled="submitting" required></div>
    <label class="field checkbox-field"><input id="lib-resolve" v-model="resolveRedirects" type="checkbox" :disabled="submitting"><span>解析直链</span><small>从原始 STRM 地址获取上游 302 直链，客户端直接播放</small></label>
    <label class="field checkbox-field"><input id="lib-proxy" v-model="proxyEnabled" type="checkbox" :disabled="submitting"><span>启用播放代理</span><small>由 Emby-go 转发视频流，适合需要服务端出网或防盗链的源</small></label>
    <div class="form-foot" style="grid-column:1/-1;margin:2px 0 0"><button class="btn btn-accent" :disabled="submitting"><Plus :size="15" :stroke-width="1.8" aria-hidden="true" /><span>添加</span></button></div>
  </form>
</template>
