import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils';
import DuplicateMoviesPanel from '../src/components/libraries/DuplicateMoviesPanel.vue';
import DuplicateDeleteDialog from '../src/components/libraries/DuplicateDeleteDialog.vue';
import DuplicateStrmViewer from '../src/components/libraries/DuplicateStrmContents.vue';
import { ApiError, apiClient } from '../src/api/client';
import { duplicatesApi, parseDuplicateGroups, parseDuplicateMembers, parseDuplicateDeletePreview, parseDuplicateStrmContents, type DuplicateGroups, type DuplicateMembers, type DuplicateDeletePreview, type DuplicateStrmContents } from '../src/api/duplicates';
import { parseLibraries } from '../src/api/contracts';
import librariesFixture from '../../vue-migration/fixtures/json/libraries.json';

const libraries = parseLibraries(librariesFixture).items || [];
const groups: DuplicateGroups = {items:[{key:'ALDN001',number:'ALDN-001',count:2},{key:'ABF018',number:'ABF-018',count:2}],total:2,total_movies:4,extra_copies:2};
const members: DuplicateMembers = {items:[{id:1,library_id:1,library_name:'First',number:'ALDN-001',title:'Copy one',source_path:'/films/copy.strm',status:'success',additional_parts:['/films/CD2.strm']}],total:2};
const wrappers: VueWrapper[] = [];
const preview: DuplicateDeletePreview = {movie_id:1,alist_url:'http://alist.test',preview_token:'signed-confirmation',expires_at:9999999999,files:[{local_path:'/films/copy.strm',remote_path:'/115/copy.mp4',removed:false,remote:{size:1048576}}]};
const strm: DuplicateStrmContents = {movie_id:1,files:[{local_path:'/films/copy.strm',content:'https://media.test/copy.mp4?sign=full-link\n',error:''},{local_path:'/films/CD2.strm',content:'https://media.test/CD2.mp4',error:''}]};
function panel() {const w = mount(DuplicateMoviesPanel,{props:{libraries},global:{stubs:{RouterLink:{props:['to'],template:'<a :href="to"><slot /></a>'}}}});wrappers.push(w);return w;}
function deferred<T>() {let resolve!:(v:T)=>void;const promise=new Promise<T>(r=>{resolve=r;});return {promise,resolve};}
beforeEach(()=>{vi.spyOn(duplicatesApi,'strm').mockResolvedValue(strm);});
afterEach(()=>{wrappers.splice(0).forEach(w=>w.unmount());vi.restoreAllMocks();});

