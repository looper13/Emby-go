<script setup lang="ts">
import { ref, watch } from 'vue';
import { RefreshCw, Trash2, Folder, Film, Tv, Layers, Shuffle, LayoutGrid, List } from '@lucide/vue';
import { adminApi } from '../api/admin';
import { catalogApi } from '../api/catalog';
import { useAdminRead } from '../composables/useAdminRead';
import { useTasksStore } from '../stores/tasks';
import { useToastStore } from '../stores/toasts';
import { usePlayerStore } from '../stores/player';
import LibraryForm from '../components/libraries/LibraryForm.vue';
import R18LibraryForm from '../components/libraries/R18LibraryForm.vue';
import DuplicateMoviesPanel from '../components/libraries/DuplicateMoviesPanel.vue';
import EmptyState from '../components/common/EmptyState.vue';
import ReadState from '../components/common/ReadState.vue';
import type { LibraryContentType, LibraryDto } from '../api/contracts';

function contentLabel(lib: LibraryDto) {
  if (lib.SourceType === 'postgres_r18') return 'R18 资料库';
  return lib.Type === 'tvshows' ? '电视节目' : lib.Type === 'mixed' ? '混合内容' : '影片';
}

const { data, loading, error, load, active } = useAdminRead(adminApi.libraries);
const tasks = useTasksStore();
const toasts = useToastStore();
const player = usePlayerStore();
const duplicates = ref<InstanceType<typeof DuplicateMoviesPanel>>();
const submitting = ref(false);
const formKey = ref(0);
const deleting = ref(new Set<number>());
const updatingProxy = ref(new Set<number>());
const playingLibraryId = ref<number | null>(null);
const editingR18 = ref<number>();
const r18FormKey = ref(0);
function browseLink(lib: LibraryDto) { return { path: lib.SourceType === 'postgres_r18' ? '/r18' : '/items', query: { library_id: lib.Id } }; }
function sourceLabel(lib: LibraryDto) { return lib.SourceType === 'postgres_r18' ? 'PostgreSQL / r18_video + article' : lib.Path; }
async function r18Saved(lib: LibraryDto) { toasts.show(`已保存媒体库「${lib.Name}」`); editingR18.value = undefined; r18FormKey.value += 1; if (active()) await load(); }

const VIEW_STORAGE_KEY = 'emby_library_view_mode';
function readInitialView(): 'grid' | 'table' {
  try {
    return (localStorage.getItem(VIEW_STORAGE_KEY) as 'grid' | 'table') || 'grid';
  } catch {
    return 'grid';
  }
}
const viewMode = ref<'grid' | 'table'>(readInitialView());
function setViewMode(mode: 'grid' | 'table') {
  viewMode.value = mode;
  try {
    localStorage.setItem(VIEW_STORAGE_KEY, mode);
  } catch {}
}

async function playRandom(lib: LibraryDto) {
  if (playingLibraryId.value === lib.Id) return;
  playingLibraryId.value = lib.Id;
  try {
    const q = new URLSearchParams({
      library_id: String(lib.Id),
      sort: 'random',
      seed: String((crypto.getRandomValues(new Uint32Array(1))[0]! & 0x7fffffff) || 1),
      limit: '1',
      offset: '0',
    });
    if (lib.Type === 'mixed') {
      q.set('media_type', 'Movie');
    }
    const result = await catalogApi.items(q);
    if (result.items.length > 0) {
      player.media(result.items[0]);
    } else {
      toasts.show(`媒体库「${lib.Name}」暂无可用影片`, 'error');
    }
  } catch (cause) {
    toasts.show(cause instanceof Error ? cause.message : '随机播放失败', 'error');
  } finally {
    playingLibraryId.value = null;
  }
}

watch(() => tasks.revision, () => {
  void load();
});

