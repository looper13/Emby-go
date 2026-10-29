<script setup lang="ts">
import { computed, onMounted, onScopeDispose, ref, watch } from 'vue';
import { ChevronLeft, ChevronRight, X } from '@lucide/vue';
import type { ImageDto } from '../../api/contracts';
import { imageURL } from '../../lib/image-url';
const props = withDefaults(defineProps<{ id?: number; images?: ImageDto[]; urls?: string[]; index: number }>(), { images: () => [] });
const emit = defineEmits<{ close: [index: number] }>();
const shell = ref<HTMLElement>();
const closeButton = ref<HTMLButtonElement>();
const count = computed(() => props.urls?.length ?? props.images.length);
const clamp = (index: number) => Math.max(0, Math.min(index, count.value - 1));
const current = ref(clamp(props.index));
let previousOverflow = '';
let locked = false;
function source(at: number) {
  if (props.urls !== undefined) return props.urls[at] ?? '';
  const image = props.images[at];
  return image && props.id !== undefined ? imageURL(props.id, 'Backdrop', undefined, image.ImageTag, image.ImageIndex) : '';
}
const many = computed(() => count.value > 1);
// 预取相邻剧照原图，左右切换时不必等图片重新下载。
const neighbors = computed(() =>
  many.value
    ? [...new Set([(current.value + 1) % count.value, (current.value - 1 + count.value) % count.value].map(source))].filter(Boolean)
    : [],
);
function step(delta: number) { if (count.value) current.value = (current.value + delta + count.value) % count.value; }
function close() { emit('close', current.value); }
function release() {
  if (!locked) return;
  document.body.style.overflow = previousOverflow;
  window.removeEventListener('keydown', keyboard, true);
  locked = false;
}
watch([() => props.index, count], ([index]) => {
  current.value = clamp(index);
  if (!count.value) { release(); close(); }
});
function keyboard(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault();
    event.stopImmediatePropagation();
    close();
    return;
  }
  if (many.value && (event.key === 'ArrowLeft' || event.key === 'ArrowRight')) {
    event.preventDefault();
    step(event.key === 'ArrowLeft' ? -1 : 1);
    return;
  }
  if (event.key !== 'Tab' || !shell.value) return;
  const candidates = Array.from(
    shell.value.querySelectorAll<HTMLElement>(
      'button, a[href], input, select, [tabindex]:not([tabindex="-1"])',
    ),
  ).filter(
    (el) => !el.hasAttribute('disabled') && el.getClientRects().length > 0,
  );
  const first = candidates[0] ?? closeButton.value;
  const last = candidates.at(-1) ?? first;
  if (
    event.shiftKey &&
    (document.activeElement === first ||
      !shell.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    last?.focus();
  } else if (
    !event.shiftKey &&
    (document.activeElement === last ||
      !shell.value.contains(document.activeElement))
  ) {
    event.preventDefault();
    first?.focus();
  }
}
onMounted(() => {
  if (!count.value) { close(); return; }
  previousOverflow = document.body.style.overflow;
  document.body.style.overflow = 'hidden';
  locked = true;
  closeButton.value?.focus();
  window.addEventListener('keydown', keyboard, true);
});
onScopeDispose(release);
</script>
<template>
  <div v-if="count" id="artwork-overlay" class="artwork-overlay" @click.self="close()">
    <div
      ref="shell"
      class="artwork-shell"
      role="dialog"
      aria-modal="true"
      aria-labelledby="artwork-title"
    >
      <div class="artwork-head">
        <strong id="artwork-title">剧照 {{ current + 1 }} / {{ count }}</strong
        ><button
          id="artwork-close"
          ref="closeButton"
          class="icon-btn"
          title="关闭 (Esc)"
          @click="close()"
        >
          <X :size="16" aria-hidden="true" />
        </button>
      </div>
      <div class="artwork-stage" @click.self="close()">
        <button
          v-if="many"
          id="artwork-prev"
          class="artwork-nav is-prev"
          title="上一张 (←)"
          aria-label="上一张剧照"
          @click="step(-1)"
        >
          <ChevronLeft :size="26" aria-hidden="true" />
        </button>
        <img class="artwork-image" :src="source(current)" :alt="`剧照 ${current + 1}`" decoding="async">
        <button
          v-if="many"
          id="artwork-next"
          class="artwork-nav is-next"
          title="下一张 (→)"
          aria-label="下一张剧照"
          @click="step(1)"
        >
          <ChevronRight :size="26" aria-hidden="true" />
        </button>
      </div>
      <img v-for="url in neighbors" :key="url" class="artwork-preload" :src="url" alt="" aria-hidden="true">
    </div>
  </div>
</template>
