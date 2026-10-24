import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
import { list } from './scrape';

export interface DuplicateGroup { key: string; number: string; count: number }
export interface DuplicateGroups { items: DuplicateGroup[]; total: number; total_movies: number; extra_copies: number }
export interface DuplicateMovie { id: number; library_id: number; library_name: string; number: string; title: string; source_path: string; status: string; additional_parts: string[] }
export interface DuplicateMembers { items: DuplicateMovie[]; total: number }
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
  strm: async (id:number,library_id:number,key:string,signal:AbortSignal) => parseDuplicateStrmContents(await apiClient.request(`/api/admin/duplicates/items/${id}/strm?${new URLSearchParams({library_id:String(library_id),key})}`,{auth:'required',signal,timeoutMs:30000})),
  previewDelete: async (id:number,library_id:number,key:string,signal:AbortSignal) => parseDuplicateDeletePreview(await apiClient.request(`/api/admin/duplicates/items/${id}/delete-preview?${new URLSearchParams({library_id:String(library_id),key})}`,{auth:'required',signal,timeoutMs:65000})),
  deleteCopy: async (id:number,library_id:number,key:string,preview:DuplicateDeletePreview) => apiClient.request(`/api/admin/duplicates/items/${id}`,{
    auth:'required',method:'DELETE',json:{library_id,key,preview_token:preview.preview_token,expires_at:preview.expires_at},
  }),
};
