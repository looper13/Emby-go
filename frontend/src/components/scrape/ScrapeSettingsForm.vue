<script setup lang="ts">
import { computed, ref, watch } from 'vue'; import type { ScrapeSettings, SourceTest, GFriendsStatus } from '../../api/scrape';
const props = defineProps<{ settings: ScrapeSettings; saving: boolean; testing: Set<string>; indexStatus?: GFriendsStatus }>(); const emit = defineEmits<{ save: [value: ScrapeSettings]; test: [target: SourceTest]; dirty: [value: boolean] }>();
const cloneSaved = (): ScrapeSettings => ({...props.settings,metatube_token:'',gfriends:{...props.settings.gfriends},translate:{...props.settings.translate,api_key:''}});
const draft = ref<ScrapeSettings>(cloneSaved()), baseline = ref('');
function restore() { draft.value = cloneSaved(); baseline.value = JSON.stringify(draft.value); }
watch(() => props.settings, restore, { immediate: true });
const dirty = computed(() => JSON.stringify(draft.value) !== baseline.value);
watch(dirty, value => emit('dirty', value), {immediate:true});
const status = computed(() => props.indexStatus ?? props.settings.gfriends_status);
</script>
<template><section class="panel"><div class="panel-head"><h2>刮削配置</h2><div class="panel-actions"><button v-for="target in (['metatube','translate'] as const)" :id="'test-' + target" :key="target" class="btn btn-sm" :disabled="testing.has(target) || saving" @click="emit('test', target)">测试 {{ target === 'metatube' ? 'MetaTube' : '翻译' }}</button></div></div><form id="scrape-form" class="field-grid" @submit.prevent="emit('save', {...draft, translate: {...draft.translate}})">
  <div class="field full"><label for="sc-url">MetaTube 地址</label><input id="sc-url" v-model="draft.metatube_url" class="mono" placeholder="https://metatube.example.com"></div>
  <div class="field full"><label for="sc-token">MetaTube Token</label><input id="sc-token" v-model="draft.metatube_token" class="mono" type="password" autocomplete="new-password" :placeholder="settings.metatube_token ? '已配置（留空表示不修改）' : 'Bearer token'" data-secret></div>
  <div class="field"><label for="sc-timeout">请求超时（秒）</label><input id="sc-timeout" v-model.number="draft.timeout_seconds" type="number" min="1" max="600" required></div>
  <div class="field"><label for="sc-concurrency">并发数</label><input id="sc-concurrency" v-model.number="draft.concurrency" type="number" min="1" max="8" required></div>
  <div class="field"><label for="sc-quality">图片质量</label><input id="sc-quality" v-model.number="draft.image_quality" type="number" min="1" max="100" required></div>
  <div class="field full"><h3>演员头像与资料</h3><label for="sc-storage">资料目录</label><input id="sc-storage" :value="settings.actor_storage_dir" readonly class="mono" data-private-path><small class="hint">在实例配置 actors_dir 中修改，重启后生效；修改路径不会自动搬迁资料。</small></div>
  <div class="field full"><label for="sc-avatar-source">头像来源</label><select id="sc-avatar-source" v-model="draft.avatar_source"><option value="metatube">MetaTube</option><option value="gfriends">GFriends</option><option value="gfriends_then_metatube">GFriends 优先，MetaTube 回退</option></select><small class="hint">GFriends 无需 MetaTube Token。已有头像优先保留，同名歧义会跳过。</small></div>
  <template v-if="draft.avatar_source !== 'metatube'">
    <div class="field full"><label for="gf-index">GFriends 索引地址</label><input id="gf-index" v-model="draft.gfriends.index_url" type="url" class="mono" required></div>
    <div class="field full"><label for="gf-base">图片根地址</label><input id="gf-base" v-model="draft.gfriends.base_url" type="url" class="mono" required><small class="hint">使用镜像时，同时填写对应索引和图片根地址。</small></div>
    <div class="field"><label for="gf-hours">索引刷新间隔（小时）</label><input id="gf-hours" v-model.number="draft.gfriends.refresh_hours" type="number" min="1" max="720" required></div>
    <div class="field"><label>索引状态</label><span id="gf-status" role="status">{{ status?.version ? `${status.groups} 个来源 · ${status.entries} 条映射${status.stale ? ' · 使用旧缓存' : ''}` : '尚未下载索引' }}</span><small v-if="status?.version" class="hint">最近成功检查：{{ new Date(status.updated_at).toLocaleString() }}</small><small v-if="status?.error" class="hint">{{ status.error }}</small><button id="test-gfriends" type="button" class="btn btn-sm" :disabled="dirty || saving || testing.has('gfriends')" @click="emit('test','gfriends')">{{ testing.has('gfriends') ? '正在检查…' : '测试来源 / 刷新索引' }}</button><small class="hint">使用已保存设置，仅检查索引。</small></div>
  </template>
  <div class="field full"><label for="sc-avatars">旧头像来源目录</label><input id="sc-avatars" v-model="draft.avatars_dir" class="mono" placeholder="留空 = 与数据库同级 avatars/" data-private-path><small class="hint">用于兼容读取和复制导入，新头像统一写入资料目录。</small></div>
  <div class="field"><label>图片下载</label><label class="check"><input id="sc-images" v-model="draft.download_images" type="checkbox"> 下载并写入海报/背景图</label></div>
  <div class="field"><label>默认覆盖策略</label><label class="check"><input id="sc-overwrite" v-model="draft.overwrite" type="checkbox"> 强制覆盖（默认只补缺失）</label></div>
  <div class="field full"><label style="color:var(--accent)">翻译（内嵌，直连自建 Deepl-Proxy）</label></div>
  <div class="field"><label>翻译标题</label><label class="check"><input id="tr-title" v-model="draft.translate.title" type="checkbox"> 译文写入 Title</label></div>
  <div class="field"><label>翻译简介</label><label class="check"><input id="tr-summary" v-model="draft.translate.summary" type="checkbox"> 译文写入 Plot</label></div>
  <div class="field"><label for="tr-lang">目标语言</label><input id="tr-lang" v-model="draft.translate.target_lang" class="mono" placeholder="ZH / ZH-HANT / JA"></div>
  <div class="field"><label for="tr-timeout">翻译超时（秒）</label><input id="tr-timeout" v-model.number="draft.translate.timeout_seconds" type="number" min="1" max="300" required></div>
  <div class="field full"><label for="tr-url">翻译服务地址</label><input id="tr-url" v-model="draft.translate.api_url" class="mono" placeholder="http://127.0.0.1:8080"></div>
  <div class="field full"><label for="tr-key">翻译网关 Token</label><input id="tr-key" v-model="draft.translate.api_key" class="mono" type="password" autocomplete="new-password" :placeholder="settings.translate.api_key ? '已配置（留空表示不修改）' : 'gateway_token'" data-secret></div>
  <div class="form-foot" style="grid-column:1/-1"><button id="scrape-save" class="btn btn-accent" type="submit" :disabled="saving">{{ saving ? '正在保存…' : '保存并即时生效' }}</button><button id="scrape-restore" type="button" class="btn" :disabled="saving || !dirty" @click="restore">恢复已保存设置</button><span id="scrape-dirty" class="hint" style="margin:0" role="status">{{ dirty ? '有未保存的修改' : '当前设置已保存' }}；密钥留空表示不修改。</span></div>
</form></section></template>
