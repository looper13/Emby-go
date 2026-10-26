import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils';
import { createPinia, disposePinia } from 'pinia';
import TelevisionBrowser from '../src/components/media/TelevisionBrowser.vue';
import LibraryForm from '../src/components/libraries/LibraryForm.vue';
import { parseLibraries } from '../src/api/contracts';
import { televisionApi, parseTelevision, type TelevisionItem } from '../src/api/television';
import { applyMediaVisibility } from '../src/composables/useMediaVisibility';

const wrappers: VueWrapper[] = [];
const stores: ReturnType<typeof createPinia>[] = [];
afterEach(() => { for (const wrapper of wrappers.splice(0)) wrapper.unmount(); stores.splice(0).forEach(disposePinia); vi.restoreAllMocks(); applyMediaVisibility(false); });
const series: TelevisionItem = { Id:'tv-series:2:L3Nob3c',Name:'Drama',Type:'Series',ImageTags:{Primary:'cover'},ChildCount:2,RecursiveItemCount:3,IndexNumber:0,SeriesName:'',ProductionYear:2026,Overview:'' };
const season: TelevisionItem = { ...series,Id:'tv-season:2:1:L3Nob3c',Name:'第 1 季',Type:'Season',IndexNumber:1 };
const episode: TelevisionItem = { ...series,Id:'5',Name:'Pilot',Type:'Episode',SeriesName:'Drama',IndexNumber:1 };
function browser(library=2, context: { series?: string; season?: string } = {}) { const pinia=createPinia(); stores.push(pinia); const w=mount(TelevisionBrowser,{props:{library,search:'',revision:0,...context},global:{plugins:[pinia]}}); wrappers.push(w); return w; }
describe('library types and television browsing', () => {
  it('preserves the content type contract and rejects unknown values', () => {
    for (const Type of ['movies','tvshows','mixed','']) expect(parseLibraries({items:[{Id:1,Name:'Library',Path:'/media',Type}],total:1}).items?.[0]?.Type).toBe(Type);
    expect(() => parseLibraries({items:[{Id:1,Name:'Library',Path:'/media',Type:'music'}],total:1})).toThrow();
    expect(() => parseTelevision({Items:[{...series,Type:'Movie'}],TotalRecordCount:1})).toThrow();
  });
  it('submits all three content types and locks inputs while creating', async () => {
    const w=mount(LibraryForm,{props:{submitting:false}}); wrappers.push(w);
    await w.get('#lib-name').setValue('Library'); await w.get('#lib-path').setValue('/media');
    for (const Type of ['movies','tvshows','mixed']) { await w.get('#lib-type').setValue(Type); await w.get('form').trigger('submit'); expect(w.emitted('submit')?.at(-1)?.[0]).toEqual({Name:'Library',Path:'/media',Type,ProxyEnabled:false,ResolveRedirects:false}); }
    await w.setProps({submitting:true}); expect(w.get('#lib-type').attributes('disabled')).toBeDefined(); await w.get('form').trigger('submit'); expect(w.emitted('submit')).toHaveLength(3);
  });
  it('navigates series and seasons into the existing numeric episode detail', async () => {
    vi.spyOn(televisionApi,'list').mockResolvedValue({items:[series],total:1}); vi.spyOn(televisionApi,'series').mockResolvedValue(series); vi.spyOn(televisionApi,'seasons').mockResolvedValue({items:[season],total:1}); const read=vi.spyOn(televisionApi,'episodes').mockResolvedValue({items:[episode],total:1});
    const w=browser(); await flushPromises(); expect(w.text()).toContain('Drama'); await w.get(`[data-tv-id="${series.Id}"]`).trigger('click'); await flushPromises();
    expect(w.emitted('browse')?.[0]).toEqual([series.Id,'']); await w.setProps({series:series.Id}); await flushPromises();
    expect(read.mock.calls[0]?.slice(0,3)).toEqual([series.Id,season.Id,0]); expect(w.text()).toContain('1. Pilot'); await w.get('[data-tv-id="5"]').trigger('click'); expect(w.emitted('open')?.[0]).toEqual([5]);
    await w.get('button[title="返回电视节目"]').trigger('click'); expect(w.emitted('browse')?.at(-1)).toEqual(['','']); await w.setProps({series:''}); await flushPromises(); expect(w.text()).not.toContain('Pilot'); expect(w.text()).toContain('Drama');
  });
  it('uses episode Thumb when Primary is missing, preserving Primary priority and image visibility', async () => {
    vi.spyOn(televisionApi, 'series').mockResolvedValue(series);
    vi.spyOn(televisionApi, 'seasons').mockResolvedValue({ items: [season], total: 1 });
    vi.spyOn(televisionApi, 'episodes').mockResolvedValue({ items: [
      { ...episode, ImageTags: { Thumb: 'episode-thumb' } },
      { ...episode, Id: '6', ImageTags: { Primary: 'episode-poster', Thumb: 'other-thumb' } },
      { ...episode, Id: '7', ImageTags: {} },
    ], total: 3 });
    const w = browser(2, { series: series.Id }); await flushPromises();
    expect(w.get('[data-tv-id="5"] img').attributes('src')).toBe('/Items/5/Images/Thumb?tag=episode-thumb&maxWidth=320');
    expect(w.get('[data-tv-id="6"] img').attributes('src')).toBe('/Items/6/Images/Primary?tag=episode-poster&maxWidth=320');
    expect(w.get('[data-tv-id="7"] .image-placeholder').text()).toBe('Pilot');
    await w.get('[data-tv-id="5"]').trigger('click'); expect(w.emitted('open')?.[0]).toEqual([5]);
    applyMediaVisibility(true); await flushPromises();
    expect(w.findAll('.television-cover img')).toHaveLength(0);
    expect(w.findAll('.television-cover .image-placeholder')).toHaveLength(3);
    applyMediaVisibility(false); await flushPromises();
    expect(w.findAll('.television-cover img')).toHaveLength(2);
  });
  it('does not use Thumb as a series poster', async () => {
    vi.spyOn(televisionApi, 'list').mockResolvedValue({ items: [{ ...series, ImageTags: { Thumb: 'series-thumb' } }], total: 1 });
    const w = browser(); await flushPromises();
    expect(w.find('.television-cover img').exists()).toBe(false);
    expect(w.get('.image-placeholder').text()).toBe('Drama');
  });
  it('discards stale library reads and clears old results on errors', async () => {
    let resolve!: (value:{items:TelevisionItem[];total:number})=>void;
    const list=vi.spyOn(televisionApi,'list').mockReturnValueOnce(new Promise(done=>{resolve=done;})).mockResolvedValueOnce({items:[{...series,Name:'Other'}],total:1});
    const w=browser(); await w.setProps({library:3}); await flushPromises(); resolve({items:[series],total:1}); await flushPromises(); expect(w.text()).toContain('Other'); expect(w.text()).not.toContain('Drama');
    list.mockRejectedValueOnce(new Error('read failed')); await w.setProps({revision:1}); await flushPromises(); expect(w.text()).toContain('read failed'); expect(w.find('[data-tv-id]').exists()).toBe(false);
  });
  it('paginates without replacing already loaded series', async () => {
    const list=vi.spyOn(televisionApi,'list').mockResolvedValueOnce({items:[series],total:2}).mockResolvedValueOnce({items:[{...series,Id:'tv-series:2:b3RoZXI',Name:'Second'}],total:2});
    const w=browser(); await flushPromises(); await w.get('button.btn').trigger('click'); await flushPromises(); expect(list.mock.calls[1]?.slice(0,3)).toEqual([2,'',1]); expect(w.findAll('[data-tv-id]')).toHaveLength(2);
  });
  it('restores the selected season and prefers main seasons to specials on first entry', async () => {
    const special = {...season,Id:'tv-season:2:0:L3Nob3c',Name:'特别篇',IndexNumber:0};
    const second = {...season,Id:'tv-season:2:2:L3Nob3c',Name:'第 2 季',IndexNumber:2};
    vi.spyOn(televisionApi,'series').mockResolvedValue(series);
    vi.spyOn(televisionApi,'seasons').mockResolvedValue({items:[special,season,second],total:3});
    const read = vi.spyOn(televisionApi,'episodes').mockResolvedValue({items:[episode],total:1});
    const w = browser(2,{series:series.Id,season:second.Id}); await flushPromises();
    expect(read.mock.lastCall?.slice(0,3)).toEqual([series.Id,second.Id,0]);
    expect(w.get('[role="tab"][aria-selected="true"]').text()).toBe('第 2 季');
    await w.setProps({season:''}); await flushPromises();
    expect(read.mock.lastCall?.[1]).toBe(season.Id);
    await w.get('[role="tab"]').trigger('click'); expect(w.emitted('browse')?.at(-1)).toEqual([series.Id,special.Id]);
  });
  it('retries a failed page at its original offset without discarding earlier pages', async () => {
    const second = {...series,Id:'tv-series:2:b3RoZXI',Name:'Second'};
    const list=vi.spyOn(televisionApi,'list').mockResolvedValueOnce({items:[series],total:2})
      .mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({items:[second],total:2});
    const w=browser(); await flushPromises(); await w.get('button.btn').trigger('click'); await flushPromises();
    expect(w.text()).toContain('offline'); expect(w.findAll('[data-tv-id]')).toHaveLength(1);
    await w.get('[role="alert"] button').trigger('click'); await flushPromises();
    expect(list.mock.calls.map(call=>call[2])).toEqual([0,1,1]);
    expect(w.findAll('[data-tv-id]')).toHaveLength(2);
  });
  it('rejects mismatched item types, malformed folder ids and unsafe episode ids', () => {
    for (const item of [{...episode,Id:series.Id},{...series,Id:'5'},{...season,Id:series.Id},{...series,Id:'tv-series:2:'},{...episode,Id:'9007199254740992'}]) {
      expect(()=>parseTelevision({Items:[item],TotalRecordCount:1})).toThrow();
    }
  });
  it('keeps a return action on missing or cross-library series and never reads the other library', async () => {
    const read = vi.spyOn(televisionApi,'series');
    const w = browser(3,{series:series.Id}); await flushPromises();
    expect(read).not.toHaveBeenCalled(); expect(w.text()).toContain('剧集不属于当前媒体库');
    await w.get('[title="返回电视节目"]').trigger('click'); expect(w.emitted('browse')?.at(-1)).toEqual(['','']);
  });
});
