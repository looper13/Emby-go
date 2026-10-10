export const statusText: Record<string, string> = { success: '已入库', manual: '手动', pending: '待补录', incompatible: '不兼容', failed: '失败' };
export const entityParams = [['person', '演员'], ['genre', '类型'], ['studio', '厂商'], ['tag', '标签'], ['collection', '合集']] as const;
export const compactNumber = (value: unknown) => String(value ?? '').toUpperCase().replace(/[-_.\s]/g, '');
export function titleCarriesNumber(title: unknown, number: unknown) {
  const lead = String(title ?? '').trim().match(/^[A-Za-z0-9._-]+/); const normalized = compactNumber(number);
  return !!normalized && !!lead && compactNumber(lead[0]) === normalized;
}
export const numberUnlessInTitle = (title: string, number: string) => titleCarriesNumber(title, number) ? '' : number;
export const plotClamped = (text: string) => String(text || '').length > 260;
export function entityIdOf(kind: string, name: string) {
  let binary = ''; new TextEncoder().encode(name).forEach(byte => { binary += String.fromCharCode(byte); });
  return `${kind.toLowerCase()}:${btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')}`;
}
export function fmtSize(bytes: number) {
  if (!bytes) return '—'; const units = ['B', 'KB', 'MB', 'GB', 'TB']; let value = bytes, unit = 0;
  while (value >= 1024 && unit < units.length - 1) { value /= 1024; unit++; }
  return `${value.toFixed(value < 10 && unit > 0 ? 1 : 0)} ${units[unit]}`;
}
export function fmtDuration(seconds: number) {
  if (!seconds) return ''; const total = Math.round(seconds); const h = Math.floor(total / 3600); const m = Math.floor(total % 3600 / 60);
  return h ? `${h} 小时 ${m} 分` : `${m} 分钟`;
}
export function probeSummary(info: Record<string, unknown>) {
  const video = (info.video ?? {}) as Record<string, unknown>; const parts = [];
  if (video.width && video.height) parts.push(`${video.width}x${video.height}`);
  if (video.codec) parts.push(String(video.codec).toUpperCase()); if (video.profile) parts.push(String(video.profile));
  if (video.framerate) parts.push(`${Number(video.framerate).toFixed(2)}fps`);
  const bitrate = Number(info.bitrate || video.bitrate); if (bitrate) parts.push(`${(bitrate / 1000000).toFixed(2)} Mbps`);
  return parts.length ? '已写入 NFO：' + parts.join(' · ') : '已写入 NFO';
}
