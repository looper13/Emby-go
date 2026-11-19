<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue';
import { ChevronDown, Film, ExternalLink, List, MessageSquare, Play, RefreshCw } from '@lucide/vue';
import type { LibraryDto } from '../../api/contracts';
import { javdbCommunityApi, type JavdbListMovies, type JavdbMatch, type JavdbPage, type JavdbRelatedList, type JavdbReview } from '../../api/javdb-community';
import { r18Api } from '../../api/r18';
import { usePageRequest } from '../../composables/usePageRequest';
import { useAuthStore } from '../../stores/auth';
import JavdbListFavoriteButton from './JavdbListFavoriteButton.vue';
import R18Cover from './R18Cover.vue';

const props = defineProps<{
  itemId: number; number: string; localLibraryId: number;
  savedList?: { libraryId: number; listId: string; relatedPage: number };
}>();
const emit = defineEmits<{ open: [id: number] }>();
const auth = useAuthStore();
const matchScope = usePageRequest(), reviewScope = usePageRequest(), listScope = usePageRequest();
const movieScope = usePageRequest();
const libraries = shallowRef<LibraryDto[]>([]);
const selectedLibrary = ref(0);
const match = shallowRef<JavdbMatch>();
const phase = ref<'loading' | 'matching' | 'ready' | 'unavailable' | 'error'>('loading');
const problem = ref('');
const tab = ref<'reviews' | 'lists'>('reviews');
const reviews = shallowRef<JavdbPage<JavdbReview>>();
const lists = shallowRef<JavdbPage<JavdbRelatedList>>();
const reviewBusy = ref(false), listBusy = ref(false);
const reviewError = ref(''), listError = ref('');
const reviewAttempt = ref(1), listAttempt = ref(1);
const selectedList = shallowRef<JavdbRelatedList>();
const selectedRelatedPage = ref(1);
const listMovies = shallowRef<JavdbListMovies>();
const localMovieCount = computed(() => listMovies.value?.items.filter(movie => movie.has_local).length ?? 0);
const savedListTarget = computed(() => props.savedList);
const movieBusy = ref(false), movieError = ref(''), movieAttempt = ref(1);
const expanded = ref(new Set<string>());
const matched = computed(() => phase.value === 'ready' && match.value?.status === 'matched');
const busy = computed(() => phase.value === 'loading' || phase.value === 'matching');
const detailLink = computed(() => match.value?.video ? {
  name: 'r18-detail', params: { libraryId: String(match.value.library_id), id: match.value.video.id },
} : undefined);
function message(cause: unknown) { return cause instanceof Error ? cause.message : 'JavDB 资料加载失败'; }
function resetListMovies() {
  movieScope.cancel(); selectedList.value = undefined; listMovies.value = undefined;
  movieBusy.value = false; movieError.value = ''; movieAttempt.value = 1;
}
function resetContent() {
  reviewScope.cancel(); listScope.cancel();
  reviews.value = undefined; lists.value = undefined;
  reviewError.value = ''; listError.value = '';
  reviewBusy.value = false; listBusy.value = false;
  reviewAttempt.value = 1; listAttempt.value = 1;
  expanded.value = new Set();
  resetListMovies();
  tab.value = 'reviews'; match.value = undefined;
}
async function loadMatch(current: ReturnType<typeof matchScope.begin>, refresh = false) {
  phase.value = 'matching'; problem.value = '';
  try {
    const result = await javdbCommunityApi.match(props.itemId, selectedLibrary.value, current.signal, refresh);
    if (!current.isCurrent()) return;
    match.value = result; phase.value = 'ready';
    if (result.status === 'matched') {
      const target = savedListTarget.value;
      if (target && target.libraryId === result.library_id) { tab.value = 'lists'; void loadLists(target.relatedPage); }
      else void loadReviews();
    }
  } catch (cause) {
    if (current.isCurrent()) { problem.value = message(cause); phase.value = 'error'; }
  }
}
async function initialize() {
  const current = matchScope.begin();
  resetContent(); problem.value = ''; phase.value = 'loading';
  if (!props.number.trim()) { phase.value = 'unavailable'; return; }
  try {
    const result = await r18Api.libraries(current.signal);
    if (!current.isCurrent()) return;
    libraries.value = result.items ?? [];
    if (!libraries.value.length) { selectedLibrary.value = 0; phase.value = 'unavailable'; return; }
    const requestedLibrary = savedListTarget.value?.libraryId;
    if (requestedLibrary && libraries.value.some(library => library.Id === requestedLibrary)) selectedLibrary.value = requestedLibrary;
    else if (!libraries.value.some(library => library.Id === selectedLibrary.value)) selectedLibrary.value = libraries.value[0]!.Id;
    await loadMatch(current);
  } catch (cause) {
    if (current.isCurrent()) { problem.value = message(cause); phase.value = 'error'; }
  }
}
function retryMatch() {
  if (!selectedLibrary.value) { void initialize(); return; }
  const current = matchScope.begin(); resetContent(); void loadMatch(current, true);
}
async function loadReviews(page = 1, refresh = false) {
  const identity = match.value;
  if (!matched.value || !identity) return;
  const current = reviewScope.begin();
  reviewBusy.value = true; reviewError.value = ''; reviewAttempt.value = page;
  try {
    const result = await javdbCommunityApi.reviews(props.itemId, identity.library_id, page, current.signal, refresh);
    if (!current.isCurrent() || match.value !== identity) return;
    if (result.number !== identity.number || result.video_id !== identity.video?.id || result.library_id !== identity.library_id) {
      throw new Error('影片番号或 JavDB 资料来源已更新，请重新匹配后查看评论。');
    }
    reviews.value = result; expanded.value = new Set();
  } catch (cause) {
    if (current.isCurrent() && match.value === identity) reviewError.value = message(cause);
  } finally {
    if (current.isCurrent() && match.value === identity) reviewBusy.value = false;
  }
}
async function loadLists(page = 1, refresh = false) {
  const identity = match.value;
  if (!matched.value || !identity) return;
  const current = listScope.begin();
  listBusy.value = true; listError.value = ''; listAttempt.value = page;
  try {
    const result = await javdbCommunityApi.lists(props.itemId, identity.library_id, page, current.signal, refresh);
    if (!current.isCurrent() || match.value !== identity) return;
    if (result.number !== identity.number || result.video_id !== identity.video?.id || result.library_id !== identity.library_id) {
      throw new Error('影片番号或 JavDB 资料来源已更新，请重新匹配后查看清单。');
    }
    resetListMovies();
    lists.value = result;
    const target = savedListTarget.value;
    if (target && target.libraryId === identity.library_id && target.relatedPage === page) {
      const selected = result.items.find(item => item.id === target.listId);
      if (selected) {
        selectedList.value = selected; selectedRelatedPage.value = result.page;
        void loadListMovies();
      } else movieError.value = '收藏的 AVdb 清单已无法从当前影片打开。';
    }
  } catch (cause) {
    if (current.isCurrent() && match.value === identity) listError.value = message(cause);
  } finally {
    if (current.isCurrent() && match.value === identity) listBusy.value = false;
  }
}
function selectList(list: JavdbRelatedList) {
  if (selectedList.value?.id === list.id) { resetListMovies(); return; }
  resetListMovies(); selectedList.value = list;
  selectedRelatedPage.value = lists.value?.page ?? 1;
  void loadListMovies();
}
async function loadListMovies(page = 1, refresh = false) {
  const identity = match.value, list = selectedList.value, localLibrary = props.localLibraryId;
  if (!matched.value || !identity || !list) return;
  const current = movieScope.begin();
  movieBusy.value = true; movieError.value = ''; movieAttempt.value = page;
  try {
    const result = await javdbCommunityApi.listMovies(props.itemId, identity.library_id, list.id, page, selectedRelatedPage.value, current.signal, refresh);
    if (!current.isCurrent() || match.value !== identity || selectedList.value !== list) return;
    if (result.number !== identity.number || result.video_id !== identity.video?.id || result.library_id !== identity.library_id
      || result.local_library_id !== localLibrary || props.localLibraryId !== localLibrary || result.list_id !== list.id) {
      throw new Error('影片番号、媒体库或清单已更新，请刷新详情后重新匹配。');
    }
    const items = page === 1 ? result.items : [...(listMovies.value?.items ?? []), ...result.items];
    listMovies.value = { ...result, items: [...new Map(items.map(movie => [movie.id, movie])).values()] };
  } catch (cause) {
    if (current.isCurrent() && match.value === identity && selectedList.value === list) movieError.value = message(cause);
  } finally {
    if (current.isCurrent() && match.value === identity && selectedList.value === list) movieBusy.value = false;
  }
}
function selectTab(value: 'reviews' | 'lists') {
  tab.value = value;
  if (value === 'lists' && !lists.value && !listBusy.value && !listError.value) void loadLists();
}
function setListFavoriteState(id: string, favorite: boolean) {
  if (!lists.value) return;
  lists.value = { ...lists.value, items: lists.value.items.map(list => list.id === id ? { ...list, favorite } : list) };
}
function toggleReview(id: string) {
  const next = new Set(expanded.value);
  if (next.has(id)) next.delete(id); else next.add(id);
  expanded.value = next;
}
function reviewBody(review: JavdbReview) {
  return review.content.length > 600 && !expanded.value.has(review.id) ? `${review.content.slice(0, 600)}…` : review.content;
}
watch(() => [props.itemId, props.number, props.localLibraryId, props.savedList?.libraryId, props.savedList?.listId, props.savedList?.relatedPage] as const, () => { void initialize(); }, { immediate: true });
</script>

