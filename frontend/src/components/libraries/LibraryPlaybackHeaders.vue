<script setup lang="ts">
import { ref } from 'vue';
import { Plus, X } from '@lucide/vue';

const props = defineProps<{ initialHeaders?: Record<string, string>; disabled: boolean; idPrefix: string }>();
const entries = Object.entries(props.initialHeaders ?? {});
const referer = ref(entries.find(([name]) => name.toLowerCase() === 'referer')?.[1] ?? '');
const origin = ref(entries.find(([name]) => name.toLowerCase() === 'origin')?.[1] ?? '');
let nextId = 0;
const extra = ref(entries.filter(([name]) => !['referer', 'origin'].includes(name.toLowerCase()))
  .map(([name, value]) => ({ id: nextId++, name, value })));

function getHeaders(): Record<string, string> {
  const headers: [string, string][] = [['Referer', referer.value], ['Origin', origin.value]];
  headers.push(...extra.value.map(row => [row.name, row.value] as [string, string]));
  const names = new Set<string>();
  const result: [string, string][] = [];
  for (const [rawName, rawValue] of headers) {
    const name = rawName.trim(), value = rawValue.trim();
    if (!name && !value) continue;
    if (!name || !/^[!#$%&'*+.^_`|~\da-z-]+$/i.test(name)) throw new Error('请求头名称只能包含英文字母、数字和合法符号');
    if (/[\u0000-\u001f\u007f]/u.test(rawValue)) throw new Error(`请求头 ${name} 不能包含换行或控制字符`);
    if (!value) continue;
    const key = name.toLowerCase();
    if (names.has(key)) throw new Error(`请求头 ${name} 重复，请合并为一项`);
    names.add(key);
    result.push([name, value]);
  }
  return Object.fromEntries(result);
}
defineExpose({ getHeaders });
</script>

<template>
  <fieldset class="library-headers" :disabled="disabled">
    <legend>播放源请求头</legend>
    <p class="hint">由服务器拉取视频时发送，适用于防盗链源。留空可清除对应请求头。</p>
    <div class="header-standard-fields">
      <div class="field">
        <label :for="`${idPrefix}-referer`">Referer</label>
        <input :id="`${idPrefix}-referer`" v-model="referer" placeholder="https://example.com/watch/" autocomplete="off">
      </div>
      <div class="field">
        <label :for="`${idPrefix}-origin`">Origin</label>
        <input :id="`${idPrefix}-origin`" v-model="origin" placeholder="https://example.com" autocomplete="off">
      </div>
    </div>
    <div v-for="(row, index) in extra" :key="row.id" class="header-extra-row" data-header-row>
      <div class="field">
        <label :for="`${idPrefix}-header-name-${row.id}`">请求头名称 {{ index + 1 }}</label>
        <input :id="`${idPrefix}-header-name-${row.id}`" v-model="row.name" data-header-name placeholder="如 User-Agent" autocomplete="off">
      </div>
      <div class="field">
        <label :for="`${idPrefix}-header-value-${row.id}`">请求头值</label>
        <input :id="`${idPrefix}-header-value-${row.id}`" v-model="row.value" data-header-value placeholder="请求头的值" autocomplete="off">
      </div>
      <button class="icon-btn" type="button" :aria-label="`移除请求头 ${index + 1}`" @click="extra.splice(index, 1)"><X :size="16" aria-hidden="true" /></button>
    </div>
    <button class="btn btn-sm" type="button" data-add-header @click="extra.push({ id: nextId++, name: '', value: '' })"><Plus :size="14" aria-hidden="true" /><span>添加请求头</span></button>
  </fieldset>
</template>

<style scoped>
.library-headers { min-width: 0; border: 1px solid var(--line); border-radius: 8px; padding: 12px 14px; }
.library-headers legend { padding-inline: 6px; font-size: 13px; font-weight: 600; }
.library-headers .hint { margin: 0 0 12px; }
.header-standard-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.header-extra-row { display: grid; grid-template-columns: minmax(140px, 1fr) minmax(0, 2fr) auto; align-items: end; gap: 10px; margin-top: 12px; }
.header-extra-row .icon-btn { margin-bottom: 4px; }
.library-headers > .btn { margin-top: 12px; }
@media (max-width: 600px) {
  .header-standard-fields { grid-template-columns: minmax(0, 1fr); }
  .header-extra-row { grid-template-columns: minmax(0, 1fr) auto; }
  .header-extra-row .field:first-child { grid-column: 1 / -1; }
}
</style>
