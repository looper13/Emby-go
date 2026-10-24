import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils';
import DuplicateMoviesPanel from '../src/components/libraries/DuplicateMoviesPanel.vue';
import { duplicatesApi, parseDuplicateGroups, parseDuplicateMembers, type DuplicateGroups, type DuplicateMembers } from '../src/api/duplicates';
import { parseLibraries } from '../src/api/contracts';
import librariesFixture from '../../vue-migration/fixtures/json/libraries.json';

const libraries = parseLibraries(librariesFixture).items || [];
const groups: DuplicateGroups = {items:[{key:'ALDN001',number:'ALDN-001',count:2},{key:'ABF018',number:'ABF-018',count:2}],total:2,total_movies:4,extra_copies:2};
const members: DuplicateMembers = {items:[{id:1,library_id:1,library_name:'First',number:'ALDN-001',title:'Copy one',source_path:'/films/copy.strm',status:'success',additional_parts:['/films/CD2.strm']}],total:2};
const wrappers: VueWrapper[] = [];
function panel() {const w = mount(DuplicateMoviesPanel,{props:{libraries},global:{stubs:{RouterLink:{props:['to'],template:'<a :href="to"><slot /></a>'}}}});wrappers.push(w);return w;}
function deferred<T>() {let resolve!:(v:T)=>void;const promise=new Promise<T>(r=>{resolve=r;});return {promise,resolve};}
afterEach(()=>{wrappers.splice(0).forEach(w=>w.unmount());vi.restoreAllMocks();});

describe('number-based duplicate movie scan',()=>{
  it('parses bounded group/member reports and rejects invalid fields',()=>{
    expect(parseDuplicateGroups(groups)).toEqual(groups);expect(parseDuplicateMembers(members)).toEqual(members);
    expect(()=>parseDuplicateGroups({...groups,total:-1})).toThrow();expect(()=>parseDuplicateMembers({...members,items:[{...members.items[0],source_path:42}]})).toThrow();
  });
  it('runs only on request and expands paths with links without mutations',async()=>{
    const list=vi.spyOn(duplicatesApi,'groups').mockResolvedValue(groups),read=vi.spyOn(duplicatesApi,'members').mockResolvedValue(members);
    const w=panel();expect(list).not.toHaveBeenCalled();await w.get('form').trigger('submit');await flushPromises();
    expect(list.mock.calls[0]?.slice(0,3)).toEqual([0,20,0]);expect(w.get('#duplicate-summary').text()).toContain('2 组');
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();
    expect(read.mock.calls[0]?.slice(0,4)).toEqual([0,'ALDN001',20,0]);expect(w.get('.duplicate-members').text()).toContain('/films/CD2.strm');expect(w.get('.duplicate-movie a').attributes('href')).toBe('/item/1');
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');expect(w.find('.duplicate-members').exists()).toBe(false);
  });
  it('scope changes abort in-flight reports and reject stale results',async()=>{
    const late=deferred<DuplicateGroups>(),list=vi.spyOn(duplicatesApi,'groups').mockReturnValueOnce(late.promise).mockResolvedValueOnce({...groups,items:[],total:0,total_movies:0,extra_copies:0});
    const w=panel();await w.get('form').trigger('submit');await w.get('#duplicate-library').setValue(String(libraries[0]!.Id));
    expect(list.mock.calls[0]![3].aborted).toBe(true);late.resolve(groups);await flushPromises();expect(w.find('#duplicate-summary').exists()).toBe(false);
    await w.get('form').trigger('submit');await flushPromises();expect(w.text()).toContain('未发现重复影片');expect(list.mock.calls[1]![0]).toBe(libraries[0]!.Id);
  });
  it('rejects stale group member responses and supports retry after failures',async()=>{
    vi.spyOn(duplicatesApi,'groups').mockRejectedValueOnce(new Error('scan offline')).mockResolvedValue(groups);
    const late=deferred<DuplicateMembers>(),read=vi.spyOn(duplicatesApi,'members').mockReturnValueOnce(late.promise).mockRejectedValueOnce(new Error('member offline')).mockResolvedValue(members);
    const w=panel();await w.get('form').trigger('submit');await flushPromises();expect(w.get('[role=alert]').text()).toContain('scan offline');await w.get('[role=alert] button').trigger('click');await flushPromises();
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await w.get('[data-duplicate-key="ABF018"]').trigger('click');await flushPromises();
    expect(read.mock.calls[0]![4].aborted).toBe(true);late.resolve({...members,items:[{...members.items[0]!,title:'stale'}]});await flushPromises();expect(w.text()).not.toContain('stale');expect(w.get('[role=alert]').text()).toContain('member offline');
    await w.get('[role=alert] button').trigger('click');await flushPromises();expect(w.find('[role=alert]').exists()).toBe(false);expect(w.text()).toContain('Copy one');
  });
  it('pages members back to page one without closing the group',async()=>{
    vi.spyOn(duplicatesApi,'groups').mockResolvedValue(groups);const read=vi.spyOn(duplicatesApi,'members').mockResolvedValue({...members,total:21});
    const w=panel();await w.get('form').trigger('submit');await flushPromises();await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();
    const buttons=()=>w.findAll('.duplicate-members .duplicate-pagination button');await buttons()[1]!.trigger('click');await flushPromises();expect(read.mock.calls.at(-1)![3]).toBe(20);
    await buttons()[0]!.trigger('click');await flushPromises();expect(read.mock.calls.at(-1)![3]).toBe(0);expect(w.find('.duplicate-members').exists()).toBe(true);
  });
});
