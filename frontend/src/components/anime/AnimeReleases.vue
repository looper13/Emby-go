<script setup lang="ts">
import { safeExternal, type AnimeRelease } from '../../api/anime';
import { useToastStore } from '../../stores/toasts';
defineProps<{ items: AnimeRelease[] }>();
const toasts = useToastStore();
async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    toasts.show('磁力链接已复制');
  } catch {
    toasts.show('无法复制，请使用资源链接', 'error');
  }
}
</script>
<template>
  <article v-for="item in items" :key="item.id" class="anime-resource">
    <strong>{{ item.title }}</strong>
    <p class="hint">
      {{ item.group || '字幕组未标明' }} ·
      {{ item.resolution || '分辨率未标明' }} ·
      {{ item.subtitle || '字幕未标明' }} ·
      {{
        item.size
          ? `${(item.size / 1024 / 1024).toFixed(1)} MiB`
          : item.size_text
      }}
      ·
      {{
        item.published_at
          ? new Date(item.published_at).toLocaleString()
          : '时间未标明'
      }}
    </p>
    <p v-if="item.ambiguous" class="hint">
      合集或特殊发布；需按实际内容确认对应章节。
    </p>
    <div class="row-actions">
      <a
        v-if="safeExternal(item.url)"
        class="btn btn-sm"
        :href="safeExternal(item.url)"
        target="_blank"
        rel="noopener noreferrer"
        >来源页 ↗</a
      ><a
        v-if="safeExternal(item.torrent)"
        class="btn btn-sm"
        :href="safeExternal(item.torrent)"
        target="_blank"
        rel="noopener noreferrer"
        >种子链接 ↗</a
      ><button
        v-if="safeExternal(item.magnet, true)"
        class="btn btn-sm"
        @click="copy(item.magnet)"
      >
        复制磁力
      </button>
    </div>
  </article>
  <p v-if="!items.length" class="hint">暂无发布资源。</p>
</template>
