<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue';
import { Heart } from '@lucide/vue';
import { favoritesApi } from '../../api/favorites';
import { useToastStore } from '../../stores/toasts';
const props = defineProps<{ itemId: string | number; favorite?: boolean; label?: string }>();
const emit = defineEmits<{ changed: [favorite: boolean] }>();
const selected = ref(props.favorite ?? false), busy = ref(false), toasts = useToastStore();
let active = true, generation = 0;
onScopeDispose(() => { active = false; });
watch(() => [props.itemId, props.favorite], () => { generation++; selected.value = props.favorite ?? false; busy.value = false; });
async function toggle() {
  if (busy.value) return;
  const current = ++generation, id = String(props.itemId);
  busy.value = true;
  try {
    const favorite = await favoritesApi.set(id, !selected.value);
    if (active && current === generation) { selected.value = favorite; emit('changed', favorite); }
  } catch (cause) {
    if (active && current === generation) toasts.show(cause instanceof Error ? cause.message : '收藏操作失败', 'error');
  } finally { if (active && current === generation) busy.value = false; }
}
</script>
<template>
  <button type="button" class="favorite-button" :class="{ selected }" :disabled="busy" :aria-busy="busy" :aria-pressed="selected"
    :title="(selected ? '取消收藏' : '收藏') + (label ? ' · ' + label : '')" :aria-label="(selected ? '取消收藏' : '收藏') + (label ? ' · ' + label : '')" @click.stop.prevent="toggle">
    <Heart :size="18" :fill="selected ? 'currentColor' : 'none'" aria-hidden="true" />
  </button>
</template>
<style scoped>
.favorite-button { display: inline-grid; place-items: center; width: 36px; height: 36px; flex: 0 0 36px; padding: 0; border: 1px solid var(--line); border-radius: 6px; background: var(--panel-2); color: var(--muted); cursor: pointer; }
.favorite-button:hover, .favorite-button.selected { color: #ee8195; border-color: #b9687a; }
.favorite-button:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.favorite-button:disabled { opacity: .55; cursor: wait; }
</style>