<template>
  <section v-if="number.trim()" class="panel javdb-community" aria-labelledby="javdb-community-title">
    <div class="panel-head javdb-heading">
      <h2 id="javdb-community-title"><MessageSquare :size="18" aria-hidden="true" /> JavDB 评论与清单</h2>
      <button class="btn javdb-refresh" :disabled="busy" aria-label="重新匹配 JavDB 番号" @click="retryMatch"><RefreshCw :size="15" aria-hidden="true" /> 重新匹配</button>
    </div>
    <div class="javdb-source">
      <span class="mono javdb-number">{{ number }}</span>
      <label v-if="libraries.length > 1" class="javdb-library-choice"><span>资料来源</span><select v-model.number="selectedLibrary" aria-label="JavDB 资料来源" :disabled="busy" @change="retryMatch"><option v-for="library in libraries" :key="library.Id" :value="library.Id">{{ library.Name }}</option></select></label>
      <span v-else-if="libraries.length" class="hint">{{ libraries[0]?.Name }}</span>
      <RouterLink v-if="matched && detailLink" :to="detailLink" class="javdb-detail-link">查看 JavDB 影片资料 <ExternalLink :size="14" aria-hidden="true" /></RouterLink>
    </div>
    <p v-if="busy" class="hint javdb-state" role="status">{{ phase === 'loading' ? '正在读取可访问的 JavDB 资料库…' : '正在按完整番号匹配 JavDB 影片…' }}</p>
    <div v-else-if="phase === 'unavailable'" class="javdb-state"><p class="hint">还没有可访问的 JavDB 资料库。</p><RouterLink v-if="auth.isAdministrator" class="btn" to="/libraries">添加 JavDB 资料库</RouterLink><p v-else class="hint">请联系管理员添加资料库并授权访问。</p></div>
    <div v-else-if="phase === 'error'" class="javdb-state"><p role="alert">{{ problem }}</p><button class="btn" @click="retryMatch">重试匹配</button></div>
    <p v-else-if="match?.status === 'not_found'" class="hint javdb-state">未找到与 {{ number }} 完整匹配的 JavDB 影片，可核对番号后重新匹配。</p>
    <p v-else-if="match?.status === 'no_number'" class="hint javdb-state">当前影片没有可用于匹配的番号。</p>
    <template v-else-if="matched">
      <div class="javdb-tabs" aria-label="JavDB 资料类型"><button class="btn" :class="{ 'is-active': tab === 'reviews' }" :aria-pressed="tab === 'reviews'" @click="selectTab('reviews')"><MessageSquare :size="16" aria-hidden="true" /> 评论<span v-if="reviews?.total !== undefined"> {{ reviews.total }}</span></button><button class="btn" :class="{ 'is-active': tab === 'lists' }" :aria-pressed="tab === 'lists'" @click="selectTab('lists')"><List :size="16" aria-hidden="true" /> 相关清单<span v-if="lists?.total !== undefined"> {{ lists.total }}</span></button></div>
      <p class="hint javdb-help">资料来自 JavDB；加载失败时可检查 AVdb 连接和服务端 JavDB 登录状态。</p>
      <div v-if="tab === 'reviews'" class="javdb-content" :aria-busy="reviewBusy" aria-label="JavDB 评论">
        <div v-if="reviewError" class="javdb-error"><p role="alert">{{ reviewError }}</p><button class="btn" :disabled="reviewBusy" @click="loadReviews(reviewAttempt, true)">重试评论</button></div>
        <p v-if="reviewBusy" class="hint" role="status">正在读取第 {{ reviewAttempt }} 页评论…</p>
        <p v-if="reviews && !reviews.items.length && !reviewBusy && !reviewError" class="hint javdb-state">当前影片暂无评论。</p>
        <div v-if="reviews?.items.length" class="javdb-reviews">
          <article v-for="review in reviews.items" :key="review.id" class="javdb-review">
            <div class="javdb-review-meta"><strong>{{ review.username }}</strong><span v-if="review.score" class="javdb-score">评分 {{ review.score }}</span><span v-if="review.status" class="hint">{{ review.status }}</span><span v-if="review.created_at" class="hint">{{ review.created_at }}</span></div>
            <p v-if="review.content" class="javdb-review-body">{{ reviewBody(review) }}</p>
            <button v-if="review.content.length > 600" class="btn javdb-expand" :aria-expanded="expanded.has(review.id)" @click="toggleReview(review.id)">{{ expanded.has(review.id) ? '收起评论' : '展开完整评论' }}</button>
          </article>
        </div>
        <nav v-if="reviews && (reviews.items.length || reviews.page > 1)" class="javdb-pagination" aria-label="JavDB 评论分页"><button class="btn" :disabled="reviewBusy || reviews.page <= 1" @click="loadReviews(reviews.page - 1)">上一页评论</button><span class="hint">第 {{ reviews.page }} 页<span v-if="reviews.total !== undefined"> · 共 {{ reviews.total }} 条</span></span><button class="btn" :disabled="reviewBusy || !reviews.has_more" @click="loadReviews(reviews.page + 1)">下一页评论</button></nav>
      </div>
      <div v-else class="javdb-content" :aria-busy="listBusy" aria-label="JavDB 相关清单">
        <div v-if="listError" class="javdb-error"><p role="alert">{{ listError }}</p><button class="btn" :disabled="listBusy" @click="loadLists(listAttempt, true)">重试清单</button></div>
        <p v-if="listBusy" class="hint" role="status">正在读取第 {{ listAttempt }} 页清单…</p>
        <p v-if="lists && !lists.items.length && !listBusy && !listError" class="hint javdb-state">当前影片暂无相关清单。</p>
        <div v-if="lists?.items.length" class="javdb-lists"><div v-for="list in lists.items" :key="list.id" class="javdb-list-row">
          <button type="button" class="javdb-list" :class="{ 'is-selected': selectedList?.id === list.id }" :aria-expanded="selectedList?.id === list.id" aria-controls="javdb-list-movies" @click="selectList(list)"><List :size="20" aria-hidden="true" /><span><strong>{{ list.name }}</strong><span class="hint"><span v-if="list.movie_count !== undefined">清单 {{ list.movie_count }} 部 · </span>查看清单影片</span></span><ChevronDown :size="16" aria-hidden="true" /></button>
          <JavdbListFavoriteButton :library-id="lists.library_id" :list-id="list.id" :favorite="list.favorite" :label="list.name" :item-id="itemId" :related-page="lists.page" @changed="setListFavoriteState(list.id, $event)" />
        </div></div>
        <section v-if="selectedList" id="javdb-list-movies" class="javdb-movie-list" :aria-busy="movieBusy" aria-labelledby="javdb-list-movies-title">
          <div class="javdb-movie-heading"><h3 id="javdb-list-movies-title">{{ selectedList.name }}</h3><span v-if="listMovies" class="hint">已加载 {{ listMovies.items.length }} 部 · 本地可播放 {{ localMovieCount }} 部</span></div>
          <div v-if="movieError" class="javdb-error"><p role="alert">{{ movieError }}</p><button class="btn" :disabled="movieBusy" @click="loadListMovies(movieAttempt, true)">重试影片列表</button></div>
          <p v-if="movieBusy" class="hint" role="status">正在读取清单影片…</p>
          <p v-if="listMovies && !listMovies.items.length && !listMovies.has_more && !movieBusy && !movieError" class="hint">这份清单暂无影片。</p>
          <div v-if="listMovies?.items.length" class="javdb-movies">
            <article v-for="movie in listMovies.items" :key="movie.id" class="javdb-movie-card" :data-javdb-movie="movie.id">
              <RouterLink class="javdb-movie-link" :to="{ name: 'r18-detail', params: { libraryId: String(listMovies.library_id), id: movie.id } }" :aria-label="`查看 ${movie.title || movie.number} 的 JavDB 详情`">
                <R18Cover :src="movie.cover_url" :alt="`${movie.title || movie.number} 封面`" loading="lazy"><span class="javdb-local-badge" :class="{ 'is-local': movie.has_local }" :title="movie.has_local ? '本库已有可播放的 STRM 文件' : '本库没有可播放的 STRM 文件'"><Play v-if="movie.has_local" :size="12" aria-hidden="true" />{{ movie.has_local ? '本地可播放' : '无本地 STRM' }}</span></R18Cover>
                <div class="javdb-movie-copy"><span class="mono">{{ movie.number }}</span><h4 :title="movie.title || movie.number">{{ movie.title || movie.number }}</h4></div>
              </RouterLink>
              <div v-if="movie.has_local" class="javdb-local-actions"><button v-for="(local, index) in movie.local_movies" :key="local.id" type="button" class="btn" :data-local-movie="local.id" :aria-label="`查看本库影片 ${local.title || local.number} 的详情`" :title="local.title || local.number" @click="emit('open', local.id)"><Film :size="14" aria-hidden="true" />本库详情<span v-if="movie.local_movies.length > 1"> {{ index + 1 }}</span></button></div>
            </article>
          </div>
          <button v-if="listMovies?.has_more" class="btn javdb-load-more" :disabled="movieBusy" @click="loadListMovies(listMovies.next_page)">加载更多影片</button>
        </section>
        <nav v-if="lists && (lists.items.length || lists.page > 1)" class="javdb-pagination" aria-label="JavDB 清单分页"><button class="btn" :disabled="listBusy || lists.page <= 1" @click="loadLists(lists.page - 1)">上一页清单</button><span class="hint">第 {{ lists.page }} 页<span v-if="lists.total !== undefined"> · 共 {{ lists.total }} 个</span></span><button class="btn" :disabled="listBusy || !lists.has_more" @click="loadLists(lists.page + 1)">下一页清单</button></nav>
      </div>
    </template>
  </section>
