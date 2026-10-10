<script setup lang="ts">
import type { MovieDto, WallDto } from '../../api/contracts';
import MediaCard from './MediaCard.vue';
defineProps<{ items: MovieDto[]; imageTags: WallDto['image_tags']; userdata?: WallDto['userdata']; busy?: Set<string> }>();
const emit = defineEmits<{ open: [id: number]; quickplay: [id: number]; probe: [id: number]; reread: [id: number]; remove: [id: number] }>();
</script>
<template><div id="wall" class="wall"><MediaCard v-for="movie in items" :key="movie.id" :movie="movie" :tags="imageTags[String(movie.id)]" :userdata="userdata?.[String(movie.id)]" :busy="busy" @open="emit('open', $event)" @quickplay="emit('quickplay', $event)" @probe="emit('probe', $event)" @reread="emit('reread', $event)" @remove="emit('remove', $event)" /></div></template>
