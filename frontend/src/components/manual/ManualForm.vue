<script setup lang="ts">
import { reactive, ref } from 'vue';
import { Film } from '@lucide/vue';
import type { ManualPayload } from '../../api/admin';
import { manualPayload } from '../../lib/manual';
const props = defineProps<{ submitting: boolean; libraryHint: string }>();
const emit = defineEmits<{ submit: [payload: ManualPayload] }>();
const fields = reactive<Record<string, string>>({ title: '', number: '', year: '', original_title: '', source_path: '', source_url: '', plot: '', genres: '', tags: '', studios: '' });
const error = ref('');
function submit() {
  if (props.submitting) return; error.value = '';
  try { emit('submit', manualPayload(fields)); } catch (cause) { error.value = cause instanceof Error ? cause.message : '表单无效'; }
}
</script>
<template>
  <form id="manual-form" class="field-grid" @submit.prevent="submit">
    <div class="field"><label for="m-title">标题 *</label><input id="m-title" v-model="fields.title" name="title" placeholder="展示标题" required></div>
    <div class="field"><label for="m-number">番号</label><input id="m-number" v-model="fields.number" name="number" placeholder="如 ABF-018"></div>
    <div class="field"><label for="m-year">年份</label><input id="m-year" v-model="fields.year" name="year" type="number" min="1900" max="2100" placeholder="2024"></div>
    <div class="field full"><label for="m-original">原名</label><input id="m-original" v-model="fields.original_title" name="original_title" placeholder="日文原名（可选）"></div>
    <div class="field full"><label for="m-path">.strm 目标路径 *</label><input id="m-path" v-model="fields.source_path" name="source_path" placeholder="服务器上源文件绝对路径，如 /data/media/AV/A/ABF-018/ABF-018.strm" required></div>
    <div class="field full"><label for="m-url">媒体直链（http/https）*</label><input id="m-url" v-model="fields.source_url" name="source_url" type="url" placeholder="https://…/ABF-018.mp4" required></div>
    <div class="field full"><label for="m-plot">简介</label><textarea id="m-plot" v-model="fields.plot" name="plot" placeholder="影片简介 / 剧情（可选）"></textarea></div>
    <div class="field"><label for="m-genres">类型（逗号分隔）</label><input id="m-genres" v-model="fields.genres" name="genres" placeholder="剧情, 偶像"></div>
    <div class="field"><label for="m-tags">标签</label><input id="m-tags" v-model="fields.tags" name="tags" placeholder="标签1, 标签2"></div>
    <div class="field"><label for="m-studios">制作商</label><input id="m-studios" v-model="fields.studios" name="studios" placeholder="制作商"></div>
    <p v-if="error" class="auth-error" style="grid-column:1/-1" role="alert">{{ error }}</p>
    <div class="form-foot" style="grid-column:1/-1"><button id="manual-submit" class="btn btn-accent" :disabled="submitting"><Film :size="15" :stroke-width="1.8" aria-hidden="true" /><span>写入并入库</span></button>
      <span v-if="libraryHint" class="hint" style="margin:0">将归入媒体库：{{ libraryHint }}</span></div>
  </form>
</template>
