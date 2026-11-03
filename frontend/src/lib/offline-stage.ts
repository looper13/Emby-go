import type { OfflineTask } from '../api/cloud115';
import { offlineActive } from './r18-playback';

const labels = new Map([
  ['submit', '提交'], ['reconcile', '任务对账'], ['discover', '文件确认'],
  ['poll', '轮询'], ['remote', '网盘下载'], ['file', '文件检查'],
  ['configuration', '凭证装载'], ['cancel', '取消'], ['cache', '缓存目录'],
  ['cleanup', '自动清理'],
]);
const runningLabels = new Map([
  ['cache', '正在核验缓存目录'],
  ['reconcile', '正在查询网盘任务'],
  ['submit', '正在提交到网盘'],
  ['poll', '正在查询网盘下载状态'],
]);

export function offlineStageLabel(stage: string): string { return labels.get(stage) ?? stage; }
export function offlineStageMessage(task?: OfflineTask): string {
  return task && !task.error && offlineActive(task) ? runningLabels.get(task.stage) ?? '' : '';
}
export function offlineProgressVisible(task?: OfflineTask): boolean { return !!task && task.status !== 'queued'; }