</template>

<style scoped>
.javdb-community { min-width: 0; }
.javdb-heading { gap: 12px; flex-wrap: wrap; }
.javdb-heading h2, .javdb-tabs .btn, .javdb-refresh, .javdb-detail-link { display: inline-flex; align-items: center; gap: 8px; }
.javdb-source { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; min-width: 0; margin-bottom: 16px; }
.javdb-number { color: var(--text); overflow-wrap: anywhere; }
.javdb-library-choice { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; min-width: 0; color: var(--muted); }
.javdb-library-choice select { width: min(260px, 100%); min-width: 0; min-height: 44px; padding: 8px 10px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--panel-2); color: var(--text); font: inherit; }
.javdb-detail-link { color: var(--accent); min-height: 44px; overflow-wrap: anywhere; }
.javdb-state { padding: 12px 0; }
.javdb-tabs { display: flex; flex-wrap: wrap; gap: 8px; }
.javdb-tabs .is-active { background: var(--accent-soft); border-color: var(--accent); color: var(--text); }
.javdb-help { margin: 12px 0 16px; line-height: 1.6; }
.javdb-content { min-width: 0; }
.javdb-error { margin-bottom: 16px; color: var(--danger); }
.javdb-error p { white-space: pre-wrap; overflow-wrap: anywhere; }
.javdb-reviews { display: grid; gap: 12px; }
.javdb-review { min-width: 0; padding: 16px; border: 1px solid var(--line); border-radius: var(--radius); background: var(--panel-2); }
.javdb-review-meta { display: flex; flex-wrap: wrap; align-items: baseline; gap: 8px 12px; overflow-wrap: anywhere; }
.javdb-score { color: var(--text); }
.javdb-review-body { max-width: 80ch; margin: 12px 0 0; color: var(--text); font-size: 15px; line-height: 1.75; white-space: pre-wrap; overflow-wrap: anywhere; }
.javdb-expand { margin-top: 12px; }
.javdb-lists { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(320px, 100%), 1fr)); gap: 12px; }
.javdb-list-row { display: flex; align-items: center; gap: 8px; min-width: 0; }
.javdb-list { display: flex; align-items: center; gap: 12px; flex: 1; min-width: 0; width: 100%; min-height: 44px; padding: 16px; border: 1px solid var(--line); border-radius: var(--radius); background: var(--panel-2); color: var(--text); font: inherit; text-align: left; cursor: pointer; }
.javdb-list > span { display: grid; gap: 8px; flex: 1; min-width: 0; }
.javdb-list strong { overflow-wrap: anywhere; line-height: 1.5; }
.javdb-list > svg { flex-shrink: 0; }
.javdb-list:hover { border-color: var(--accent); }
.javdb-list.is-selected { border-color: var(--accent); background: var(--accent-soft); }
.javdb-movie-list { min-width: 0; margin-top: 16px; padding: 16px; border: 1px solid var(--line); border-radius: var(--radius); }
.javdb-movie-heading { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px 16px; margin-bottom: 12px; }
.javdb-movie-heading h3 { margin: 0; font-size: 16px; overflow-wrap: anywhere; }
.javdb-movies { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(var(--wall-card-min-width, 176px), 100%), 1fr)); gap: 16px; }
.javdb-movie-card { display: flex; flex-direction: column; min-width: 0; border: 1px solid var(--line); border-radius: 10px; background: var(--panel-2); }
.javdb-movie-link { display: block; flex: 1; min-width: 0; color: var(--text); text-decoration: none; border-radius: 10px; }
.javdb-movie-link:hover { color: var(--accent); }
.javdb-movie-link :deep(.r18-poster) { border: 0; }
.javdb-local-badge { position: absolute; top: 8px; right: 8px; display: inline-flex; align-items: center; gap: 4px; max-width: calc(100% - 16px); padding: 5px 6px; border: 1px solid var(--line); border-radius: 5px; background: var(--panel); color: var(--muted); font-size: 12px; font-weight: 600; line-height: 1.4; overflow-wrap: anywhere; }
.javdb-local-badge svg { flex-shrink: 0; }
.javdb-local-badge.is-local { border-color: var(--accent); background: var(--accent); color: var(--accent-ink); }
.javdb-movie-copy { display: grid; gap: 6px; min-width: 0; padding: 12px; }
.javdb-movie-copy .mono { color: var(--accent); font-size: 12px; overflow-wrap: anywhere; }
.javdb-movie-copy h4 { margin: 0; font-size: 14px; line-height: 1.6; font-weight: 600; overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.javdb-local-actions { display: flex; flex-wrap: wrap; gap: 8px; padding: 0 12px 12px; }
.javdb-local-actions .btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; min-width: 0; padding: 8px; font-size: 12px; flex: 1; }
.javdb-load-more { margin-top: 16px; }
.javdb-pagination { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; margin-top: 16px; }
.javdb-community .btn { min-height: 44px; }
.javdb-community :is(button, a, select):focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
@media (max-width: 600px) {
  .javdb-heading h2 { font-size: 17px; }
  .javdb-library-choice { flex: 1 0 100%; }
  .javdb-library-choice select { flex: 1; width: auto; }
  .javdb-tabs .btn { flex: 1; justify-content: center; }
  .javdb-review { padding: 12px; }
  .javdb-review-body { font-size: 16px; }
  .javdb-movies { grid-template-columns: repeat(auto-fill, minmax(min(132px, 100%), 1fr)); gap: 12px; }
  .javdb-pagination > .hint { width: 100%; order: -1; text-align: center; }
}
</style>
