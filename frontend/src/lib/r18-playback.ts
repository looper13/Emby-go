import type { CloudSource, OfflineFile, OfflineState, OfflineTask } from '../api/cloud115';
import type { R18Resource } from '../api/r18';

export const offlineLabels: Record<string, string> = { queued: '排队中', submitting: '提交中 / 正在确认任务', downloading: '115 离线下载中', discovering: '正在确认视频文件', waiting_selection: '请选择正片文件', ready: '已就绪', failed: '失败', timed_out: '等待超时', needs_auth: '需要重新授权', cancelled: '已取消本地等待', cleanup_pending: '缓存到期 / 正在自动删除', expired: '缓存已自动删除' };
export const offlineActive = (t: OfflineTask) => ['queued', 'submitting', 'downloading', 'discovering'].includes(t.status);

export function magnetHash(magnet: string): string {
  try {
    const url = new URL(magnet);
    if (url.protocol !== 'magnet:') return '';
    const raw = url.searchParams.getAll('xt').find(xt => /^urn:btih:/iu.test(xt))?.slice(9).toLowerCase() ?? '';
    if (/^[a-f0-9]{40}$/u.test(raw)) return raw;
    if (!/^[a-z2-7]{32}$/u.test(raw)) return '';
    const alphabet = 'abcdefghijklmnopqrstuvwxyz234567';
    let bits = 0, value = 0, hex = '';
    for (const letter of raw) {
      value = (value << 5) | alphabet.indexOf(letter); bits += 5;
      if (bits >= 8) { bits -= 8; hex += ((value >>> bits) & 255).toString(16).padStart(2, '0'); }
    }
    return hex;
  } catch { return ''; }
}

export interface R18PlaybackVersion {
  id: string; name: string; magnet: string; resourceId: string;
  task?: OfflineTask; source?: CloudSource;
}
export function playbackVersions(resources: R18Resource[], state: OfflineState): R18PlaybackVersion[] {
  const versions = new Map<string, R18PlaybackVersion>();
  for (const r of resources) {
    const hash = magnetHash(r.magnet);
    if (hash && !versions.has(hash)) versions.set(hash, { id: hash, name: r.title || r.number || '磁力版本', magnet: r.magnet, resourceId: r.id });
  }
  for (const task of state.tasks) {
    const key = task.info_hash?.toLowerCase() || magnetHash(task.magnet || '') || [...versions.values()].find(v => v.resourceId === task.resource_id)?.id || `task:${task.id}`;
    const version = versions.get(key) ?? { id: key, name: task.files.find(f => f.id === task.file_id)?.name || '手动磁力版本', magnet: task.magnet || '', resourceId: task.resource_id };
    version.task = task; versions.set(key, version);
  }
  for (const source of state.sources) {
    const key = source.InfoHash?.toLowerCase() || [...versions.values()].find(v => `115-${v.task?.id}` === source.Id || !!source.ResourceId && v.resourceId === source.ResourceId)?.id || `source:${source.Id}`;
    const version = versions.get(key) ?? { id: key, name: source.Name, magnet: '', resourceId: source.ResourceId || '' };
    version.source = source; versions.set(key, version);
  }
  return [...versions.values()];
}

// Recommend the largest video, never the first directory entry (often a short attachment).
export function mainVideo(files: OfflineFile[]): OfflineFile | undefined {
  return [...files].sort((a, b) => b.size - a.size || a.path.localeCompare(b.path))[0];
}
