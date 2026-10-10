<script setup lang="ts">
import type { Candidate } from '../../api/scrape';
import { numberUnlessInTitle } from '../../lib/media';
import MediaImage from '../media/MediaImage.vue';
defineProps<{ candidates: Candidate[]; provider?: string; id?: string; disabled: boolean; query: string; expected: string }>(); const emit = defineEmits<{ select: [candidate: Candidate] }>();
</script>
<template><div><p class="hint" style="margin:0 0 8px">候选 {{ candidates.length }} 条 · 搜索词 <code>{{ query }}</code><template v-if="expected"> · 预期番号 <code>{{ expected }}</code></template></p><div class="scrape-candidates"><button v-for="(item, index) in candidates" :key="item.provider + ':' + item.id" class="scrape-candidate" :class="{'is-active': provider === item.provider && id === item.id}" :data-idx="index" :disabled="disabled" @click="emit('select', item)"><MediaImage v-if="item.thumb" :src="item.thumb" alt="" loading="lazy" /><span v-else class="noimg"></span><span><strong>{{ item.title || '—' }}</strong><small>{{ [numberUnlessInTitle(item.title, item.number), item.provider, item.score ? '★ ' + item.score : '', item.exact ? '番号命中' : ''].filter(Boolean).join(' · ') }}</small></span></button></div></div></template>
