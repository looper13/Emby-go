import { apiClient } from './client';
import { isRecord } from './contracts';

export type CloudProvider = '115' | 'guangya';
export interface CloudConfig { configured: boolean; account_id: string; folder_id: string; folder_name?: string; revision: number }
export interface OfflineFile { id: string; name: string; path: string; size: number }
export interface OfflineTask { id: string; resource_id: string; magnet?: string; info_hash?: string; status: string; progress: number; stage: string; error: string; retryable: boolean; version: number; files: OfflineFile[]; file_id: string; expires_at?: number }
export interface CloudSource { Id: string; Name: string; DirectStreamUrl: string; Container: string; Default: boolean; InfoHash?: string; ResourceId?: string }
export interface OfflineState { configured: boolean; tasks: OfflineTask[]; sources: CloudSource[] }
const statuses = ['queued', 'submitting', 'downloading', 'discovering', 'waiting_selection', 'ready', 'failed', 'timed_out', 'needs_auth', 'cancelled', 'cleanup_pending', 'expired'];
function parsers(name: string) {
  const invalid = () => new Error(`${name} 响应格式不正确`);
  return {
    record(raw: unknown) { if (!isRecord(raw)) throw invalid(); return raw; },
    text(raw: unknown) { if (typeof raw !== 'string') throw invalid(); return raw; },
    number(raw: unknown) { if (typeof raw !== 'number' || !Number.isSafeInteger(raw) || raw < 0) throw invalid(); return raw; },
    bool(raw: unknown) { if (typeof raw !== 'boolean') throw invalid(); return raw; },
    list(raw: unknown): unknown[] { if (!Array.isArray(raw)) throw invalid(); return raw; },
  };
}
export function parseCloudConfig(raw: unknown, name = '115'): CloudConfig {
  const { record, text, number, bool } = parsers(name);
  const r = record(raw);
  return { configured: bool(r.configured), account_id: text(r.account_id), folder_id: text(r.folder_id), revision: number(r.revision), ...(r.folder_name === undefined ? {} : { folder_name: text(r.folder_name) }) };
}
export function parseOfflineState(raw: unknown, name = '115', sourcePrefix?: CloudProvider): OfflineState {
  const { record, text, number, bool, list } = parsers(name);
  const r = record(raw);
  return { configured: bool(r.configured), tasks: list(r.tasks).map(raw => {
    const t = record(raw), status = text(t.status);
    if (!statuses.includes(status)) throw new Error(`${name} 任务状态不正确`);
    return { id: text(t.id), resource_id: text(t.resource_id), magnet: t.magnet === undefined ? '' : text(t.magnet), info_hash: t.info_hash === undefined ? '' : text(t.info_hash), status, progress: number(t.progress), stage: text(t.stage), error: text(t.error), retryable: bool(t.retryable), version: number(t.version), file_id: text(t.file_id), expires_at: t.expires_at === undefined ? 0 : number(t.expires_at), files: list(t.files).map(raw => {
      const f = record(raw); return { id: text(f.id), name: text(f.name), path: text(f.path), size: number(f.size) };
    }) };
  }), sources: list(r.sources).map(raw => {
    const s = record(raw), url = text(s.DirectStreamUrl), id = text(s.Id);
    if (!/^\/Videos\/r18-[1-9]\d*-[A-Za-z0-9_-]+\/stream\?[^\s\\]*$/u.test(url)) throw new Error(`${name} 播放地址不正确`);
    if (sourcePrefix && (!id.startsWith(`${sourcePrefix}-`) || id.length <= sourcePrefix.length + 1)) throw new Error(`${name} 播放源标识不正确`);
    return { Id: id, Name: text(s.Name), DirectStreamUrl: url, Container: text(s.Container), Default: bool(s.Default), InfoHash: s.InfoHash === undefined ? '' : text(s.InfoHash), ResourceId: s.ResourceId === undefined ? '' : text(s.ResourceId) };
  }) };
}
export function createOfflineApi(provider: CloudProvider, name: string) {
  function base(library: string, video: string) { return `/api/r18/libraries/${encodeURIComponent(library)}/videos/${encodeURIComponent(video)}/offline${provider === '115' ? '' : `/${provider}`}`; }
  const parse = (raw: unknown) => parseOfflineState(raw, name, provider);
  return {
    async state(library: string, video: string, signal?: AbortSignal) { return parse(await apiClient.request(base(library, video), { auth: 'required', signal, timeoutMs: 10000 })); },
    async prepare(library: string, video: string, source: string, signal?: AbortSignal) {
      return parse(await apiClient.request(`${base(library, video)}/prepare?${new URLSearchParams({ source_id: source })}`, { auth: 'required', signal, timeoutMs: 25000 }));
    },
    async submit(library: string, video: string, value: { resource_id?: string; magnet?: string }, signal?: AbortSignal) {
      return parse(await apiClient.request(base(library, video), { auth: 'required', method: 'POST', json: value, signal, timeoutMs: 15000 }));
    },
    async select(library: string, video: string, task: string, file: string, signal?: AbortSignal, makeDefault = true, prepare = false) {
      return parse(await apiClient.request(`${base(library, video)}/selection`, { auth: 'required', method: 'POST', json: { task_id: task, file_id: file, default: makeDefault, prepare }, signal, timeoutMs: prepare ? 25000 : 10000 }));
    },
    async change(library: string, video: string, task: OfflineTask, action: 'retry' | 'cancel', signal?: AbortSignal) {
      return parse(await apiClient.request(`${base(library, video)}/tasks/${encodeURIComponent(task.id)}/${action}`, { auth: 'required', method: 'POST', json: { version: task.version }, signal }));
    },
  };
}
export type OfflineApi = ReturnType<typeof createOfflineApi>;
export const cloud115Api = {
  ...createOfflineApi('115', '115'),
  async config(signal?: AbortSignal) { return parseCloudConfig(await apiClient.request('/api/admin/115/connection', { auth: 'required', signal })); },
  async save(value: { cookie: string; revision: number }, signal?: AbortSignal) {
    await apiClient.request('/api/admin/115/connection', { auth: 'required', method: 'PUT', json: value, signal, timeoutMs: 65000 });
  },
};