async function create(payload: {
  Name: string;
  Path: string;
  Type: LibraryContentType;
  ProxyEnabled: boolean;
  ResolveRedirects: boolean;
}) {
  if (submitting.value || !payload.Name.trim() || !payload.Path.trim()) return;
  submitting.value = true;
  try {
    const lib = await adminApi.createLibrary(payload);
    toasts.show(`已添加媒体库「${lib.Name}」`);
    if (active()) {
      formKey.value += 1;
      await load();
    }
  } catch (cause) {
    toasts.show(cause instanceof Error ? cause.message : '添加失败', 'error');
  } finally {
    submitting.value = false;
  }
}

async function toggleProxy(lib: LibraryDto, event: Event, field: 'ProxyEnabled' | 'ResolveRedirects' = 'ProxyEnabled') {
  const input = event.target as HTMLInputElement;
  const enabled = input.checked;
  if (updatingProxy.value.has(lib.Id) || lib.Revision === undefined) {
    input.checked = !enabled;
    return;
  }
  updatingProxy.value.add(lib.Id);
  try {
    await adminApi.updateLibrary(lib.Id, {
      ProxyEnabled: lib.ProxyEnabled === true,
      ResolveRedirects: lib.ResolveRedirects === true,
      [field]: enabled,
      Revision: lib.Revision,
    });
    toasts.show(`媒体库「${lib.Name}」已${enabled ? '开启' : '关闭'}${field === 'ResolveRedirects' ? '直链解析' : '播放代理'}`);
    if (active()) await load();
  } catch (cause) {
    input.checked = !enabled;
    toasts.show(cause instanceof Error ? cause.message : '保存失败', 'error');
    if (active()) await load();
  } finally {
    updatingProxy.value.delete(lib.Id);
  }
}

async function remove(id: number) {
  if (
    deleting.value.has(id) ||
    !window.confirm('删除该媒体库及其影片索引？不会删除磁盘文件，但该库影片的播放进度/收藏会一并清除。')
  )
    return;
  deleting.value.add(id);
  try {
    await adminApi.deleteLibrary(id);
    toasts.show('媒体库已删除');
    if (active()) await load();
  } catch (cause) {
    toasts.show(cause instanceof Error ? cause.message : '删除失败', 'error');
  } finally {
    deleting.value.delete(id);
  }
}
</script>

