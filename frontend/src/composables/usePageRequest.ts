import { onScopeDispose } from 'vue';

export function createPageRequestScope() {
  let controller: AbortController | undefined;
  let disposed = false;

  function cancel() {
    controller?.abort();
    controller = undefined;
  }

  function begin() {
    cancel();
    const current = new AbortController();
    controller = current;
    if (disposed) current.abort();
    return {
      signal: current.signal,
      isCurrent: () => !disposed && controller === current && !current.signal.aborted,
    };
  }

  function dispose() {
    disposed = true;
    cancel();
  }

  return { begin, cancel, dispose };
}

// Call begin() when parameters change, pass its signal only to page reads, and
// check isCurrent() before committing either data or errors after every await.
export function usePageRequest() {
  const scope = createPageRequestScope();
  onScopeDispose(scope.dispose);
  return scope;
}
