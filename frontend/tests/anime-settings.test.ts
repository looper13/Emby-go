import { afterEach, expect, it, vi } from 'vitest';
import { createPinia, disposePinia } from 'pinia';
import { flushPromises, mount } from '@vue/test-utils';
import { createMemoryHistory, createRouter, RouterView } from 'vue-router';
import AnimeSettingsPage from '../src/pages/AnimeSettingsPage.vue';
import { animeApi, type AnimeSettings } from '../src/api/anime';

afterEach(() => vi.restoreAllMocks());
it('keeps a conflicting draft and only restores the last successful save', async () => {
  const saved: AnimeSettings = { source:{mikan_url:'https://mikan.tangbai.cc',revision:1},library:{Id:1,Name:'动漫',Path:'',Enabled:true,SourceType:'anime_catalog',Revision:1} };
  vi.spyOn(animeApi,'settings').mockResolvedValue(saved);
  const write=vi.spyOn(animeApi,'saveSettings').mockRejectedValue(new Error('数据已被修改，请刷新后重试'));
  const check=vi.spyOn(animeApi,'test').mockResolvedValue({ok:true});
  const pinia=createPinia();
  const router=createRouter({history:createMemoryHistory(),routes:[{path:'/anime/settings',component:AnimeSettingsPage},{path:'/anime',component:{template:'<div />'}}]});
  await router.push('/anime/settings');await router.isReady();
  const wrapper=mount(RouterView,{global:{plugins:[pinia,router]}});
  try {
    await flushPromises();await wrapper.get('#anime-name').setValue('草稿');
    expect(write).not.toHaveBeenCalled();
    const button=(label:string)=>wrapper.findAll('button').find(b=>b.text()===label)!;
    await button('测试草稿连接').trigger('click');await flushPromises();
    expect(check).toHaveBeenCalledOnce();expect(write).not.toHaveBeenCalled();
    await wrapper.get('form').trigger('submit');await flushPromises();
    expect(wrapper.text()).toContain('数据已被修改');
    expect((wrapper.get('#anime-name').element as HTMLInputElement).value).toBe('草稿');
    await button('恢复已保存值').trigger('click');
    expect((wrapper.get('#anime-name').element as HTMLInputElement).value).toBe('动漫');
    expect(write).toHaveBeenCalledOnce();
  } finally { wrapper.unmount();disposePinia(pinia); }
});
