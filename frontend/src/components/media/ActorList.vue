<script setup lang="ts">
import type { ActorDto } from '../../api/contracts';
import { entityIdOf } from '../../lib/media';
import { imageURL } from '../../lib/image-url';
import MediaImage from './MediaImage.vue';
import FavoriteButton from './FavoriteButton.vue';
import { useMediaVisibility } from '../../composables/useMediaVisibility';

defineProps<{ actors: ActorDto[] }>();
const emit = defineEmits<{ entity: [key: string, value: string] }>();
const { hideImages } = useMediaVisibility();
</script>

<template>
  <section class="detail-section">
    <h3>
      <span>演职人员</span>
      <small>{{ actors.length }}</small>
    </h3>
    <div v-if="actors.length" class="avatar-grid">
      <div v-for="actor in actors" :key="actor.name" class="actor-entry">
        <button
          class="avatar-card"
          data-entity-key="person"
          :data-entity-value="actor.name"
          :title="`查看 ${actor.name} 相关影片`"
          @click="emit('entity', 'person', actor.name)"
        >
          <MediaImage
            v-if="!hideImages && actor.has_image"
            :src="imageURL(entityIdOf('person', actor.name), 'Primary', 200, actor.image_tag)"
            loading="lazy"
            alt=""
          />
          <span v-else class="avatar-fallback">{{ (actor.name || '?').trim().slice(0, 1) }}</span>
          <small>{{ actor.name }}</small>
        </button>
        <FavoriteButton :item-id="entityIdOf('person', actor.name)" :favorite="actor.favorite" :label="actor.name" />
      </div>
    </div>
    <p v-else class="hint" style="margin:8px 0 0">NFO 中没有演员信息。</p>
  </section>
</template>

<style scoped>
.actor-entry {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.actor-entry .avatar-card {
  width: 100%;
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  background: transparent;
  border: 0;
  cursor: pointer;
  padding: 0;
}

.avatar-card small {
  font-size: 12px;
  font-weight: 500;
  color: var(--muted);
  text-align: center;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
  transition: color 0.15s ease;
}

.avatar-card:hover small {
  color: var(--accent);
}
</style>
