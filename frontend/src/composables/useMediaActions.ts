import { onScopeDispose, ref } from 'vue';
import { mediaApi } from '../api/media';
import { useToastStore } from '../stores/toasts';
import { probeSummary, statusText } from '../lib/media';
export function useMediaActions(refresh: () => Promise<unknown>, owner?: () => string) {
  const busy = ref(new Set<string>()); const toasts = useToastStore(); let active = true;
  onScopeDispose(() => { active = false; });
  async function run(kind: 'probe' | 'reread' | 'remove', id: number) {
    const key = `${kind}:${id}`;
    if (busy.value.has(key) || (kind === 'remove' && !window.confirm('确定删除该影片？将删除本地文件（STRM/视频、NFO、图片等）并清理关联网盘源文件。'))) return;
    busy.value.add(key);
    const origin = owner?.();
    try {
      if (kind === 'probe') { toasts.show('正在探测…'); toasts.show(probeSummary(await mediaApi.probe(id))); }
      else if (kind === 'reread') { const status = await mediaApi.reread(id); toasts.show(`状态已更新：${statusText[status] || status}`); }
      else { await mediaApi.remove(id); toasts.show('已删除影片及文件'); }
      if (active && origin === owner?.()) await refresh();
    } catch (cause) { toasts.show(cause instanceof Error ? cause.message : '操作失败', 'error'); }
    finally { busy.value.delete(key); }
  }
  return { busy, run };
}
