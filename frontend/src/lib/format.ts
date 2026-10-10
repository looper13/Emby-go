export function fmtTime(value?: string): string {
  if (!value) return '—'; const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false });
}
export const taskTypes: Record<string, string> = { scan: '增量扫描媒体库', watch: '实时局部刷新', poll: '兼容模式局部刷新', reindex: '全量重建索引', probe: '媒体信息探测', scrape: '内置刮削', scrape_avatars: '演员头像补全' };
export const runStatuses: Record<string, string> = { success: '成功', failed: '失败', skipped: '跳过', cancelled: '已取消', running: '进行中' };
