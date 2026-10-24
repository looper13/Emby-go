import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { list } from './scrape';

export interface DuplicateGroup { key: string; number: string; count: number }
export interface DuplicateGroups { items: DuplicateGroup[]; total: number; total_movies: number; extra_copies: number }
export interface DuplicateMovie { id: number; library_id: number; library_name: string; number: string; title: string; source_path: string; status: string; additional_parts: string[] }
export interface DuplicateMembers { items: DuplicateMovie[]; total: number }
export type DuplicateRule = 'oldest' | 'newest' | 'shortest' | 'longest';
export const duplicateRuleLabels: Record<DuplicateRule,string> = {oldest:'保留最早加入',newest:'保留最新加入',shortest:'保留路径最短',longest:'保留路径最长'};
export interface DuplicateSelectionMovie extends DuplicateMovie { created_at:string; path_length:number }
export interface DuplicateSelectionItem { key:string; movie:DuplicateSelectionMovie; keep:DuplicateSelectionMovie; selection_token:string }
export interface DuplicateSelection { rule:DuplicateRule; library_id:number; items:DuplicateSelectionItem[]; total:number; groups:number }
export interface DuplicateSelectionProof { keep_id:number; selection_token:string }
export function parseDuplicateSelection(value:unknown): DuplicateSelection {
  const v=record(value),rule=string(v.rule);
  if (!Object.hasOwn(duplicateRuleLabels,rule)) throw new Error('服务返回了无效的选择规则');
  const movie=(value:unknown):DuplicateSelectionMovie=>{
    const m=record(value),parsed=parseDuplicateMembers({items:[m],total:1}).items[0]!;
    return {...parsed,created_at:string(m.created_at),path_length:count(m.path_length)};
  };
  const items=list(v.items).map(value=>{const item=record(value);return {key:string(item.key),movie:movie(item.movie),keep:movie(item.keep),selection_token:string(item.selection_token)};});
  const total=count(v.total),groups=count(v.groups);
  const targets=new Set(items.map(item=>item.movie.id)),keepers=new Map<string,number>();
  for(const item of items) {
    if(keepers.has(item.key) && keepers.get(item.key)!==item.keep.id) throw new Error('同一分组的保留副本不一致');
    keepers.set(item.key,item.keep.id);
  }
  if (total!==items.length || groups!==keepers.size || targets.size!==total || items.some(item=>!item.key || !item.selection_token || !item.movie.id || !item.keep.id || targets.has(item.keep.id))) throw new Error('服务返回了不完整的全量选择');
  return {rule:rule as DuplicateRule,library_id:count(v.library_id),items,total,groups};
}
export interface DuplicateStrmFile { local_path: string; content: string; error: string }
export interface DuplicateStrmContents { movie_id: number; files: DuplicateStrmFile[] }
export function parseDuplicateStrmContents(value: unknown): DuplicateStrmContents {
  const v = record(value);
  return { movie_id:count(v.movie_id),files:list(v.files).map(item => {
    const f = record(item); return { local_path:string(f.local_path),content:string(f.content),error:string(f.error) };
  }) };
}
export interface DuplicateDeletePreview {
  movie_id: number; alist_url: string; preview_token: string; expires_at: number;
  files: { local_path: string; remote_path: string; removed: boolean; remote: {size:number} }[];
}
export function parseDuplicateDeletePreview(value: unknown): DuplicateDeletePreview {
  const v = record(value);
  return { movie_id:count(v.movie_id),alist_url:string(v.alist_url),preview_token:string(v.preview_token),expires_at:count(v.expires_at),
    files:list(v.files).map(item=>{const f=record(item);return {local_path:string(f.local_path),remote_path:string(f.remote_path),removed:boolean(f.removed),remote:{size:count(record(f.remote).size)}};}) };
}
export function parseDuplicateGroups(value: unknown): DuplicateGroups {
  const v = record(value);
  return { items: list(v.items).map(item => { const g = record(item); return {key:string(g.key),number:string(g.number),count:count(g.count)}; }), total:count(v.total), total_movies:count(v.total_movies), extra_copies:count(v.extra_copies) };
}
export function parseDuplicateMembers(value: unknown): DuplicateMembers {
  const v = record(value);
  return { items:list(v.items).map(item => {const m = record(item); return { id:count(m.id),library_id:count(m.library_id),library_name:string(m.library_name),number:string(m.number),title:string(m.title),source_path:string(m.source_path),status:string(m.status),additional_parts:list(m.additional_parts).map(string) };}), total:count(v.total) };
}
const read = (path: string, params: Record<string,string|number>, signal: AbortSignal) => apiClient.request(`/api/admin/duplicates${path}?${new URLSearchParams(Object.entries(params).map(([key,value]) => [key,String(value)]))}`, { auth:'required',signal });
export const duplicatesApi = {
  groups: async (library_id:number,limit:number,offset:number,signal:AbortSignal) => parseDuplicateGroups(await read('',{library_id,limit,offset},signal)),
  members: async (library_id:number,key:string,limit:number,offset:number,signal:AbortSignal) => parseDuplicateMembers(await read('/items',{library_id,key,limit,offset},signal)),
  selection: async (library_id:number,rule:DuplicateRule,signal:AbortSignal) => {
    const result=parseDuplicateSelection(await apiClient.request(`/api/admin/duplicates/selection?${new URLSearchParams({library_id:String(library_id),rule})}`,{auth:'required',signal,timeoutMs:30000}));
    if(result.library_id!==library_id || result.rule!==rule) throw new Error('全量选择范围或规则与请求不一致');
    return result;
  },
  strm: async (id:number,library_id:number,key:string,signal:AbortSignal) => parseDuplicateStrmContents(await apiClient.request(`/api/admin/duplicates/items/${id}/strm?${new URLSearchParams({library_id:String(library_id),key})}`,{auth:'required',signal,timeoutMs:30000})),
  previewDelete: async (id:number,library_id:number,key:string,signal:AbortSignal,selection?:DuplicateSelectionProof) => parseDuplicateDeletePreview(await apiClient.request(`/api/admin/duplicates/items/${id}/delete-preview?${new URLSearchParams({library_id:String(library_id),key,...(selection?{keep_id:String(selection.keep_id),selection_token:selection.selection_token}:{})})}`,{auth:'required',signal,timeoutMs:65000})),
  deleteCopy: async (id:number,library_id:number,key:string,preview:DuplicateDeletePreview,selection?:DuplicateSelectionProof) => apiClient.request(`/api/admin/duplicates/items/${id}`,{
    auth:'required',method:'DELETE',json:{library_id,key,preview_token:preview.preview_token,expires_at:preview.expires_at,...selection},
  }),
};
