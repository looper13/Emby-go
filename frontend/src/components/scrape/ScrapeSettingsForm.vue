<script setup lang="ts">
import { computed, ref, watch } from 'vue'; import type { ScrapeSettings, SourceTest } from '../../api/scrape';
const props = defineProps<{ settings: ScrapeSettings; saving: boolean; testing: Set<string> }>(); const emit = defineEmits<{ save: [value: MovieScrapeSettings]; test: [target: SourceTest]; dirty: [value: boolean] }>();
type MovieScrapeSettings = Pick<ScrapeSettings, 'metatube_url' | 'metatube_token' | 'timeout_seconds' | 'concurrency' | 'image_quality' | 'download_images' | 'download_stills' | 'download_trailer' | 'overwrite' | 'translate'>;
const cloneSaved = (): MovieScrapeSettings => ({metatube_url:props.settings.metatube_url,metatube_token:'',timeout_seconds:props.settings.timeout_seconds,concurrency:props.settings.concurrency,image_quality:props.settings.image_quality,download_images:props.settings.download_images,download_stills:props.settings.download_stills,download_trailer:props.settings.download_trailer,overwrite:props.settings.overwrite,translate:{...props.settings.translate,api_key:''}});
const draft = ref<MovieScrapeSettings>(cloneSaved()), baseline = ref('');
function restore() { draft.value = cloneSaved(); baseline.value = JSON.stringify(draft.value); }
watch(() => props.settings, restore, { immediate: true });
const dirty = computed(() => JSON.stringify(draft.value) !== baseline.value);
watch(dirty, value => emit('dirty', value), {immediate:true});
</script>
<template><section class="panel"><div class="panel-head"><h2>刮削配置</h2><div class="panel-actions"><button v-for="target in (['metatube','translate'] as const)" :id="'test-' + target" :key="target" class="btn btn-sm" :disabled="testing.has(target) || saving" @click="emit('test', target)">测试 {{ target === 'metatube' ? 'MetaTube' : '翻译' }}</button></div></div><form id="scrape-form" class="field-grid" @submit.prevent="emit('save', {...draft, translate: {...draft.translate}})">
  <div class="field full"><label for="sc-url">MetaTube 地址</label><input id="sc-url" v-model="draft.metatube_url" class="mono" placeholder="https://metatube.example.com"></div>
  <div class="field full"><label for="sc-token">MetaTube Token</label><input id="sc-token" v-model="draft.metatube_token" class="mono" type="password" autocomplete="new-password" :placeholder="settings.metatube_token ? '已配置（留空表示不修改）' : '可留空（后端未启用鉴权时）'" data-secret></div>
  <div class="field"><label for="sc-timeout">请求超时（秒）</label><input id="sc-timeout" v-model.number="draft.timeout_seconds" type="number" min="1" max="600" required></div>
  <div class="field"><label for="sc-concurrency">并发数</label><input id="sc-concurrency" v-model.number="draft.concurrency" type="number" min="1" max="8" required></div>
  <div class="field"><label for="sc-quality">图片质量</label><input id="sc-quality" v-model.number="draft.image_quality" type="number" min="1" max="100" required></div>
  <div class="field"><label>图片下载</label><label class="check"><input id="sc-images" v-model="draft.download_images" type="checkbox"> 下载并写入海报/背景图</label></div>
  <div class="field"><label>剧照下载</label><label class="check"><input id="sc-stills" v-model="draft.download_stills" type="checkbox"> 下载全部剧照到本地</label></div>
  <div class="field"><label>预告片下载</label><label class="check"><input id="sc-trailer" v-model="draft.download_trailer" type="checkbox"> 下载 MP4 预告片到本地</label></div>
  <div class="field"><label>默认覆盖策略</label><label class="check"><input id="sc-overwrite" v-model="draft.overwrite" type="checkbox"> 强制覆盖（默认只补缺失）</label></div>
  <p class="hint" style="grid-column:1/-1">下载选项独立生效，适用于单条、批量及定时刮削；按影片名保存，已有文件遵循覆盖策略。预告片优先播放本地副本。仅补图片或预告片时，取消下方“仅处理缺失元数据的影片”。</p>
  <div class="field full"><label style="color:var(--accent)">翻译（内嵌，直连自建 Deepl-Proxy）</label></div>
  <div class="field"><label>翻译标题</label><label class="check"><input id="tr-title" v-model="draft.translate.title" type="checkbox"> 译文写入 Title</label></div>
  <div class="field"><label>翻译简介</label><label class="check"><input id="tr-summary" v-model="draft.translate.summary" type="checkbox"> 译文写入 Plot</label></div>
  <div class="field"><label for="tr-lang">目标语言</label><input id="tr-lang" v-model="draft.translate.target_lang" class="mono" placeholder="ZH / ZH-HANT / JA"></div>
  <div class="field"><label for="tr-timeout">翻译超时（秒）</label><input id="tr-timeout" v-model.number="draft.translate.timeout_seconds" type="number" min="1" max="300" required></div>
  <div class="field full"><label for="tr-url">翻译服务地址</label><input id="tr-url" v-model="draft.translate.api_url" class="mono" placeholder="http://127.0.0.1:8080"></div>
  <div class="field full"><label for="tr-key">翻译网关 Token</label><input id="tr-key" v-model="draft.translate.api_key" class="mono" type="password" autocomplete="new-password" :placeholder="settings.translate.api_key ? '已配置（留空表示不修改）' : 'gateway_token'" data-secret></div>
  <div class="form-foot" style="grid-column:1/-1"><button id="scrape-save" class="btn btn-accent" type="submit" :disabled="saving">{{ saving ? '正在保存…' : '保存并即时生效' }}</button><button id="scrape-restore" type="button" class="btn" :disabled="saving || !dirty" @click="restore">恢复已保存设置</button><span id="scrape-dirty" class="hint" style="margin:0" role="status">{{ dirty ? '有未保存的修改' : '当前设置已保存' }}；密钥留空表示不修改。</span></div>
</form></section></template>
