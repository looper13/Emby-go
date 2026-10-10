import { apiClient } from './client';
import { boolean, count, record, string } from './admin';
export interface ScanResult { success: number; pending: number; incompatible: number; failed: number; added: number; updated: number; skipped: number; deleted: number }
export interface ScanProgress extends ScanResult {
  running: boolean; library_index: number; libraries: number; library_name: string; phase: string;
  total: number; done: number; current: string; started_at?: string; finished_at?: string; cancelled?: boolean; error?: string;
}
export function parseScanResult(value: unknown): ScanResult {
  const v = record(value);
  return { success: count(v.success), pending: count(v.pending), incompatible: count(v.incompatible), failed: count(v.failed),
    added: count(v.added), updated: count(v.updated), skipped: count(v.skipped), deleted: count(v.deleted) };
}
export function parseScanProgress(value: unknown): ScanProgress {
  const v = record(value);
  return { ...parseScanResult(v), running: boolean(v.running), library_index: count(v.library_index), libraries: count(v.libraries),
    library_name: string(v.library_name), phase: v.phase == null ? '' : string(v.phase), total: count(v.total), done: count(v.done), current: string(v.current),
    started_at: v.started_at == null ? undefined : string(v.started_at), finished_at: v.finished_at == null ? undefined : string(v.finished_at),
    cancelled: v.cancelled == null ? false : boolean(v.cancelled), error: v.error == null ? '' : string(v.error) };
}
export const taskApi = {
  async progress(signal?: AbortSignal) { return parseScanProgress(await apiClient.request('/api/admin/scan/progress', { auth: 'required', signal })); },
  async run(kind: 'scan' | 'reindex', libraryId?: number, signal?: AbortSignal) {
    const suffix = kind === 'scan' && libraryId ? `?library_id=${encodeURIComponent(libraryId)}` : '';
    return parseScanResult(await apiClient.request(`/api/admin/${kind}${suffix}`, { auth: 'required', method: 'POST', signal }));
  },
};
