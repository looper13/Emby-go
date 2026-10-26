<script setup lang="ts">
import { ref } from 'vue';
import { Copy, Check } from '@lucide/vue';
import type { FileDto, StreamDto } from '../../api/contracts';
import { fmtSize } from '../../lib/media';

defineProps<{ files: FileDto[] }>();
const types: Record<string, string> = { Video: '视频', Audio: '音频', Subtitle: '字幕' };
const copiedPath = ref<string | null>(null);

function parameters(s: StreamDto) {
  return (s.Type === 'Video'
    ? [s.Codec?.toUpperCase(), s.Width && s.Height ? `${s.Width}×${s.Height}` : '', s.BitRate ? `${Math.round(s.BitRate / 1000)} kbps` : '', s.Profile, s.BitDepth ? `${s.BitDepth}bit` : '']
    : [s.Codec?.toUpperCase(), s.ChannelLayout || (s.Channels ? `${s.Channels}ch` : ''), s.BitRate ? `${Math.round(s.BitRate / 1000)} kbps` : '', s.Language, s.SampleRate ? `${s.SampleRate} Hz` : '']
  ).filter(Boolean).join(' · ') || '—';
}

async function copyPath(path: string) {
  try {
    await navigator.clipboard.writeText(path);
    copiedPath.value = path;
    setTimeout(() => { if (copiedPath.value === path) copiedPath.value = null; }, 2000);
  } catch {
    // Clipboard fallback
  }
}
</script>

<template>
  <section class="detail-section">
    <h3>
      <span>媒体信息</span>
      <small>{{ files.length }} 个文件</small>
    </h3>
    <div v-for="file in files" :key="file.index" class="detail-file">
      <div class="detail-file-head">
        <strong>{{ file.role === 'main' ? '主文件' : `分段 ${file.index - 1}` }}</strong>
        <span class="mono file-name">{{ file.name }}</span>
        <span class="file-size-badge">{{ fmtSize(file.size) }}</span>
        <span v-if="!file.probed" class="badge pending">未探测</span>
      </div>

      <div v-if="file.streams.length" class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>#</th>
              <th>类型</th>
              <th>参数</th>
              <th>标记</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(s, index) in file.streams" :key="index">
              <td class="mono">#{{ s.Index ?? '' }}</td>
              <td>
                <span class="stream-type-tag" :class="s.Type?.toLowerCase()">
                  {{ types[s.Type || ''] || s.Type || '—' }}
                </span>
              </td>
              <td class="mono stream-param-cell">{{ parameters(s) }}</td>
              <td class="mono stream-flags-cell">
                {{ [s.IsDefault ? '默认' : '', s.IsForced ? '强制' : '', s.IsExternal ? '外挂' : ''].filter(Boolean).join(' ') || '—' }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="hint" style="margin:8px 0 0">未探测</p>

      <div class="detail-path-row">
        <p class="detail-path mono">{{ file.path }}</p>
        <button
          type="button"
          class="btn-copy-path"
          :title="copiedPath === file.path ? '已复制' : '复制文件路径'"
          @click="copyPath(file.path)"
        >
          <Check v-if="copiedPath === file.path" :size="13" class="copy-success" aria-hidden="true" />
          <Copy v-else :size="13" aria-hidden="true" />
          <span>{{ copiedPath === file.path ? '已复制' : '复制' }}</span>
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.file-name {
  font-weight: 500;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.file-size-badge {
  font-family: var(--font-mono);
  font-size: 11.5px;
  color: var(--dim);
  background: var(--panel);
  padding: 1px 6px;
  border-radius: 4px;
  border: 1px solid var(--line);
}

.stream-type-tag {
  display: inline-flex;
  align-items: center;
  padding: 1px 7px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  background: var(--panel);
  border: 1px solid var(--line);
  color: var(--muted);
}

.stream-type-tag.video {
  background: rgba(43, 122, 254, 0.12);
  border-color: rgba(43, 122, 254, 0.3);
  color: var(--accent);
}

.stream-type-tag.audio {
  background: rgba(32, 201, 151, 0.12);
  border-color: rgba(32, 201, 151, 0.3);
  color: var(--ok);
}

.stream-type-tag.subtitle {
  background: rgba(117, 92, 255, 0.12);
  border-color: rgba(117, 92, 255, 0.3);
  color: #a38cf8;
}

.stream-param-cell {
  font-size: 12px;
  color: var(--text);
}

.stream-flags-cell {
  font-size: 11.5px;
  color: var(--dim);
}

.detail-path-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 10px;
  padding: 7px 10px;
  border-radius: 6px;
  background: var(--panel);
  border: 1px solid var(--line);
}

.detail-path {
  margin: 0;
  font-size: 11.5px;
  color: var(--dim);
  word-break: break-all;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

.btn-copy-path {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border-radius: 4px;
  background: var(--panel-2);
  border: 1px solid var(--line);
  color: var(--muted);
  font-size: 11px;
  cursor: pointer;
  flex-shrink: 0;
  transition: all 0.15s ease;
}

.btn-copy-path:hover {
  background: var(--line);
  color: var(--text);
}

.copy-success {
  color: var(--ok);
}
</style>
