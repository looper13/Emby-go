<script setup lang="ts">
import { ref, watch } from 'vue'; import type { ScrapeSettings } from '../../api/scrape';
const props = defineProps<{ settings: ScrapeSettings; saving: boolean; testing: Set<string> }>(); const emit = defineEmits<{ save: [value: ScrapeSettings]; test: [target: 'metatube' | 'translate'] }>();
const draft = ref<ScrapeSettings>({ ...props.settings, translate: { ...props.settings.translate } });
watch(() => props.settings, value => { draft.value = { ...value, metatube_token: '', translate: { ...value.translate, api_key: '' } }; }, { immediate: true });
</script>
<template><section class="panel"><div class="panel-head"><h2>刮削配置</h2><div class="panel-actions"><button v-for="target in (['metatube','translate'] as const)" :id="'test-' + target" :key="target" class="btn btn-sm" :disabled="testing.has(target) || saving" @click="emit('test', target)">测试 {{ target === 'metatube' ? 'MetaTube' : '翻译' }}</button></div></div><form id="scrape-form" class="field-grid" @submit.prevent="emit('save', {...draft, translate: {...draft.translate}})">
  <div class="field full"><label for="sc-url">MetaTube 地址</label><input id="sc-url" v-model="draft.metatube_url" class="mono" placeholder="https://metatube.example.com"></div>
  <div class="field full"><label for="sc-token">MetaTube Token</label><input id="sc-token" v-model="draft.metatube_token" class="mono" type="password" autocomplete="new-password" :placeholder="settings.metatube_token ? '已配置（留空表示不修改）' : 'Bearer token'" data-secret></div>
  <div class="field"><label for="sc-timeout">请求超时（秒）</label><input id="sc-timeout" v-model.number="draft.timeout_seconds" type="number" min="1" max="600" required></div>
  <div class="field"><label for="sc-concurrency">并发数</label><input id="sc-concurrency" v-model.number="draft.concurrency" type="number" min="1" max="8" required></div>
  <div class="field"><label for="sc-quality">图片质量</label><input id="sc-quality" v-model.number="draft.image_quality" type="number" min="1" max="100" required></div>
  <div class="field full"><label for="sc-avatars">头像目录</label><input id="sc-avatars" v-model="draft.avatars_dir" class="mono" placeholder="留空 = 与数据库同级 avatars/" data-private-path></div>
  <div class="field"><label>图片下载</label><label class="check"><input id="sc-images" v-model="draft.download_images" type="checkbox"> 下载并写入海报/背景图</label></div>
  <div class="field"><label>默认覆盖策略</label><label class="check"><input id="sc-overwrite" v-model="draft.overwrite" type="checkbox"> 强制覆盖（默认只补缺失）</label></div>
  <div class="field full"><label style="color:var(--accent)">翻译（内嵌，直连自建 Deepl-Proxy）</label></div>
  <div class="field"><label>翻译标题</label><label class="check"><input id="tr-title" v-model="draft.translate.title" type="checkbox"> 译文写入 Title</label></div>
  <div class="field"><label>翻译简介</label><label class="check"><input id="tr-summary" v-model="draft.translate.summary" type="checkbox"> 译文写入 Plot</label></div>
  <div class="field"><label for="tr-lang">目标语言</label><input id="tr-lang" v-model="draft.translate.target_lang" class="mono" placeholder="ZH / ZH-HANT / JA"></div>
  <div class="field"><label for="tr-timeout">翻译超时（秒）</label><input id="tr-timeout" v-model.number="draft.translate.timeout_seconds" type="number" min="1" max="300" required></div>
  <div class="field full"><label for="tr-url">翻译服务地址</label><input id="tr-url" v-model="draft.translate.api_url" class="mono" placeholder="http://127.0.0.1:8080"></div>
  <div class="field full"><label for="tr-key">翻译网关 Token</label><input id="tr-key" v-model="draft.translate.api_key" class="mono" type="password" autocomplete="new-password" :placeholder="settings.translate.api_key ? '已配置（留空表示不修改）' : 'gateway_token'" data-secret></div>
  <div class="form-foot" style="grid-column:1/-1"><button id="scrape-save" class="btn btn-accent" type="submit" :disabled="saving">{{ saving ? '正在保存…' : '保存并即时生效' }}</button><span class="hint" style="margin:0">保存后无需重启；密钥留空表示不修改。</span></div>
</form></section></template>
