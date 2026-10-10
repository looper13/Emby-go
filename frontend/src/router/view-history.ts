import type { InjectionKey } from 'vue';
import type { LocationQuery, Router, RouterHistory } from 'vue-router';

export interface ViewPosition { scrollY: number; wallCount: number; focusID: string }
export function readViewPosition(value: unknown): ViewPosition | null {
  if (typeof value !== 'object' || value === null) return null;
  const v = value as Partial<ViewPosition>;
  return typeof v.scrollY === 'number' && Number.isFinite(v.scrollY) && v.scrollY >= 0
    && typeof v.wallCount === 'number' && Number.isSafeInteger(v.wallCount) && v.wallCount >= 0
    && typeof v.focusID === 'string' ? v as ViewPosition : null;
}

export function saveViewPosition(position: ViewPosition) {
  window.history.replaceState({ ...window.history.state, __embyView: position }, '');
}

export function createViewHistory(router: Router, history: RouterHistory, wallCount: () => number) {
  let pop = false;
  let target: ViewPosition | null = null;
  const visited = new Set<number>();
  const stop = history.listen(() => { pop = true; });
  router.beforeEach((_to, from) => {
    target = pop ? readViewPosition(history.state.__embyView) : null;
    if (!pop && from.matched.length) {
      const card = document.activeElement?.closest<HTMLElement>('.wall-card');
      saveViewPosition({ scrollY: window.scrollY, wallCount: from.name === 'items' ? wallCount() : 0, focusID: card?.dataset.play ?? '' });
    }
    pop = false;
  });
  router.afterEach((_to, _from, failure) => {
    if (!failure && typeof history.state.position === 'number') visited.add(history.state.position);
  });
  function backFromItem(query: LocationQuery) {
    const position = history.state.position;
    if (typeof position === 'number' && visited.has(position - 1)) router.back();
    else void router.replace({ path: '/items', query });
  }
  return { target: () => target, backFromItem, dispose: stop };
}

export type ViewHistory = ReturnType<typeof createViewHistory>;
export const viewHistoryKey: InjectionKey<ViewHistory> = Symbol('view-history');
export const viewHistories = new WeakMap<Router, ViewHistory>();
