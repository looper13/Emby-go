<script setup lang="ts">
import type { FieldDiff } from '../../api/scrape'; defineProps<{ fields: FieldDiff[] }>();
</script>
<template><div><h3 class="scrape-h3">字段差异（预览只显示原文，翻译在确认后执行）</h3><dl class="scrape-diff"><div v-for="field in fields" :key="field.key" class="scrape-diff-row" :class="{'is-change': field.change}"><dt>{{ field.label }}</dt><dd><span v-if="field.old" class="old">{{ field.old }}{{ ' ' }}</span><span v-if="field.next" class="next">{{ field.next }}</span><span v-else class="old">（不写入）</span></dd></div></dl></div></template>