describe('number-based duplicate movie scan',()=>{
  it('parses bounded group/member reports and rejects invalid fields',()=>{
    expect(parseDuplicateGroups(groups)).toEqual(groups);expect(parseDuplicateMembers(members)).toEqual(members);
    expect(()=>parseDuplicateGroups({...groups,total:-1})).toThrow();expect(()=>parseDuplicateMembers({...members,items:[{...members.items[0],source_path:42}]})).toThrow();
  });
  it('runs only on request and expands paths with links without mutations',async()=>{
    const list=vi.spyOn(duplicatesApi,'groups').mockResolvedValue(groups),read=vi.spyOn(duplicatesApi,'members').mockResolvedValue(members);
    const w=panel();expect(list).not.toHaveBeenCalled();expect(duplicatesApi.strm).not.toHaveBeenCalled();await w.get('form').trigger('submit');await flushPromises();
    expect(duplicatesApi.strm).not.toHaveBeenCalled();
    expect(list.mock.calls[0]?.slice(0,3)).toEqual([0,20,0]);expect(w.get('#duplicate-summary').text()).toContain('2 组');
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();
    expect(read.mock.calls[0]?.slice(0,4)).toEqual([0,'ALDN001',20,0]);expect(w.get('.duplicate-members').text()).toContain('/films/CD2.strm');expect(w.get('.duplicate-movie a').attributes('href')).toBe('/item/1');
    expect(duplicatesApi.strm).toHaveBeenCalledWith(1,0,'ALDN001',expect.any(AbortSignal));expect(w.findAll('.duplicate-strm-content').map(block=>block.text())).toEqual(strm.files.map(file=>file.content.trim()));
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');expect(w.find('.duplicate-members').exists()).toBe(false);
  });
  it('shows paths before content arrives, keeps per-file failures visible and renders content as plain text',async()=>{
    vi.spyOn(duplicatesApi,'groups').mockResolvedValue(groups);vi.spyOn(duplicatesApi,'members').mockResolvedValue(members);
    const pending=deferred<DuplicateStrmContents>();vi.mocked(duplicatesApi.strm).mockReturnValueOnce(pending.promise);
    const remove=vi.spyOn(duplicatesApi,'deleteCopy'),previewRead=vi.spyOn(duplicatesApi,'previewDelete');
    const w=panel();await w.get('form').trigger('submit');await flushPromises();await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();
    expect(w.get('.duplicate-strm').text()).toContain('/films/copy.strm');expect(w.get('.duplicate-strm [role=status]').text()).toContain('正在读取');
    pending.resolve({movie_id:1,files:[{...strm.files[0]!,content:'<script>window.unwanted=true</script>\nhttps://media.test/raw.mp4'},{...strm.files[1]!,content:'',error:'STRM 文件不存在'}]});await flushPromises();
    expect(w.get('.duplicate-strm-content').element.textContent).toBe('<script>window.unwanted=true</script>\nhttps://media.test/raw.mp4');expect(w.find('.duplicate-strm script').exists()).toBe(false);expect(w.find('.duplicate-strm a').exists()).toBe(false);
    expect(w.get('.duplicate-strm-error').text()).toContain('文件不存在');expect(remove).not.toHaveBeenCalled();expect(previewRead).not.toHaveBeenCalled();
  });
  it('cancels content reads on collapse and reads fresh content on reopening',async()=>{
    vi.spyOn(duplicatesApi,'groups').mockResolvedValue(groups);vi.spyOn(duplicatesApi,'members').mockResolvedValue(members);
    const pending=deferred<DuplicateStrmContents>(),read=vi.mocked(duplicatesApi.strm).mockReturnValueOnce(pending.promise).mockResolvedValueOnce({...strm,files:[{...strm.files[0]!,content:'fresh content'}]});
    const w=panel();await w.get('form').trigger('submit');await flushPromises();await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');expect(read.mock.calls[0]![3].aborted).toBe(true);
    pending.resolve({...strm,files:[{...strm.files[0]!,content:'stale content'}]});await flushPromises();expect(w.text()).not.toContain('stale content');
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();expect(read).toHaveBeenCalledTimes(2);expect(w.text()).toContain('fresh content');
  });
  it('ends timed-out content reads, retries and distinguishes an empty file',async()=>{
    vi.mocked(duplicatesApi.strm).mockRejectedValueOnce(new DOMException('request timeout','TimeoutError')).mockResolvedValueOnce({...strm,files:[{...strm.files[0]!,content:''}]});
    const w=mount(DuplicateStrmViewer,{props:{movie:members.items[0]!,libraryId:0,groupKey:'ALDN001'}});wrappers.push(w);await flushPromises();
    expect(w.find('[role=status]').exists()).toBe(false);expect(w.get('[role=alert]').text()).toContain('读取 STRM 内容超时');expect(w.text()).toContain('/films/copy.strm');
    await w.get('[role=alert] button').trigger('click');await flushPromises();expect(w.find('[role=alert]').exists()).toBe(false);expect(w.text()).toContain('STRM 文件为空');
  });
  it('rejects stale content responses when the group changes and aborts on unmount',async()=>{
    const pending=deferred<DuplicateStrmContents>(),read=vi.mocked(duplicatesApi.strm).mockReturnValueOnce(pending.promise).mockResolvedValueOnce({...strm,files:[{...strm.files[0]!,content:'current group'}]});
    const w=mount(DuplicateStrmViewer,{props:{movie:members.items[0]!,libraryId:0,groupKey:'ALDN001'}});wrappers.push(w);await flushPromises();
    await w.setProps({groupKey:'ABF018'});await flushPromises();expect(read.mock.calls[0]![3].aborted).toBe(true);
    pending.resolve({...strm,files:[{...strm.files[0]!,content:'stale group'}]});await flushPromises();expect(w.text()).toContain('current group');expect(w.text()).not.toContain('stale group');
    w.unmount();expect(read.mock.calls[1]![3].aborted).toBe(true);
  });
  it('validates STRM responses and uses an authenticated bounded read with encoded scope',async()=>{
    expect(parseDuplicateStrmContents(strm)).toEqual(strm);expect(()=>parseDuplicateStrmContents({...strm,files:[{...strm.files[0],content:42}]})).toThrow();
    expect(()=>parseDuplicateStrmContents({...strm,files:[{...strm.files[0],error:null}]})).toThrow();
    const request=vi.spyOn(apiClient,'request').mockResolvedValue(strm),signal=new AbortController().signal;
    vi.mocked(duplicatesApi.strm).mockRestore();
    expect(await duplicatesApi.strm(1,7,'A&B',signal)).toEqual(strm);
    expect(request).toHaveBeenCalledWith('/api/admin/duplicates/items/1/strm?library_id=7&key=A%26B',{auth:'required',signal,timeoutMs:30000});
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
  it('previews paths and requires explicit confirmation before deleting then refreshes groups',async()=>{
    const list=vi.spyOn(duplicatesApi,'groups').mockResolvedValue(groups);vi.spyOn(duplicatesApi,'members').mockResolvedValue(members);
    const read=vi.spyOn(duplicatesApi,'previewDelete').mockResolvedValue(preview),remove=vi.spyOn(duplicatesApi,'deleteCopy').mockResolvedValue(null);
    const w=panel();await w.get('form').trigger('submit');await flushPromises();await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();
    await w.get('[data-duplicate-delete="1"]').trigger('click');await flushPromises();
    expect(read.mock.calls[0]!.slice(0,3)).toEqual([1,0,'ALDN001']);expect(w.get('dialog').text()).toContain('/115/copy.mp4');expect(remove).not.toHaveBeenCalled();
    expect(w.get('#duplicate-delete-confirm').attributes('disabled')).toBeDefined();await w.get('#duplicate-delete-confirmed').setValue(true);await w.get('#duplicate-delete-confirm').trigger('click');await flushPromises();
    expect(remove).toHaveBeenCalledWith(1,0,'ALDN001',preview);expect(w.find('dialog').exists()).toBe(false);expect(list).toHaveBeenCalledTimes(2);expect(w.text()).toContain('已删除远程文件');
  });
  it('keeps failed copies visible and requires a new preview before retrying a partial deletion',async()=>{
    vi.spyOn(duplicatesApi,'previewDelete').mockResolvedValue(preview);
    let resolve!:(value:unknown)=>void;const pending=new Promise(r=>{resolve=r;});
    const remove=vi.spyOn(duplicatesApi,'deleteCopy').mockRejectedValueOnce(new ApiError('remote denied',502,{removed_remote_paths:['/115/CD1.mp4']})).mockReturnValueOnce(pending);
    const w=mount(DuplicateDeleteDialog,{props:{movie:members.items[0]!,libraryId:0,groupKey:'ALDN001'},global:{stubs:{RouterLink:true}}});wrappers.push(w);await flushPromises();
    await w.get('#duplicate-delete-confirmed').setValue(true);await w.get('#duplicate-delete-confirm').trigger('click');await flushPromises();
    expect(w.text()).toContain('remote denied');expect(w.text()).toContain('/115/CD1.mp4');expect(w.find('#duplicate-delete-confirm').exists()).toBe(false);expect(w.emitted('deleted')).toBeUndefined();
    await w.get('[role=alert] button').trigger('click');await flushPromises();expect(w.get<HTMLInputElement>('#duplicate-delete-confirmed').element.checked).toBe(false);
    await w.get('#duplicate-delete-confirmed').setValue(true);await w.get('#duplicate-delete-confirm').trigger('click');await w.get('#duplicate-delete-confirm').trigger('click');expect(remove).toHaveBeenCalledTimes(2);
    await w.get('dialog').trigger('cancel');expect(w.emitted('close')).toBeUndefined();resolve(null);await flushPromises();expect(w.emitted('deleted')).toHaveLength(1);
  });
  it('cancels preview reads when the dialog is closed and validates preview responses',async()=>{
    expect(parseDuplicateDeletePreview(preview)).toEqual(preview);expect(()=>parseDuplicateDeletePreview({...preview,files:[{...preview.files[0],removed:'no'}]})).toThrow();
    const late=deferred<DuplicateDeletePreview>(),read=vi.spyOn(duplicatesApi,'previewDelete').mockReturnValue(late.promise),remove=vi.spyOn(duplicatesApi,'deleteCopy');
    const w=mount(DuplicateDeleteDialog,{props:{movie:members.items[0]!,libraryId:0,groupKey:'ALDN001'},global:{stubs:{RouterLink:true}}});
    await flushPromises();w.unmount();expect(read.mock.calls[0]![3].aborted).toBe(true);late.resolve(preview);await flushPromises();expect(remove).not.toHaveBeenCalled();
  });
});