<template>
  <section class="panel library-manage-panel">
    <div class="panel-head">
      <div class="panel-head-title">
        <Folder :size="20" class="panel-icon" aria-hidden="true" />
        <h2>媒体库</h2>
      </div>
      <div class="library-head-actions">
        <span class="hint" style="margin:0">共 {{ data?.total ?? 0 }} 个媒体库 · 扫描与浏览均以库为单位</span>
        <div v-if="data?.items?.length" class="view-mode-toggle" role="group" aria-label="视图模式">
          <button
            type="button"
            class="view-mode-btn"
            :class="{ 'is-active': viewMode === 'grid' }"
            title="卡片视图"
            aria-label="卡片视图"
            @click="setViewMode('grid')"
          >
            <LayoutGrid :size="13" aria-hidden="true" />
            <span>卡片</span>
          </button>
          <button
            type="button"
            class="view-mode-btn"
            :class="{ 'is-active': viewMode === 'table' }"
            title="列表视图"
            aria-label="列表视图"
            @click="setViewMode('table')"
          >
            <List :size="13" aria-hidden="true" />
            <span>列表</span>
          </button>
        </div>
      </div>
    </div>

    <ReadState :error="error" :loading="loading" :empty="!data" @retry="load" />

    <!-- 卡片网格视图 -->
    <div v-if="data?.items?.length && viewMode === 'grid'" class="library-card-grid">
      <article v-for="lib in data.items" :key="lib.Id" class="library-card">
        <div class="lib-card-head">
          <div class="lib-card-icon" :class="lib.Type">
            <Tv v-if="lib.Type === 'tvshows'" :size="20" aria-hidden="true" />
            <Layers v-else-if="lib.Type === 'mixed'" :size="20" aria-hidden="true" />
            <Film v-else :size="20" aria-hidden="true" />
          </div>
          <div class="lib-card-title-group">
            <div class="lib-card-title-row">
              <strong class="lib-card-title">{{ lib.Name }}</strong>
              <span class="id-badge">#{{ lib.Id }}</span>
            </div>
            <span class="lib-type-pill" :class="lib.Type">
              <span>{{ contentLabel(lib) }}</span>
            </span>
          </div>
        </div>

        <div class="lib-card-body">
          <div class="lib-card-path" :title="sourceLabel(lib)">
            <span class="lib-path-label">{{ lib.SourceType === 'postgres_r18' ? '来源' : '路径' }}</span>
            <span class="mono lib-path-text">{{ sourceLabel(lib) }}</span>
          </div>
          <div v-if="lib.SourceType !== 'postgres_r18'" class="proxy-toggles-wrap">
            <label class="proxy-toggle">
              <input
                type="checkbox"
                :checked="lib.ProxyEnabled === true"
                :disabled="updatingProxy.has(lib.Id)"
                :aria-label="`${lib.Name} 播放代理`"
                @change="toggleProxy(lib, $event)"
              >
              <span>播放代理</span>
            </label>
            <label class="proxy-toggle">
              <input
                type="checkbox"
                :checked="lib.ResolveRedirects === true"
                :disabled="updatingProxy.has(lib.Id)"
                :aria-label="`${lib.Name} 解析直链`"
                @change="toggleProxy(lib, $event, 'ResolveRedirects')"
              >
              <span>解析直链</span>
            </label>
          </div>
        </div>

        <div class="lib-card-foot">
          <RouterLink class="btn btn-sm btn-browse" :to="browseLink(lib)">
            <Folder :size="13" aria-hidden="true" />
            <span>浏览</span>
          </RouterLink>
          <button
            v-if="lib.SourceType !== 'postgres_r18'"
            class="btn btn-sm btn-random-play"
            :data-lib-random-play="lib.Id"
            title="随机播放该媒体库中的影片"
            :disabled="playingLibraryId === lib.Id"
            @click="playRandom(lib)"
          >
            <Shuffle :size="13" aria-hidden="true" />
            <span>{{ playingLibraryId === lib.Id ? '选取中…' : '随机播放' }}</span>
          </button>
          <button
            v-if="lib.SourceType !== 'postgres_r18'"
            class="btn btn-sm"
            :data-lib-scan="lib.Id"
            title="更新该媒体库中新增或变化的媒体"
            :disabled="tasks.mutationBusy"
            @click="tasks.run('scan', lib.Id)"
          >
            <RefreshCw :size="13" aria-hidden="true" />
            <span>增量扫描</span>
          </button>
          <button
            v-if="lib.SourceType !== 'postgres_r18'"
            class="btn btn-sm"
            :data-lib-duplicates="lib.Id"
            title="检测重复媒体"
            @click="duplicates?.scan(lib.Id)"
          >
            <span>查重</span>
          </button>
          <button v-if="lib.SourceType === 'postgres_r18'" class="btn btn-sm" @click="editingR18 = lib.Id">连接配置</button>
          <button
            class="icon-btn danger"
            :data-lib-delete="lib.Id"
            title="删除媒体库索引（不删文件）"
            :disabled="deleting.has(lib.Id)"
            @click="remove(lib.Id)"
          >
            <Trash2 :size="14" aria-hidden="true" />
          </button>
        </div>
      </article>
    </div>

    <!-- 表格列表视图 -->
    <div v-else-if="data?.items?.length" class="table-wrap library-table-wrap">
      <table class="library-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>内容类型</th>
            <th>路径</th>
            <th>播放设置</th>
            <th class="lib-id">ID</th>
            <th style="text-align:right">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="lib in data.items" :key="lib.Id">
            <td>
              <div class="lib-title-cell">
                <strong class="title">{{ lib.Name }}</strong>
              </div>
            </td>
            <td>
              <span class="lib-type-pill" :class="lib.Type">
                <Tv v-if="lib.Type === 'tvshows'" :size="12" aria-hidden="true" />
                <Layers v-else-if="lib.Type === 'mixed'" :size="12" aria-hidden="true" />
                <Film v-else :size="12" aria-hidden="true" />
                <span>{{ contentLabel(lib) }}</span>
              </span>
            </td>
            <td class="mono">
              <span class="lib-path" :title="sourceLabel(lib)">{{ sourceLabel(lib) }}</span>
            </td>
            <td>
              <div v-if="lib.SourceType !== 'postgres_r18'" class="proxy-toggles-wrap">
                <label class="proxy-toggle">
                  <input
                    type="checkbox"
                    :checked="lib.ProxyEnabled === true"
                    :disabled="updatingProxy.has(lib.Id)"
                    :aria-label="`${lib.Name} 播放代理`"
                    @change="toggleProxy(lib, $event)"
                  >
                  <span>播放代理</span>
                </label>
                <label class="proxy-toggle">
                  <input
                    type="checkbox"
                    :checked="lib.ResolveRedirects === true"
                    :disabled="updatingProxy.has(lib.Id)"
                    :aria-label="`${lib.Name} 解析直链`"
                    @change="toggleProxy(lib, $event, 'ResolveRedirects')"
                  >
                  <span>解析直链</span>
                </label>
              </div>
            </td>
            <td class="num lib-id">
              <span class="id-badge">#{{ lib.Id }}</span>
            </td>
            <td>
              <div class="row-actions">
                <RouterLink class="btn btn-sm btn-browse" :to="browseLink(lib)">
                  <Folder :size="13" aria-hidden="true" />
                  <span>浏览</span>
                </RouterLink>
                <button
                  v-if="lib.SourceType !== 'postgres_r18'"
                  class="btn btn-sm btn-random-play"
                  :data-lib-random-play="lib.Id"
                  title="随机播放该媒体库中的影片"
                  :disabled="playingLibraryId === lib.Id"
                  @click="playRandom(lib)"
                >
                  <Shuffle :size="13" aria-hidden="true" />
                  <span>{{ playingLibraryId === lib.Id ? '选取中…' : '随机播放' }}</span>
                </button>
                <button
                  v-if="lib.SourceType !== 'postgres_r18'"
                  class="btn btn-sm"
                  :data-lib-scan="lib.Id"
                  title="更新该媒体库中新增或变化的媒体"
                  :disabled="tasks.mutationBusy"
                  @click="tasks.run('scan', lib.Id)"
                >
                  <RefreshCw :size="13" aria-hidden="true" />
                  <span>增量扫描</span>
                </button>
                <button
                  v-if="lib.SourceType !== 'postgres_r18'"
                  class="btn btn-sm"
                  :data-lib-duplicates="lib.Id"
                  title="检测重复媒体"
                  @click="duplicates?.scan(lib.Id)"
                >
                  <span>查重</span>
                </button>
                <button v-if="lib.SourceType === 'postgres_r18'" class="btn btn-sm" @click="editingR18 = lib.Id">连接配置</button>
                <button
                  class="icon-btn danger"
                  :data-lib-delete="lib.Id"
                  title="删除媒体库索引（不删文件）"
                  :disabled="deleting.has(lib.Id)"
                  @click="remove(lib.Id)"
                >
                  <Trash2 :size="14" aria-hidden="true" />
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <EmptyState v-else-if="data" title="还没有媒体库" hint="先在下方登记一个存放 .strm 的目录。" />
  </section>

  <DuplicateMoviesPanel ref="duplicates" :libraries="(data?.items || []).filter(lib => lib.SourceType !== 'postgres_r18')" />

  <section v-if="editingR18" class="panel"><div class="panel-head"><h2>R18 连接配置</h2></div><R18LibraryForm :key="editingR18" :library-id="editingR18" @saved="r18Saved" @cancel="editingR18 = undefined" /></section>

  <section class="panel library-add-panel">
    <div class="panel-head">
      <h2>添加媒体库</h2>
      <span class="hint" style="margin:0">登记本地或挂载的 .strm 视频目录</span>
    </div>
    <LibraryForm :key="formKey" :submitting="submitting" @submit="create" />
  </section>
  <section v-if="!editingR18" class="panel"><details><summary>添加 PostgreSQL / R18 资料库</summary><R18LibraryForm :key="r18FormKey" @saved="r18Saved" /></details></section>
