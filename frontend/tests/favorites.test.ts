import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { createMemoryHistory, createRouter } from 'vue-router';
import FavoriteButton from '../src/components/media/FavoriteButton.vue';
import FavoritesPage from '../src/pages/FavoritesPage.vue';
import { favoriteDestination, favoriteEntityId, favoritesApi, parseFavoriteItem, type FavoriteItem } from '../src/api/favorites';
import { apiClient } from '../src/api/client';
import { useToastStore } from '../src/stores/toasts';
import { useAuthStore } from '../src/stores/auth';
import authFixture from '../../vue-migration/fixtures/json/auth-login.json';
import { parseUser } from '../src/api/contracts';

let pinia: Pinia;
const wrappers: VueWrapper[] = [];
const item: FavoriteItem = { Id: '5', Name: 'Movie', Type: 'Movie', ImageTags: {}, UserData: { IsFavorite: true }, ParentId: '', SeriesId: '' };
beforeEach(() => { pinia = createPinia(); setActivePinia(pinia); useAuthStore(pinia).user = parseUser({ ...authFixture.User, Id: '2' }); });
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); disposePinia(pinia); vi.restoreAllMocks(); });
function button() {
  const w = mount(FavoriteButton, { props: { itemId: '5', favorite: false, label: 'Movie' }, global: { plugins: [pinia] } });
  wrappers.push(w); return w;
}
async function page(path = '/favorites') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/favorites', component: FavoritesPage }, { path: '/items', component: { template: '<div />' } }, { path: '/item/:id', component: { template: '<div />' } }] });
  await router.push(path); await router.isReady();
  const w = mount(FavoritesPage, { global: { plugins: [pinia, router] } }); wrappers.push(w);
  await flushPromises(); return { w, router };
}

describe('favorite controls and API', () => {
  it('uses the Emby mutation contract and validates returned identities', async () => {
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ ItemId: 'person:a/b', IsFavorite: true });
    expect(await favoritesApi.set('person:a/b', true)).toBe(true);
    expect(request).toHaveBeenLastCalledWith('/Users/2/FavoriteItems/person%3Aa%2Fb', { auth: 'required', method: 'POST' });
    request.mockResolvedValueOnce({ ItemId: 'person:a/b', IsFavorite: false });
    expect(await favoritesApi.set('person:a/b', false)).toBe(false);
    expect(request.mock.lastCall?.[1]?.method).toBe('DELETE');
    request.mockResolvedValueOnce({ ItemId: 'wrong', IsFavorite: true });
    await expect(favoritesApi.set('5', true)).rejects.toThrow();
    expect(() => parseFavoriteItem({ ...item, UserData: { IsFavorite: 'true' } })).toThrow();
  });
  it('sends positive favorite filtering, item types and server paging', async () => {
    const request = vi.spyOn(apiClient, 'request').mockResolvedValue({ Items: [item], TotalRecordCount: 49 });
    expect(await favoritesApi.list('Movie,Episode', 'test', 48)).toEqual({ items: [item], total: 49 });
    const query = new URL(request.mock.lastCall![0], 'http://localhost').searchParams;
    expect(Object.fromEntries(query)).toMatchObject({ Filters: 'IsFavorite', IncludeItemTypes: 'Movie,Episode', StartIndex: '48', Limit: '48', SearchTerm: 'test' });
  });
  it('guards duplicate clicks and emits the server state only after success', async () => {
    let finish!: (value: boolean) => void;
    const set = vi.spyOn(favoritesApi, 'set').mockReturnValue(new Promise(resolve => { finish = resolve; }));
    const w = button();
    await w.trigger('click'); await w.trigger('click');
    expect(set).toHaveBeenCalledExactlyOnceWith('5', true);
    expect(w.attributes('disabled')).toBeDefined(); expect(w.attributes('aria-pressed')).toBe('false');
    finish(true); await flushPromises();
    expect(w.emitted('changed')).toEqual([[true]]); expect(w.attributes('aria-pressed')).toBe('true');
    set.mockResolvedValueOnce(false); await w.trigger('click'); await flushPromises();
    expect(set).toHaveBeenLastCalledWith('5', false); expect(w.attributes('aria-pressed')).toBe('false');
  });
  it('keeps failures retryable and ignores stale responses after navigation or unmount', async () => {
    const set = vi.spyOn(favoritesApi, 'set').mockRejectedValueOnce(new Error('offline'));
    const w = button(); await w.trigger('click'); await flushPromises();
    expect(w.attributes('aria-pressed')).toBe('false'); expect(w.attributes('disabled')).toBeUndefined();
    expect(useToastStore().items.at(-1)?.message).toBe('offline');
    let finish!: (value: boolean) => void;
    set.mockReturnValueOnce(new Promise(resolve => { finish = resolve; }));
    await w.trigger('click'); await w.setProps({ itemId: '6' }); finish(true); await flushPromises();
    expect(w.emitted('changed')).toBeUndefined(); expect(w.attributes('aria-pressed')).toBe('false');
    set.mockReturnValueOnce(new Promise(resolve => { finish = resolve; }));
    await w.trigger('click'); w.unmount(); finish(true); await flushPromises();
    expect(w.emitted('changed')).toBeUndefined();
  });
  it('routes media, anime, television and named entities to usable views', () => {
    expect(favoriteDestination(item)).toEqual({ path: '/item/5' });
    expect(favoriteDestination({ ...item, Id: 'anime-season:10:1', Type: 'Season', SeriesId: 'anime-series:10', bangumiSubjectId: '101' })).toEqual({ path: '/anime/subjects/101' });
    expect(favoriteDestination({ ...item, Id: 'tv-season:2:1:L3Nob3c', Type: 'Season', SeriesId: 'tv-series:2:L3Nob3c' })).toMatchObject({ path: '/items', query: { library_id: '2', tv_season: 'tv-season:2:1:L3Nob3c' } });
    for (const [Type, key] of [['Person', 'person'], ['Genre', 'genre'], ['Tag', 'tag'], ['Studio', 'studio'], ['BoxSet', 'collection']]) {
      expect(favoriteDestination({ ...item, Type: Type!, Id: favoriteEntityId(key!, 'Name'), Name: 'Name' })).toEqual({ path: '/items', query: { [key!]: 'Name' } });
    }
  });
  it('resolves anime episode links with external series ids and shares parent reads', async () => {
    const request = vi.spyOn(apiClient, 'request').mockResolvedValueOnce({ Items: [
      { ...item, Id: 'anime-episode:1', Type: 'Episode', SeriesId: 'anime-series:10', ProviderIds: { Bangumi: '201' } },
      { ...item, Id: 'anime-episode:2', Type: 'Episode', SeriesId: 'anime-series:10', ProviderIds: { Bangumi: '202' } },
    ], TotalRecordCount: 2 }).mockResolvedValueOnce({ ...item, Id: 'anime-series:10', Type: 'Series', ProviderIds: { Bangumi: '101' } });
    const result = await favoritesApi.list('Episode', '', 0);
    expect(request).toHaveBeenCalledTimes(2);
    expect(request.mock.lastCall?.[0]).toBe('/Items/anime-series%3A10');
    for (const episode of result.items) expect(favoriteDestination(episode)).toEqual({ path: '/anime/subjects/101' });
    const film = parseFavoriteItem({ ...item, Id: 'anime-movie:11', ProviderIds: { Bangumi: '102' } });
    expect(favoriteDestination(film)).toEqual({ path: '/anime/subjects/102' });
  });
});

