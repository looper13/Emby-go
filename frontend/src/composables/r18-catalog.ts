export type R18BrowseMode = 'latest' | 'categories' | 'top250' | 'ranking' | 'playback' | 'popular';
export interface R18CatalogQuery {
  library_id: string; mode: R18BrowseMode; type: string; filter: string; search: string;
  sort: string; order: string; year: string; month: string; duration: string;
  tag_ids: string[]; main_filter: string[]; period: string; ignore_watched: string;
  page: number; page_size: string;
}
export const r18Modes: { value: R18BrowseMode; label: string }[] = [
  { value: 'latest', label: '最新影片' }, { value: 'categories', label: '分类查询' },
  { value: 'top250', label: 'TOP250' }, { value: 'ranking', label: '排行榜' },
  { value: 'playback', label: '热播' }, { value: 'popular', label: '热门' },
];
export const r18Sorts = [
  { value: 'release', label: '发行日期' }, { value: 'update', label: '最近更新' },
  { value: 'score', label: '评分' }, { value: 'hit', label: '热度' },
  { value: 'want_watch_count', label: '想看人数' }, { value: 'watched_count', label: '看过人数' },
];
export const r18BasicFilters = ['p', 'm', 'c', 's', 'i', 'v'];
export const r18Durations = ['lt-45', '45-90', '90-120', 'gt-120'];
export const r18SpecialGroups = ['main', 'year', 'month', 'duration'];
const modes = r18Modes.map(choice => choice.value);
const filters = ['all', 'magnets', 'subtitle', 'can_play'];
const periods = ['daily', 'weekly', 'monthly'];
const single = (input: Record<string, unknown>, key: string) => typeof input[key] === 'string' ? (input[key] as string).trim() : '';
const split = (raw: string) => [...new Set(raw.split(',').map(value => value.trim()).filter(Boolean))];
export function isR18Chart(mode: R18BrowseMode) { return !['latest', 'categories'].includes(mode); }
export function r18Types(mode: R18BrowseMode) {
  return mode === 'playback' || mode === 'popular' ? ['all']
    : mode === 'ranking' ? ['0', '1', '2', '3'] : mode === 'top250' ? ['all', '0', '1', '2', '3'] : ['all', '0', '1', '2', '3', '4'];
}
export function readR18CatalogQuery(input: Record<string, unknown>): R18CatalogQuery {
  const search = single(input, 'search');
  const rawMode = single(input, 'mode');
  const mode = search ? 'latest' : modes.includes(rawMode as R18BrowseMode) ? rawMode as R18BrowseMode : 'latest';
  const allowedTypes = r18Types(mode), rawType = single(input, 'type'), rawFilter = single(input, 'filter');
  const page = Number(single(input, 'page'));
  const rawSort = single(input, 'sort'), rawPeriod = single(input, 'period');
  return { library_id: single(input, 'library_id'), mode, search,
    type: allowedTypes.includes(rawType) ? rawType : mode === 'ranking' ? '0' : 'all',
    filter: isR18Chart(mode) ? 'all' : filters.includes(rawFilter) ? rawFilter : 'magnets',
    sort: r18Sorts.some(choice => choice.value === rawSort) && !(mode === 'categories' && rawSort === 'update') ? rawSort : 'release',
    order: single(input, 'order') === 'asc' ? 'asc' : 'desc', year: single(input, 'year'),
    month: single(input, 'month'), duration: single(input, 'duration'),
    tag_ids: split(single(input, 'tag_ids')), main_filter: split(single(input, 'main_filter')),
    period: periods.includes(rawPeriod) ? rawPeriod : 'daily',
    ignore_watched: ['true', 'false'].includes(single(input, 'ignore_watched')) ? single(input, 'ignore_watched') : '',
    page: Number.isSafeInteger(page) && page >= 1 && page <= 9999 ? page : 1,
    page_size: ['24', '40', '50'].includes(single(input, 'page_size')) ? single(input, 'page_size') : '24' };
}
export function serializeR18CatalogQuery(state: R18CatalogQuery, target = state.page): Record<string, string> {
  if (!Number.isSafeInteger(target) || target < 1 || target > 9999) throw new Error('请输入 1 到 9999 的整数页码');
  const search = state.search.trim(), mode = search ? 'latest' : state.mode;
  const allowedTypes = r18Types(mode);
  const type = allowedTypes.includes(state.type) ? state.type : mode === 'ranking' ? '0' : 'all';
  const query: Record<string, string> = { library_id: state.library_id, type,
    filter: isR18Chart(mode) ? 'all' : state.filter, page: String(target), page_size: state.page_size };
  if (mode !== 'latest') query.mode = mode;
  if (search) { query.search = search; return query; }
  if (mode === 'top250' && (target - 1) * Number(state.page_size) >= 250) throw new Error(`TOP250 当前每页最多 ${Math.ceil(250 / Number(state.page_size))} 页`);
  function setYear() {
    if (!state.year.trim()) return;
    const year = Number(state.year);
    if (!Number.isSafeInteger(year) || year < 1 || year > 9999) throw new Error('请输入有效的发行年份');
    query.year = String(year);
  }
  if (mode === 'latest' || mode === 'categories') {
    if (!r18Sorts.some(choice => choice.value === state.sort) || (mode === 'categories' && state.sort === 'update')) throw new Error('此浏览模式不支持该排序方式');
    if (state.sort === 'update' && state.year) throw new Error('最近更新排序不能叠加年份筛选');
    query.sort = state.sort; query.order = state.order; setYear();
  } else if (mode === 'top250') {
    if (state.year && type !== 'all') throw new Error('TOP250 年份榜不能同时限定影片类型');
    setYear(); if (state.ignore_watched) query.ignore_watched = state.ignore_watched;
  } else query.period = state.period;
  if (mode === 'categories') {
    const tags = [...new Set(state.tag_ids)].sort();
    if (tags.length > 5) throw new Error('最多选 5 个题材标签');
    if (tags.some(tag => !/^[0-4]:[A-Za-z0-9_-]{1,120}$/u.test(tag) || (type !== 'all' && !tag.startsWith(type + ':')))) throw new Error('题材标签与影片类型不匹配');
    if (tags.length) query.tag_ids = tags.join(',');
    const basic = r18BasicFilters.filter(value => state.main_filter.includes(value)).sort();
    if (state.main_filter.some(value => !r18BasicFilters.includes(value))) throw new Error('基础筛选条件不正确');
    if (basic.length) query.main_filter = basic.join(',');
    if (state.month) {
      const month = Number(state.month);
      if (!Number.isSafeInteger(month) || month < 1 || month > 12) throw new Error('请输入有效的发行月份');
      query.month = String(month);
    }
    if (state.duration) {
      if (!r18Durations.includes(state.duration)) throw new Error('影片时长条件不正确');
      query.duration = state.duration;
    }
  }
  return query;
}
