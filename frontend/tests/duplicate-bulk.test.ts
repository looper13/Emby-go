import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import DuplicateMoviesPanel from '../src/components/libraries/DuplicateMoviesPanel.vue';
import DuplicateBulkDeleteDialog from '../src/components/libraries/DuplicateBulkDeleteDialog.vue';
import { ApiError, apiClient } from '../src/api/client';
import { duplicatesApi, parseDuplicateSelection, type DuplicateSelection, type DuplicateSelectionItem, type DuplicateDeletePreview } from '../src/api/duplicates';
import { parseLibraries } from '../src/api/contracts';
import librariesFixture from '../../vue-migration/fixtures/json/libraries.json';

const libraries=parseLibraries(librariesFixture).items || [];
const keeper={id:500,library_id:1,library_name:'Main',number:'ALDN-001',title:'Keep',source_path:'/films/keep.strm',status:'success',additional_parts:[],created_at:'2020-01-01T00:00:00Z',path_length:16};
const item=(id:number):DuplicateSelectionItem=>({key:'ALDN001',movie:{...keeper,id,title:`Copy ${id}`,source_path:`/films/copy-${id}.strm`,created_at:'2024-01-01T00:00:00Z'},keep:keeper,selection_token:`signed-${id}`});
const selection=(count=3):DuplicateSelection=>({rule:'oldest',library_id:0,items:Array.from({length:count},(_,index)=>item(index+1)),total:count,groups:1});
const preview:DuplicateDeletePreview={movie_id:1,alist_url:'http://alist.test',preview_token:'fresh-preview',expires_at:9999999999,files:[{local_path:'/films/copy-1.strm',remote_path:'/115/movie.mp4',removed:false,remote:{size:100}}]};
const wrappers:VueWrapper[]=[];
function dialog(value=selection()) {const w=mount(DuplicateBulkDeleteDialog,{props:{selection:value,libraryName:'All libraries'}});wrappers.push(w);return w;}
function panel() {const w=mount(DuplicateMoviesPanel,{props:{libraries},global:{stubs:{RouterLink:{props:['to'],template:'<a :href="to"><slot /></a>'}}}});wrappers.push(w);return w;}
function deferred<T>() {let resolve!:(value:T)=>void;const promise=new Promise<T>(r=>{resolve=r;});return {promise,resolve};}
beforeEach(()=>{vi.spyOn(duplicatesApi,'strm').mockResolvedValue({movie_id:1,files:[]});});
afterEach(()=>{wrappers.splice(0).forEach(w=>w.unmount());vi.restoreAllMocks();});

