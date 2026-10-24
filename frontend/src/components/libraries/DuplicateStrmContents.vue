<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue';
import { duplicatesApi, type DuplicateMovie, type DuplicateStrmContents } from '../../api/duplicates';
import { usePageRequest } from '../../composables/usePageRequest';

const props = defineProps<{ movie: DuplicateMovie; libraryId: number; groupKey: string }>();
const result = shallowRef<DuplicateStrmContents|null>(null), loading = ref(false), error = ref('');
const scope = usePageRequest();
const files = computed(() => [props.movie.source_path, ...props.movie.additional_parts].map(local_path => ({
  local_path, loaded: result.value?.files.find(file => file.local_path === local_path),
})));

async function load() {
  const current = scope.begin(); result.value = null; loading.value = true; error.value = '';
  try {
    const contents = await duplicatesApi.strm(props.movie.id, props.libraryId, props.groupKey, current.signal);
    if (current.isCurrent()) result.value = contents;
  } catch (cause) {
    if (current.isCurrent()) error.value = (cause instanceof Error || cause instanceof DOMException) && cause.name === 'TimeoutError'
      ? '读取 STRM 内容超时，请检查媒体库目录后重试。'
      : cause instanceof Error || cause instanceof DOMException ? cause.message : '读取 STRM 内容失败';
  } finally {
    if (current.isCurrent()) loading.value = false;
  }
}
watch(() => [props.movie.id, props.libraryId, props.groupKey], load, { immediate:true });
</script>

<template>
  <div class="duplicate-strm" :aria-busy="loading">
    <p v-if="loading" class="hint" role="status">正在读取 STRM 文件内容…</p>
    <p v-if="error" class="auth-error" role="alert">{{ error }} <button class="btn btn-sm" @click="load">重试读取</button></p>
    <div v-for="(file, index) in files" :key="file.local_path" class="duplicate-strm-file">
      <p class="mono duplicate-path" data-private-path>{{ index ? '分段：' : '' }}{{ file.local_path }}</p>
      <template v-if="file.loaded">
        <p v-if="file.loaded.error" class="hint duplicate-strm-error">{{ file.loaded.error }}</p>
        <template v-else>
          <p class="hint duplicate-strm-label">STRM 内容</p>
          <pre v-if="file.loaded.content" class="mono duplicate-strm-content" data-private-path>{{ file.loaded.content }}</pre>
          <p v-else class="hint">STRM 文件为空。</p>
        </template>
      </template>
    </div>
  </div>
</template>
