import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import ActorsPage from '../src/pages/ActorsPage.vue';
import ActorSettingsForm from '../src/components/scrape/ActorSettingsForm.vue';
import ScrapeSettingsForm from '../src/components/scrape/ScrapeSettingsForm.vue';
import { actorsApi, parseActors, type ActorsResult } from '../src/api/actors';
import { scrapeApi, parseScrapeSettings } from '../src/api/scrape';
import { adminApi } from '../src/api/admin';
import { backgroundApi, parseBackgroundProgress } from '../src/api/background';
import fixture from '../../vue-migration/fixtures/json/scrape-settings.json';

let pinia: Pinia;
const cleanup: (() => void)[] = [];
const settings = parseScrapeSettings({...fixture, avatar_source:'gfriends', avatar_configured:true});
const inventory: ActorsResult = {items:[{name:'Example',movie_count:2,has_image:false,image_tag:'',favorite:false}],total:1,stats:{total:1,missing:1,ready:0}};
beforeEach(() => {
  pinia=createPinia(); setActivePinia(pinia);
  vi.spyOn(scrapeApi,'settings').mockResolvedValue(settings);
  vi.spyOn(adminApi,'libraries').mockResolvedValue({items:[{Id:7,Name:'Owned library',Path:'private'}],total:1});
});
afterEach(() => { cleanup.splice(0).forEach(fn => fn()); disposePinia(pinia); vi.restoreAllMocks(); });
function page() { const w=mount(ActorsPage,{global:{plugins:[pinia],stubs:{RouterLink:true}}}); cleanup.push(() => w.unmount()); return w; }
describe('actor management', () => {
  it('rejects untrusted portrait states and counts', () => {
    expect(parseActors(inventory)).toEqual(inventory);
    expect(() => parseActors({...inventory,items:[{...inventory.items[0],has_image:'true'}]})).toThrow();
    expect(() => parseActors({...inventory,stats:{total:1,ready:0,missing:-1}})).toThrow();
  });
  it('saves only the fields owned by each page, preserving the other page settings', async () => {
    for (const [component, keys] of [[ActorSettingsForm,['avatar_source','avatars_dir','gfriends']], [ScrapeSettingsForm,['metatube_url','metatube_token','timeout_seconds','concurrency','image_quality','download_images','overwrite','translate']]] as const) {
      const w=mount(component as typeof ActorSettingsForm,{props:{settings,saving:false,testing:new Set<string>()},global:{stubs:{RouterLink:true}}});
      await w.get('form').trigger('submit');
      expect(Object.keys(w.emitted('save')![0]![0] as object).sort()).toEqual([...keys].sort()); w.unmount();
    }
  });
  it('batch uses the whole selected library regardless of search and refuses unsaved settings', async () => {
    vi.spyOn(actorsApi,'list').mockResolvedValue(inventory);
    const start=vi.spyOn(backgroundApi,'start').mockResolvedValue({total:1,message:''});
    vi.spyOn(backgroundApi,'progress').mockResolvedValue(parseBackgroundProgress({running:false,total:1,done:1,success:1,skipped:0,failed:0,current:'',kind:'scrape_avatars',finished_at:'now'}));
    const w=page(); await flushPromises();
    await w.get('#actor-library').setValue('7'); await w.get('#actor-status').setValue('missing');
    await w.get('#actor-search').setValue('different'); await w.get('.actor-filters').trigger('submit'); await flushPromises();
    await w.get('#gf-index').setValue('https://mirror.invalid/index.json');
    expect(w.get('#avatar-start').attributes('disabled')).toBeDefined();
    await w.get('#scrape-restore').trigger('click'); await w.get('#avatar-start').trigger('click'); await flushPromises();
    expect(start.mock.calls[0]?.slice(0,3)).toEqual(['scrape',true,{library_id:7}]);
    expect(start).toHaveBeenCalledTimes(1);
  });
  it('discards stale library reads, paginates on the server, and keeps read failures retryable', async () => {
    let resolve!:(value:ActorsResult)=>void;
    const late=new Promise<ActorsResult>(done => {resolve=done;});
    const list=vi.spyOn(actorsApi,'list').mockReturnValueOnce(late).mockResolvedValue({...inventory,total:80});
    const w=page(); await flushPromises(); await w.get('#actor-library').setValue('7'); await flushPromises();
    expect(list.mock.calls[0]![1]!.aborted).toBe(true);
    resolve({...inventory,items:[],total:0}); await flushPromises(); expect(w.findAll('.actor-card')).toHaveLength(1);
    await w.get('.actor-pagination button:last-child').trigger('click'); await flushPromises();
    expect(list.mock.calls.at(-1)![0]).toMatchObject({library_id:7,offset:48});
    list.mockRejectedValueOnce(new Error('offline')); await w.get('#actor-status').setValue('ready'); await flushPromises();
    expect(w.get('[role=alert]').text()).toContain('offline'); expect(w.get('#avatar-start').attributes('disabled')).toBeDefined();
    await w.get('[role=alert] button').trigger('click'); await flushPromises();
    expect(w.find('[role=alert]').exists()).toBe(false); expect(list.mock.calls.at(-1)![0].offset).toBe(0);
  });
});
