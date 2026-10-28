<script setup lang="ts">
import { computed, onMounted, ref, shallowRef, watch } from 'vue';
import { useRouter } from 'vue-router';
import { Archive, RefreshCw } from '@lucide/vue';
import { catalogApi, type ResumeEntry } from '../api/catalog';
import { adminApi } from '../api/admin';
import { isAbortError } from '../api/client';
import { usePageRequest } from '../composables/usePageRequest';
import type { LibrariesDto, StatusDto, WallDto, MovieDto } from '../api/contracts';
import { useTasksStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import { usePlayerStore } from '../stores/player';
import MediaCard from '../components/media/MediaCard.vue';

interface Overview {
  libraries: LibrariesDto;
  successTotal: number;
  status: StatusDto;
  resume: ResumeEntry[];
  latest: WallDto;
}

const data = shallowRef<Overview | null>(null);
const loading = ref(false);
const error = ref('');
const requests = usePageRequest();
const tasks = useTasksStore();
const auth = useAuthStore();
const player = usePlayerStore();
const router = useRouter();

watch(() => tasks.revision, () => { void load(); });
const labels = { success: '已入库', manual: '手动录入', pending: '待补录', incompatible: '不兼容' };
const statusKeys: (keyof StatusDto)[] = ['success', 'manual', 'pending', 'incompatible'];
const rows = computed(() => statusKeys.flatMap((key) => {
  const count = data.value?.status[key] ?? 0;
  return count > 0 ? [{ key, label: labels[key], count }] : [];
}));
const cards = computed(() => data.value ? [
  { label: '媒体库', value: data.value.libraries.total, sub: 'Collection Folder' },
  { label: '已入库影片', value: data.value.successTotal, sub: 'NFO 真源 · 可见于 Emby' },
  { label: '待补录', value: data.value.status.pending, sub: '缺少 NFO 元数据' },
  { label: '不兼容源', value: data.value.status.incompatible, sub: '旧索引状态，扫描后重新归类' },
] : []);

function openItem(id: number) {
  void router.push({ name: 'item', params: { id } });
}

function quickplay(movie: MovieDto) {
  player.media(movie);
}

async function load() {
  const request = requests.begin();
  loading.value = true;
  error.value = '';
  try {
    const [libraries, successTotal, status, resume, latest] = await Promise.all([
      adminApi.libraries(request.signal),
      catalogApi.successTotal(request.signal),
      catalogApi.status(request.signal),
      catalogApi.resume(auth.user?.Id || 1, request.signal),
      catalogApi.latest(request.signal),
    ]);
    if (request.isCurrent()) data.value = { libraries, successTotal, status, resume, latest };
  } catch (cause) {
    if (request.isCurrent() && !isAbortError(cause)) {
      error.value = cause instanceof Error ? cause.message : '无法连接服务';
    }
  } finally {
    if (request.isCurrent()) loading.value = false;
  }
}

onMounted(() => { void load(); });
</script>

<template>
  <div v-if="data" class="cards">
    <div v-for="card in cards" :key="card.label" class="card">
      <small>{{ card.label }}</small><strong>{{ card.value }}</strong><span>{{ card.sub }}</span>
    </div>
  </div>

  <!-- 继续观看 (断点续播流) -->
  <section v-if="data?.resume?.length" class="overview-shelf-section" aria-label="继续观看">
    <div class="shelf-head">
      <div class="shelf-title-wrap">
        <h3 class="shelf-title">继续观看</h3>
        <span class="count-pill">{{ data.resume.length }}</span>
      </div>
    </div>
    <div class="media-shelf">
      <MediaCard
        v-for="entry in data.resume"
        :key="entry.movie.id"
        :movie="entry.movie"
        :tags="entry.tags"
        :userdata="entry.userdata"
        :can-manage="auth.isAdministrator"
        @open="openItem"
        @quickplay="quickplay(entry.movie)"
      />
    </div>
  </section>

  <!-- 最新入库 (横向滚动流) -->
  <section v-if="data?.latest?.items?.length" class="overview-shelf-section" aria-label="最新入库">
    <div class="shelf-head">
      <div class="shelf-title-wrap">
        <h3 class="shelf-title">最新入库</h3>
        <span class="count-pill">{{ data.latest.total || data.latest.items.length }}</span>
      </div>
      <router-link to="/wall?sort=datecreated&order=desc" class="link-more">查看全部 →</router-link>
    </div>
    <div class="media-shelf">
      <MediaCard
        v-for="item in data.latest.items"
        :key="item.id"
        :movie="item"
        :tags="data.latest.image_tags?.[item.id]"
        :userdata="data.latest.userdata?.[item.id]"
        :can-manage="auth.isAdministrator"
        @open="openItem"
        @quickplay="quickplay(item)"
      />
    </div>
  </section>

  <section class="panel" :aria-busy="loading">
    <div class="panel-head">
      <h2>档案状态</h2>
      <div class="panel-actions">
        <button id="reindex" class="btn" title="忽略变化记录，重新读取所有影片" :disabled="tasks.mutationBusy" @click="tasks.run('reindex')"><RefreshCw :size="15" :stroke-width="1.8" aria-hidden="true" /><span>全量重建索引</span></button>
        <button class="icon-btn" title="刷新" aria-label="刷新" :disabled="loading" @click="load">
          <RefreshCw :size="16" :stroke-width="1.8" aria-hidden="true" />
        </button>
      </div>
    </div>
    <p v-if="error" class="auth-error" role="alert">{{ error }}</p>
    <p v-if="loading && !data" role="status">加载中…</p>
    <template v-if="data">
      <div v-if="rows.length" class="table-wrap">
        <table>
          <thead><tr><th>状态</th><th style="width:80px">数量</th></tr></thead>
          <tbody>
            <tr v-for="row in rows" :key="row.key">
              <td><span class="badge" :class="row.key">{{ row.label }}</span></td><td class="num">{{ row.count }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">
        <Archive :size="40" :stroke-width="1.8" aria-hidden="true" /><p>档案为空</p><small>添加媒体库并开始扫描。</small>
      </div>
      <p class="hint">NFO 决定影片入库；播放时读取当前 .strm，仅支持 http/https 地址。</p>
    </template>
  </section>
</template>

<style scoped>
.overview-shelf-section {
  margin: 20px 0;
}

.shelf-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.shelf-title-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
}

.shelf-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--text);
  letter-spacing: -0.01em;
}

.count-pill {
  font-size: 11px;
  font-weight: 600;
  padding: 1px 7px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent-strong);
}

.link-more {
  font-size: 13px;
  color: var(--muted);
  text-decoration: none;
  transition: color 0.15s ease;
}

.link-more:hover {
  color: var(--accent);
}

.media-shelf {
  display: flex;
  gap: 14px;
  overflow-x: auto;
  padding: 4px 2px 14px;
  scroll-snap-type: x mandatory;
  -webkit-overflow-scrolling: touch;
}

.media-shelf > * {
  flex: 0 0 clamp(140px, 15vw, 175px);
  scroll-snap-align: start;
}
</style>
