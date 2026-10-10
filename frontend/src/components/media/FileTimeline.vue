<script setup lang="ts">
import type { MovieDto } from '../../api/contracts'; import { fmtTime } from '../../lib/format'; import MetadataRow from './MetadataRow.vue';
defineProps<{ movie: MovieDto; modifiedAt?: string; expanded: boolean }>(); const emit = defineEmits<{ expanded: [value: boolean] }>();
</script>
<template><details class="detail-section item-details" :open="expanded" @toggle="emit('expanded', ($event.target as HTMLDetailsElement).open)"><summary>文件与时间</summary><dl class="detail-meta"><MetadataRow label="源文件" :value="movie.source_path" /><MetadataRow label="NFO" :value="movie.NFOPath" /><MetadataRow label="最后修改" :value="fmtTime(modifiedAt)" /><MetadataRow label="入库时间" :value="fmtTime(movie.created_at)" /><MetadataRow label="上次刮削" :value="fmtTime(movie.last_scrape_at)" /><MetadataRow label="刮削结果" :value="movie.last_scrape_error" /></dl></details></template>