</template>

<style scoped>
.panel-head-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.panel-head-title .panel-icon {
  color: var(--accent);
}

.panel-head-title h2 {
  margin: 0;
}

.library-head-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.view-mode-toggle {
  display: inline-flex;
  align-items: center;
  background: var(--panel-2);
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 2px;
  gap: 2px;
}

.view-mode-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  border: 0;
  border-radius: 4px;
  background: transparent;
  color: var(--dim);
  font-size: 11.5px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
}

.view-mode-btn:hover {
  color: var(--text);
}

.view-mode-btn.is-active {
  background: var(--accent);
  color: #ffffff;
  font-weight: 600;
  box-shadow: 0 1px 4px rgba(26, 113, 255, 0.25);
}

/* 卡片网格视图 */
.library-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  gap: 16px;
  margin-top: 14px;
}

.library-card {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 14px;
  padding: 18px 20px;
  background: var(--panel-2);
  border: 1px solid var(--line);
  border-radius: var(--radius);
  transition: border-color 0.18s ease, box-shadow 0.18s ease, transform 0.15s ease;
}

.library-card:hover {
  border-color: var(--line-strong);
  box-shadow: var(--shadow);
  transform: translateY(-2px);
}

.lib-card-head {
  display: flex;
  align-items: flex-start;
  gap: 14px;
}

