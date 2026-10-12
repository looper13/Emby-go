<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { ScrapeSettings, SourceTest, GFriendsStatus } from '../../api/scrape';
export type ActorSettings = Pick<ScrapeSettings, 'avatar_source' | 'avatars_dir' | 'gfriends'>;
const props = defineProps<{ settings: ScrapeSettings; saving: boolean; testing: Set<string>; indexStatus?: GFriendsStatus }>();
const emit = defineEmits<{ save: [value: ActorSettings]; test: [target: SourceTest]; dirty: [value: boolean] }>();
const cloneSaved = (): ActorSettings => ({avatar_source:props.settings.avatar_source,avatars_dir:props.settings.avatars_dir,gfriends:{...props.settings.gfriends}});
const draft = ref<ActorSettings>(cloneSaved()), baseline = ref('');
function restore() { draft.value=cloneSaved(); baseline.value=JSON.stringify(draft.value); }
watch(() => props.settings, restore, {immediate:true});
const dirty = computed(() => JSON.stringify(draft.value)!==baseline.value);
watch(dirty, value => emit('dirty', value), {immediate:true});
const status = computed(() => props.indexStatus ?? props.settings.gfriends_status);
</script>
<template><section class="panel"><div class="panel-head"><h2>头像来源与存储</h2></div>
<form id="actor-settings-form" class="field-grid" @submit.prevent="emit('save', {...draft,gfriends:{...draft.gfriends}})">
  <div class="field full"><label for="sc-storage">资料目录</label><input id="sc-storage" :value="settings.actor_storage_dir" readonly class="mono" data-private-path><small class="hint">在「设置 → 演员资料目录」中修改，重启后生效；修改路径不会自动搬迁资料。</small></div>
  <div class="field full"><label for="sc-avatar-source">头像来源</label><select id="sc-avatar-source" v-model="draft.avatar_source"><option value="metatube">MetaTube</option><option value="gfriends">GFriends</option><option value="gfriends_then_metatube">GFriends 优先，MetaTube 回退</option></select><small class="hint">GFriends 无需 MetaTube Token。已有头像优先保留，同名歧义会跳过。</small></div>
  <template v-if="draft.avatar_source !== 'metatube'">
    <div class="field full"><label for="gf-index">GFriends 索引地址</label><input id="gf-index" v-model="draft.gfriends.index_url" type="url" class="mono" required></div>
    <div class="field full"><label for="gf-base">图片根地址</label><input id="gf-base" v-model="draft.gfriends.base_url" type="url" class="mono" required><small class="hint">使用镜像时，同时填写对应索引和图片根地址。</small></div>
    <div class="field"><label for="gf-hours">索引刷新间隔（小时）</label><input id="gf-hours" v-model.number="draft.gfriends.refresh_hours" type="number" min="1" max="720" required></div>
    <div class="field"><label>索引状态</label><span id="gf-status" role="status">{{ status?.version ? `${status.groups} 个来源 · ${status.entries} 条映射${status.stale ? ' · 使用旧缓存' : ''}` : '尚未下载索引' }}</span><small v-if="status?.version" class="hint">最近成功检查：{{ new Date(status.updated_at).toLocaleString() }}</small><small v-if="status?.error" class="hint">{{ status.error }}</small><button id="test-gfriends" type="button" class="btn btn-sm" :disabled="dirty || saving || testing.has('gfriends')" @click="emit('test','gfriends')">{{ testing.has('gfriends') ? '正在检查…' : '测试来源 / 刷新索引' }}</button><small class="hint">使用已保存设置，仅检查索引。</small></div>
  </template>
  <div class="field full"><label for="sc-avatars">旧头像来源目录</label><input id="sc-avatars" v-model="draft.avatars_dir" class="mono" placeholder="留空 = 与数据库同级 avatars/" data-private-path><small class="hint">用于兼容读取和复制导入，新头像统一写入资料目录。</small></div>
  <p v-if="draft.avatar_source !== 'gfriends'" class="hint full">MetaTube 地址、Token 和请求参数沿用<RouterLink to="/scrape">影片刮削配置</RouterLink>。</p>
  <div class="form-foot full"><button id="scrape-save" class="btn btn-accent" type="submit" :disabled="saving">{{ saving ? '正在保存…' : '保存头像设置' }}</button><button id="scrape-restore" class="btn" type="button" :disabled="saving || !dirty" @click="restore">恢复已保存设置</button><span id="scrape-dirty" class="hint" role="status">{{ dirty ? '有未保存的修改' : '当前设置已保存' }}</span></div>
</form></section></template>

<style scoped>
.full { grid-column: 1 / -1; }
.form-foot { flex-wrap: wrap; }
.hint { line-height: 1.7; }
</style>
