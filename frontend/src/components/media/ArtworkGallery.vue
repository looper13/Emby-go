<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import type { ImageDto } from '../../api/contracts';
import { imageURL } from '../../lib/image-url';
import MediaImage from './MediaImage.vue';
import ArtworkLightbox from './ArtworkLightbox.vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

const props = withDefaults(defineProps<{ id?: number; images?: ImageDto[]; urls?: string[] }>(), { images: () => [] });
const { hideImages } = useMediaVisibility();
const active = ref<number | null>(null);
const thumbs: (HTMLButtonElement | null)[] = [];
const sources = computed(() => {
  if (props.urls !== undefined) return props.urls;
  const id = props.id;
  return id === undefined ? [] : props.images.map(image => imageURL(id, 'Backdrop', 480, image.ImageTag, image.ImageIndex));
});

// 更换影片或隐藏图片时丢弃旧灯箱状态，重新显示图片不会自动打开原图。
watch([sources, hideImages], () => { active.value = null; }, { flush: 'sync', deep: true });

// 关闭后回到原始触发按钮，不因浏览其它剧照改变键盘位置。
function close() {
  const trigger = active.value === null ? null : thumbs[active.value];
  active.value = null;
  void nextTick(() => { if (active.value === null && trigger?.isConnected) trigger.focus({ preventScroll: true }); });
}
</script>

<template>
  <section v-if="!hideImages && sources.length" class="detail-section">
    <h3>
      <span>剧照</span>
      <small>{{ sources.length }}</small>
    </h3>
    <div class="detail-artwork-grid">
      <button
        v-for="(source, index) in sources"
        :key="`${index}:${source}`"
        :ref="el => (thumbs[index] = el as HTMLButtonElement | null)"
        type="button"
        class="artwork-thumb"
        :title="'查看剧照 ' + (index + 1)"
        :aria-label="'查看剧照 ' + (index + 1)"
        @click="active = index"
      >
        <MediaImage
          :src="source"
          loading="lazy"
          :alt="'剧照 ' + (index + 1)"
        />
      </button>
    </div>
    <ArtworkLightbox
      v-if="active !== null"
      :id="id"
      :images="images"
      :urls="urls"
      :index="active"
      @close="close"
    />
  </section>
</template>
