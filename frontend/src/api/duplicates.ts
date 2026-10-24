import { apiClient } from './client';
import { count, record, string } from './admin';
import { list } from './scrape';

export interface DuplicateGroup { key: string; number: string; count: number }
export interface DuplicateGroups { items: DuplicateGroup[]; total: number; total_movies: number; extra_copies: number }
export interface DuplicateMovie { id: number; library_id: number; library_name: string; number: string; title: string; source_path: string; status: string; additional_parts: string[] }
export interface DuplicateMembers { items: DuplicateMovie[]; total: number }
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
};
