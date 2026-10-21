<script setup lang="ts">
import { ref, shallowRef, watch } from 'vue';
import { Play, SlidersHorizontal } from '@lucide/vue';
import DetailPlaybackControls from './DetailPlaybackControls.vue';
import type { TrackSelection } from '../../lib/player-tracks';
const props = defineProps<{ itemId: string; disabled?: boolean }>();
const emit = defineEmits<{ play: [selection: TrackSelection | undefined] }>();
const expanded = ref(false), opened = ref(false), selection = shallowRef<TrackSelection>();
watch(() => props.itemId, () => { expanded.value = false; opened.value = false; selection.value = undefined; });
</script>
<template>
  <div class="detail-play-action" :class="{ expanded }">
    <div class="detail-play-buttons">
      <button class="btn btn-accent" :disabled="disabled" @click="emit('play', selection)"><Play :size="16" aria-hidden="true" />播放</button>
      <button class="icon-btn" title="播放设置与外部播放器" aria-label="播放设置与外部播放器" :aria-expanded="expanded" @click="opened = true; expanded = !expanded"><SlidersHorizontal :size="17" aria-hidden="true" /></button>
    </div>
    <DetailPlaybackControls v-if="opened" v-show="expanded" :item-id="itemId" @selection="selection = $event" />
  </div>
</template>
<style scoped>
.detail-play-action { min-width: 0; max-width: 100%; }
.detail-play-action.expanded { width: 500px; }
.detail-play-buttons { display: flex; align-items: center; justify-content: flex-end; gap: 6px; }
.detail-play-buttons .btn { display: inline-flex; align-items: center; gap: 6px; }
</style>
