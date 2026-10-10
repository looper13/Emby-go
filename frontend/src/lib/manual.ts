import type { ManualPayload } from '../api/admin';
export function manualPayload(fields: Record<string, string>): ManualPayload {
  const year = fields.year ? Number(fields.year) : 0;
  if (year !== 0 && (!Number.isInteger(year) || year < 1900 || year > 2100)) throw new Error('年份应在 1900–2100 之间');
  let url: URL;
  try { url = new URL(fields.source_url ?? ''); } catch { throw new Error('媒体直链必须为 http/https'); }
  if (!['http:', 'https:'].includes(url.protocol)) throw new Error('媒体直链必须为 http/https');
  if (!fields.title?.trim() || !fields.source_path?.trim()) throw new Error('标题和 .strm 目标路径必填');
  const split = (key: string) => (fields[key] ?? '').split(/[,，]/u).map(v => v.trim()).filter(Boolean);
  return { title: fields.title, source_path: fields.source_path, source_url: fields.source_url!, year,
    number: fields.number ?? '', original_title: fields.original_title ?? '', plot: fields.plot ?? '',
    genres: split('genres'), tags: split('tags'), studios: split('studios') };
}
