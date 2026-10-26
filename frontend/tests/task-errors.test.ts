import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, disposePinia, setActivePinia, type Pinia } from 'pinia';
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils';
import TaskError from '../src/components/tasks/TaskError.vue';
import TasksPage from '../src/pages/TasksPage.vue';
import { adminApi, parseTasks } from '../src/api/admin';

const summary = '局部刷新有 52 个条目处理失败: /mnt/data/mdc/guangya';
const detail = '失败明细（显示 20 / 52 条；全部失败项见运行日志）：\n\n1. 目录: /mnt/data/mdc/guangya/subfolder\n文件: /mnt/data/mdc/guangya/subfolder/movie.nfo\n阶段: 读取或解析 NFO\n原因: XML syntax error on line 12';
const error = `${summary}\n${detail}`;
const history = parseTasks({ running: false, items: [{ id: 1, type: 'watch', status: 'failed', started_at: '2026-10-26T06:52:12Z', ended_at: '2026-10-26T06:52:23Z', error }] });
let pinia: Pinia;
const wrappers: VueWrapper[] = [];
beforeEach(() => { pinia = createPinia(); setActivePinia(pinia); });
afterEach(() => { wrappers.splice(0).forEach(w => w.unmount()); disposePinia(pinia); vi.restoreAllMocks(); });

describe('task failure details', () => {
  it('shows a short summary with native collapsible, selectable multiline details', () => {
    const w = mount(TaskError, { props: { error } }); wrappers.push(w);
    expect(w.get('.task-error > span').text()).toBe(summary);
    expect(w.get('summary').text()).toBe('查看失败明细');
    expect(w.get('details').attributes('open')).toBeUndefined();
    expect(w.get('pre').element.textContent).toBe(detail);
    w.get('details').element.setAttribute('open', '');
    expect(w.get('details').attributes('open')).toBe('');
  });
  it('keeps old single-line errors and successful tasks compatible', async () => {
    const w = mount(TaskError, { props: { error: '局部刷新有 344 个文件读取失败: /mnt/data/Anime' } }); wrappers.push(w);
    expect(w.text()).toContain('/mnt/data/Anime'); expect(w.find('details').exists()).toBe(false);
    await w.setProps({ error: undefined }); expect(w.text()).toBe('—');
  });
  it('renders file names and original errors as text, never executable markup', () => {
    const w = mount(TaskError, { props: { error: `${summary}\n<script>window.bad = true</script>\n<img src=x onerror=bad()>` } }); wrappers.push(w);
    expect(w.get('pre').text()).toContain('<script>'); expect(w.find('script').exists()).toBe(false); expect(w.find('img').exists()).toBe(false);
  });
  it('passes task API details to the history table and preserves expansion on refresh', async () => {
    const read = vi.spyOn(adminApi, 'tasks').mockResolvedValue(history);
    const w = mount(TasksPage); wrappers.push(w); await flushPromises();
    expect(w.get('tbody').text()).toContain('失败'); expect(w.get('tbody .task-error > span').text()).toBe(summary);
    w.get('details').element.setAttribute('open', '');
    await w.get('#task-refresh').trigger('click'); await flushPromises();
    expect(read).toHaveBeenCalledTimes(2); expect(w.get('details').attributes('open')).toBe(''); expect(w.get('pre').text()).toContain('subfolder/movie.nfo');
  });
});