describe('full duplicate selection and serial batch deletion',()=>{
  it('validates full reports rather than silently accepting a truncated page',()=>{
    expect(parseDuplicateSelection(selection(25))).toEqual(selection(25));
    expect(()=>parseDuplicateSelection({...selection(),total:100})).toThrow();
    expect(()=>parseDuplicateSelection({...selection(),rule:'anything'})).toThrow();
    expect(()=>parseDuplicateSelection({...selection(1),items:[{...item(1),keep:item(1).movie}]})).toThrow();
    expect(()=>parseDuplicateSelection({...selection(1),items:[{...item(1),selection_token:''}]})).toThrow();
    expect(()=>parseDuplicateSelection({...selection(2),items:[item(1),item(1)]})).toThrow();
  });
  it('selects all pages, retains exclusions across member pages and opens a confirmation without deleting',async()=>{
    const report=selection(25),read=vi.spyOn(duplicatesApi,'selection').mockResolvedValue(report);
    vi.spyOn(duplicatesApi,'groups').mockResolvedValue({items:[{key:'ALDN001',number:'ALDN-001',count:26}],total:1,total_movies:26,extra_copies:25});
    vi.spyOn(duplicatesApi,'members').mockImplementation(async(_lib,_key,_limit,offset)=>({items:offset?[...report.items.slice(20).map(item=>item.movie),keeper]:report.items.slice(0,20).map(item=>item.movie),total:26}));
    const remove=vi.spyOn(duplicatesApi,'deleteCopy'),previewRead=vi.spyOn(duplicatesApi,'previewDelete');
    const w=panel();expect(read).not.toHaveBeenCalled();await w.get('form').trigger('submit');await flushPromises();
    await w.get('#duplicate-select-all').trigger('click');await flushPromises();expect(read.mock.calls[0]!.slice(0,2)).toEqual([0,'oldest']);expect(w.get('#duplicate-bulk-delete').text()).toContain('25');
    await w.get('[data-duplicate-key="ALDN001"]').trigger('click');await flushPromises();await w.get('[data-duplicate-selected="1"]').setValue(false);expect(w.get('#duplicate-bulk-delete').text()).toContain('24');
    const next=w.findAll('.duplicate-members .duplicate-pagination button')[1]!;await next.trigger('click');await flushPromises();expect(w.get<HTMLInputElement>('[data-duplicate-selected="21"]').element.checked).toBe(true);expect(w.text()).toContain('按规则保留此副本');
    await w.findAll('.duplicate-members .duplicate-pagination button')[0]!.trigger('click');await flushPromises();expect(w.get<HTMLInputElement>('[data-duplicate-selected="1"]').element.checked).toBe(false);
    await w.get('#duplicate-bulk-delete').trigger('click');await flushPromises();expect(w.get('dialog').text()).toContain('全量选择 24 份');expect(remove).not.toHaveBeenCalled();expect(previewRead).not.toHaveBeenCalled();
  });
  it('clears full selection on rule/scope changes and rejects late selection responses',async()=>{
    vi.spyOn(duplicatesApi,'groups').mockResolvedValue({items:[{key:'ALDN001',number:'ALDN-001',count:3}],total:1,total_movies:3,extra_copies:2});
    const late=deferred<DuplicateSelection>(),read=vi.spyOn(duplicatesApi,'selection').mockReturnValueOnce(late.promise).mockResolvedValue(selection());
    const w=panel();await w.get('form').trigger('submit');await flushPromises();await w.get('#duplicate-select-all').trigger('click');await w.get('#duplicate-keep-rule').setValue('longest');expect(read.mock.calls[0]![2].aborted).toBe(true);
    late.resolve(selection());await flushPromises();expect(w.get('#duplicate-bulk-delete').attributes('disabled')).toBeDefined();
    await w.get('#duplicate-select-all').trigger('click');await flushPromises();expect(read.mock.calls[1]![1]).toBe('longest');await w.get('#duplicate-library').setValue(String(libraries[0]!.Id));expect(w.find('#duplicate-bulk-delete').exists()).toBe(false);
  });
  it('requires confirmation, processes one copy at a time and stops after the in-flight deletion',async()=>{
    const checked=vi.spyOn(duplicatesApi,'previewDelete').mockResolvedValue(preview),pending=deferred<unknown>();
    const remove=vi.spyOn(duplicatesApi,'deleteCopy').mockReturnValueOnce(pending.promise).mockResolvedValue(null);
    const w=dialog();await flushPromises();expect(checked).not.toHaveBeenCalled();expect(w.get('#duplicate-bulk-confirm').attributes('disabled')).toBeDefined();
    await w.get('#duplicate-bulk-confirmed').setValue(true);const confirm=w.get('#duplicate-bulk-confirm');await confirm.trigger('click');await confirm.trigger('click');await flushPromises();expect(checked).toHaveBeenCalledTimes(1);expect(remove).toHaveBeenCalledTimes(1);
    expect(remove).toHaveBeenCalledWith(1,0,'ALDN001',preview,{keep_id:500,selection_token:'signed-1'});expect(w.get('dialog').text()).toContain('正在删除第 1 份');await w.get('dialog').trigger('cancel');expect(w.emitted('close')).toBeUndefined();
    await w.findAll('button').find(button=>button.text()==='停止队列')!.trigger('click');pending.resolve(null);await flushPromises();expect(checked).toHaveBeenCalledTimes(1);expect(w.text()).toContain('已完成 1 / 3');expect(w.get('#duplicate-bulk-confirm').attributes('disabled')).toBeDefined();
    await w.get('#duplicate-bulk-confirmed').setValue(true);await w.get('#duplicate-bulk-confirm').trigger('click');await flushPromises();expect(checked.mock.calls.map(call=>call[0])).toEqual([1,2,3]);expect(remove).toHaveBeenCalledTimes(3);expect(w.text()).toContain('批量删除完成');
  });
  it('cancels a pending preview before any deletion and starts no later copies on unmount',async()=>{
    const late=deferred<DuplicateDeletePreview>(),checked=vi.spyOn(duplicatesApi,'previewDelete').mockReturnValue(late.promise),remove=vi.spyOn(duplicatesApi,'deleteCopy');
    const w=dialog();await w.get('#duplicate-bulk-confirmed').setValue(true);await w.get('#duplicate-bulk-confirm').trigger('click');await flushPromises();
    await w.findAll('button').find(button=>button.text()==='停止队列')!.trigger('click');expect(checked.mock.calls[0]![3].aborted).toBe(true);late.resolve(preview);await flushPromises();expect(remove).not.toHaveBeenCalled();expect(w.text()).toContain('已完成 0 / 3');
    await w.get('dialog').trigger('cancel');expect(w.emitted('close')).toEqual([[0]]);
  });
  it('stops on a partial failure, shows removed paths and requires fresh confirmation and preview',async()=>{
    const checked=vi.spyOn(duplicatesApi,'previewDelete').mockResolvedValue(preview);
    const remove=vi.spyOn(duplicatesApi,'deleteCopy').mockRejectedValueOnce(new ApiError('remote denied',502,{removed_remote_paths:['/115/CD1.mp4']})).mockResolvedValue(null);
    const w=dialog(selection(2));await w.get('#duplicate-bulk-confirmed').setValue(true);await w.get('#duplicate-bulk-confirm').trigger('click');await flushPromises();expect(checked).toHaveBeenCalledTimes(1);expect(w.get('[role=alert]').text()).toContain('remote denied');expect(w.text()).toContain('/115/CD1.mp4');expect(w.get('#duplicate-bulk-confirm').attributes('disabled')).toBeDefined();
    await w.get('#duplicate-bulk-confirmed').setValue(true);await w.get('#duplicate-bulk-confirm').trigger('click');await flushPromises();expect(checked.mock.calls.map(call=>call[0])).toEqual([1,1,2]);expect(remove).toHaveBeenCalledTimes(3);expect(w.text()).toContain('已完成 2 / 2');
  });
  it('does not advance to another copy when the page closes during a deletion',async()=>{
    const checked=vi.spyOn(duplicatesApi,'previewDelete').mockResolvedValue(preview),pending=deferred<unknown>(),remove=vi.spyOn(duplicatesApi,'deleteCopy').mockReturnValue(pending.promise);
    const w=dialog();await w.get('#duplicate-bulk-confirmed').setValue(true);await w.get('#duplicate-bulk-confirm').trigger('click');await flushPromises();w.unmount();pending.resolve(null);await flushPromises();expect(checked).toHaveBeenCalledTimes(1);expect(remove).toHaveBeenCalledTimes(1);
  });
  it('refreshes duplicate groups when a finished batch closes',async()=>{
    const list=vi.spyOn(duplicatesApi,'groups').mockResolvedValue({items:[{key:'ALDN001',number:'ALDN-001',count:2}],total:1,total_movies:2,extra_copies:1});vi.spyOn(duplicatesApi,'selection').mockResolvedValue(selection(1));vi.spyOn(duplicatesApi,'previewDelete').mockResolvedValue(preview);vi.spyOn(duplicatesApi,'deleteCopy').mockResolvedValue(null);
    const w=panel();await w.get('form').trigger('submit');await flushPromises();await w.get('#duplicate-select-all').trigger('click');await flushPromises();await w.get('#duplicate-bulk-delete').trigger('click');await flushPromises();await w.get('#duplicate-bulk-confirmed').setValue(true);await w.get('#duplicate-bulk-confirm').trigger('click');await flushPromises();await w.findAll('dialog button').find(button=>button.text()==='关闭')!.trigger('click');await flushPromises();expect(list).toHaveBeenCalledTimes(2);expect(w.text()).toContain('批量删除已完成 1 份');expect(w.find('dialog').exists()).toBe(false);
  });
  it('sends batch identity proofs on both preview and delete while keeping selection read-only',async()=>{
    const request=vi.spyOn(apiClient,'request').mockResolvedValueOnce({...selection(1),rule:'shortest'}).mockResolvedValueOnce(preview).mockResolvedValueOnce(null),signal=new AbortController().signal,proof={keep_id:500,selection_token:'signed-1'};
    await duplicatesApi.selection(0,'shortest',signal);await duplicatesApi.previewDelete(1,0,'ALDN001',signal,proof);await duplicatesApi.deleteCopy(1,0,'ALDN001',preview,proof);
    expect(request.mock.calls[0]).toEqual(['/api/admin/duplicates/selection?library_id=0&rule=shortest',{auth:'required',signal,timeoutMs:30000}]);expect(request.mock.calls[1]![0]).toContain('keep_id=500&selection_token=signed-1');expect(request.mock.calls[2]![1].json).toMatchObject(proof);
  });
});