describe('favorites page', () => {
  it('changes tabs, searches, paginates and goes back when the last item is removed', async () => {
    const list = vi.spyOn(favoritesApi, 'list').mockResolvedValue({ items: [item], total: 49 });
    vi.spyOn(favoritesApi, 'set').mockResolvedValue(false);
    const { w, router } = await page();
    expect(w.find('a button').exists()).toBe(false);
    await w.get('[aria-label="下一页"]').trigger('click'); await flushPromises();
    expect(list.mock.lastCall?.slice(0, 3)).toEqual(['Movie,Episode', '', 48]);
    list.mockResolvedValueOnce({ items: [], total: 48 }).mockResolvedValue({ items: [item], total: 48 });
    await w.get('.favorite-button').trigger('click'); await flushPromises();
    expect(router.currentRoute.value.query.offset).toBe('0');
    await w.get('input').setValue('actor'); await w.get('form').trigger('submit'); await flushPromises();
    await w.findAll('[role="tab"]')[2]!.trigger('click'); await flushPromises();
    expect(list.mock.lastCall?.slice(0, 3)).toEqual(['Person', 'actor', 0]);
  });
  it('discards old tab responses and retries read failures', async () => {
    let finish!: (value: { items: FavoriteItem[]; total: number }) => void;
    const list = vi.spyOn(favoritesApi, 'list').mockReturnValueOnce(new Promise(resolve => { finish = resolve; })).mockResolvedValue({ items: [], total: 0 });
    const { w } = await page();
    await w.findAll('[role="tab"]')[3]!.trigger('click'); await flushPromises();
    expect(list.mock.calls[0]?.[3]?.aborted).toBe(true);
    finish({ items: [item], total: 1 }); await flushPromises(); expect(w.find('.favorite-card').exists()).toBe(false);
    list.mockRejectedValueOnce(new Error('offline')); await w.get('[aria-label="刷新收藏"]').trigger('click'); await flushPromises();
    expect(w.get('[role="alert"]').text()).toContain('offline');
    await w.get('[role="alert"] button').trigger('click'); await flushPromises(); expect(w.find('[role="alert"]').exists()).toBe(false);
  });
});