.lib-card-icon {
  width: 42px;
  height: 42px;
  border-radius: 10px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  background: var(--panel);
  border: 1px solid var(--line);
  color: var(--accent);
}

.lib-card-icon.tvshows {
  color: #8b73ff;
  background: rgba(117, 92, 255, 0.1);
  border-color: rgba(117, 92, 255, 0.25);
}

.lib-card-icon.mixed {
  color: var(--ok);
  background: rgba(32, 201, 151, 0.1);
  border-color: rgba(32, 201, 151, 0.25);
}

.lib-card-icon.movies {
  color: var(--accent);
  background: rgba(43, 122, 254, 0.1);
  border-color: rgba(43, 122, 254, 0.25);
}

.lib-card-title-group {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.lib-card-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.lib-card-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lib-card-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.lib-card-path {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 12px;
  color: var(--muted);
  background: var(--panel);
  padding: 6px 10px;
  border-radius: 6px;
  border: 1px solid var(--line);
}

.lib-path-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--dim);
  text-transform: uppercase;
  flex-shrink: 0;
}

.lib-path-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  word-break: break-all;
}

.lib-card-foot {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding-top: 10px;
  border-top: 1px solid var(--line);
}

.library-table-wrap {
  margin-top: 10px;
}

.lib-title-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.lib-type-pill {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11.5px;
  font-weight: 600;
  background: var(--panel-2);
  border: 1px solid var(--line);
  color: var(--muted);
}

.lib-type-pill.tvshows {
  background: rgba(117, 92, 255, 0.12);
  border-color: rgba(117, 92, 255, 0.28);
  color: #8b73ff;
}

.lib-type-pill.mixed {
  background: rgba(32, 201, 151, 0.12);
  border-color: rgba(32, 201, 151, 0.28);
  color: var(--ok);
}

.lib-type-pill.movies {
  background: rgba(43, 122, 254, 0.12);
  border-color: rgba(43, 122, 254, 0.28);
  color: var(--accent);
}

.proxy-toggles-wrap {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.proxy-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--dim);
  cursor: pointer;
  user-select: none;
}

.proxy-toggle:hover {
  color: var(--text);
}

.proxy-toggle input {
  width: 15px;
  height: 15px;
  margin: 0;
  accent-color: var(--accent);
  cursor: pointer;
}

.id-badge {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--dim);
  background: var(--panel-2);
  padding: 2px 6px;
  border-radius: 4px;
  border: 1px solid var(--line);
}

.btn-browse {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  background: var(--accent-soft);
  border-color: rgba(26, 113, 255, 0.3);
  color: var(--accent);
  font-weight: 600;
}

.btn-browse:hover {
  background: var(--accent);
  color: #ffffff;
}

.btn-random-play {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}
</style>
