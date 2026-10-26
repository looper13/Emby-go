<script setup lang="ts">
import type { MovieDto, WallDto } from '../../api/contracts';
import MediaCard from './MediaCard.vue';
import SkeletonCard from '../common/SkeletonCard.vue';

defineProps<{
  items: MovieDto[];
  imageTags: WallDto['image_tags'];
  userdata?: WallDto['userdata'];
  busy?: Set<string>;
  canManage?: boolean;
  loading?: boolean;
}>();

const emit = defineEmits<{
  open: [id: number];
  quickplay: [id: number];
  probe: [id: number];
  reread: [id: number];
  remove: [id: number];
}>();
</script>

<template>
  <div id="wall" class="wall">
    <SkeletonCard v-if="loading && !items.length" :count="14" />
    <MediaCard
      v-for="movie in items"
      :key="movie.id"
      :movie="movie"
      :tags="imageTags[String(movie.id)]"
      :userdata="userdata?.[String(movie.id)]"
      :busy="busy"
      :can-manage="canManage"
      @open="emit('open', $event)"
      @quickplay="emit('quickplay', $event)"
      @probe="emit('probe', $event)"
      @reread="emit('reread', $event)"
      @remove="emit('remove', $event)"
    />
  </div>
</template>
