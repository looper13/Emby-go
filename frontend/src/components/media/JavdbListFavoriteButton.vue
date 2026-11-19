<script setup lang="ts">
import { onScopeDispose, ref, watch } from 'vue';
import { Heart } from '@lucide/vue';
import { javdbCommunityApi } from '../../api/javdb-community';
import { useToastStore } from '../../stores/toasts';

const props = defineProps<{
  libraryId: number;
  listId: string;
  favorite?: boolean;
  label?: string;
  itemId?: number;
  relatedPage?: number;
}>();
const emit = defineEmits<{ changed: [favorite: boolean] }>();
const selected = ref(props.favorite ?? false), busy = ref(false), toasts = useToastStore();
let active = true, generation = 0;
onScopeDispose(() => { active = false; });
watch(() => [props.libraryId, props.listId, props.favorite], () => { generation++; selected.value = props.favorite ?? false; busy.value = false; });

async function toggle() {
  if (busy.value) return;
  const current = ++generation, next = !selected.value;
  busy.value = true;
  try {
    if (next) {
      if (props.itemId === undefined || props.relatedPage === undefined) throw new Error('缺少清单来源，无法收藏');
      await javdbCommunityApi.setListFavorite(props.itemId, props.libraryId, props.listId, props.relatedPage);
    } else {
      await javdbCommunityApi.removeListFavorite(props.libraryId, props.listId);
    }
    if (active && current === generation) { selected.value = next; emit('changed', next); }
  } catch (cause) {
    if (active && current === generation) toasts.show(cause instanceof Error ? cause.message : '清单收藏操作失败', 'error');
  } finally { if (active && current === generation) busy.value = false; }
}
</script>

<template>
  <button type="button" class="javdb-list-favorite" :class="{ selected }" :disabled="busy" :aria-busy="busy" :aria-pressed="selected"
    :title="(selected ? '取消收藏清单' : '收藏清单') + (label ? ' · ' + label : '')" :aria-label="(selected ? '取消收藏清单' : '收藏清单') + (label ? ' · ' + label : '')" @click.stop.prevent="toggle">
    <Heart :size="18" :fill="selected ? 'currentColor' : 'none'" aria-hidden="true" />
  </button>
</template>

<style scoped>
.javdb-list-favorite { display: inline-grid; place-items: center; width: 44px; height: 44px; flex: 0 0 44px; padding: 0; border: 1px solid var(--line); border-radius: 6px; background: var(--panel-2); color: var(--muted); cursor: pointer; }
.javdb-list-favorite:hover, .javdb-list-favorite.selected { color: #ee8195; border-color: #b9687a; }
.javdb-list-favorite:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.javdb-list-favorite:disabled { opacity: .55; cursor: wait; }
</style>
