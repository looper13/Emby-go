<script setup lang="ts">
import { nextTick, ref } from 'vue';
import type { ImageDto } from '../../api/contracts';
import { imageURL } from '../../lib/image-url';
import MediaImage from './MediaImage.vue';
import ArtworkLightbox from './ArtworkLightbox.vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

defineProps<{ id: number; images: ImageDto[] }>();
const { hideImages } = useMediaVisibility();
const active = ref<number | null>(null);
const thumbs: HTMLButtonElement[] = [];

// 关闭后把焦点还给打开它的那张缩略图，键盘用户不会掉回页面顶部。
function close(index: number) {
  active.value = null;
  void nextTick(() => thumbs[index]?.focus());
}
</script>

<template>
  <section v-if="!hideImages && images.length" class="detail-section">
    <h3>
      <span>剧照</span>
      <small>{{ images.length }}</small>
    </h3>
    <div class="detail-artwork-grid">
      <button
        v-for="(image, index) in images"
        :key="image.ImageIndex ?? index"
        :ref="el => (thumbs[index] = el as HTMLButtonElement)"
        type="button"
        class="artwork-thumb"
        :title="'查看剧照 ' + (index + 1)"
        :aria-label="'查看剧照 ' + (index + 1)"
        @click="active = index"
      >
        <MediaImage
          :src="imageURL(id, 'Backdrop', 480, image.ImageTag, image.ImageIndex)"
          loading="lazy"
          :alt="'剧照 ' + (index + 1)"
        />
      </button>
    </div>
    <ArtworkLightbox
      v-if="active !== null"
      :id="id"
      :images="images"
      :index="active"
      @close="close"
    />
  </section>
</template>
