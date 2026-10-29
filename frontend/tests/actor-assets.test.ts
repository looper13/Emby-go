import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { parseScrapeSettings, scrapeApi } from '../src/api/scrape';
import { backgroundApi, parseBackgroundProgress } from '../src/api/background';
import { apiClient } from '../src/api/client';
import ActorSettingsForm from '../src/components/scrape/ActorSettingsForm.vue';
import ActorImportPanel from '../src/components/scrape/ActorImportPanel.vue';
import fixture from './fixtures/json/scrape-settings.json';

let pinia: Pinia;
beforeEach(() => { pinia=createPinia();setActivePinia(pinia); });
afterEach(() => { disposePinia(pinia);vi.restoreAllMocks(); });
const settings = () => parseScrapeSettings({...fixture,avatar_source:'gfriends',avatar_configured:true,actor_storage_dir:'D:/data/actors'});
describe('actor assets', () => {
  it('keeps settings local until explicit save and restores nested source configuration', async () => {
    const save=vi.spyOn(scrapeApi,'save');
    const w=mount(ActorSettingsForm,{props:{settings:settings(),saving:false,testing:new Set<string>()}});
    await w.get('#gf-index').setValue('https://mirror.invalid/Filetree.json');
    expect(w.get('#scrape-dirty').text()).toContain('未保存');expect(w.get('#test-gfriends').attributes('disabled')).toBeDefined();expect(save).not.toHaveBeenCalled();
    await w.get('#scrape-restore').trigger('click');expect((w.get('#gf-index').element as HTMLInputElement).value).toContain('raw.githubusercontent.com');
    expect(w.get('#scrape-dirty').text()).toContain('已保存');
    await w.get('#sc-avatar-source').setValue('gfriends_then_metatube');await w.get('form').trigger('submit');expect(w.emitted('save')?.[0]?.[0]).toMatchObject({avatar_source:'gfriends_then_metatube'});w.unmount();
  });
  it('preview stays read-only and import uses the application task lifetime', async () => {
    const preview=vi.spyOn(scrapeApi,'importPreview').mockResolvedValue({total:4,ready:1,existing:1,conflicts:1,damaged:1,missing:0,samples:[],source:'old',destination:'new'});
    const start=vi.spyOn(backgroundApi,'start').mockResolvedValue({total:4,message:''});
    vi.spyOn(backgroundApi,'progress').mockResolvedValue(parseBackgroundProgress({running:false,total:4,done:4,success:2,skipped:1,failed:1,current:'',kind:'actor_import',finished_at:'now',partial:1}));
    const w=mount(ActorImportPanel,{props:{source:'old',disabled:false},global:{plugins:[pinia]}});
    expect(w.find('#actor-import-start').exists()).toBe(false);await w.get('#actor-import-preview').trigger('click');await flushPromises();
    expect(preview).toHaveBeenCalledTimes(1);expect(start).not.toHaveBeenCalled();expect(w.get('#actor-import-summary').text()).toContain('冲突 1');
    await w.get('#actor-import-start').trigger('click');await flushPromises();expect(start.mock.calls[0]?.slice(0,3)).toEqual(['scrape','import',{}]);w.unmount();
  });
  it('uses a read-only request for import preview and validates additive fields', async () => {
    const request=vi.spyOn(apiClient,'request').mockResolvedValue({total:0,ready:0,existing:0,conflicts:0,damaged:0,missing:0,samples:[],source:'old',destination:'new'});
    await scrapeApi.importPreview();expect(request.mock.calls[0]?.[1]).toMatchObject({method:'POST',readOnly:true});
    expect(() => parseScrapeSettings({...fixture,avatar_source:'unknown'})).toThrow();
    expect(() => parseScrapeSettings({...fixture,avatar_configured:'true'})).toThrow();
  });
});
