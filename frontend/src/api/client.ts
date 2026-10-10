export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly payload: unknown = null,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export interface AuthHandlers {
  getToken: () => string | null;
  onUnauthorized: (token: string) => void;
}

export interface RequestOptions extends Omit<RequestInit, 'headers'> {
  auth: 'required' | 'none';
  headers?: HeadersInit;
  json?: unknown;
  timeoutMs?: number;
  readOnly?: boolean;
}

export function isAbortError(error: unknown): boolean {
  return error instanceof Error && error.name === 'AbortError';
}

function errorMessage(payload: unknown, fallback: string): string {
  if (typeof payload === 'object' && payload !== null && 'error' in payload
    && typeof payload.error === 'string' && payload.error.trim()) {
    return payload.error;
  }
  // Proxy HTML is not useful to users, but plain-text upstream failures are.
  if (typeof payload === 'string' && payload.trim() && !payload.trim().startsWith('<')) {
    return payload.trim().slice(0, 500);
  }
  return fallback;
}

export function createApiClient(fetcher: typeof fetch = (...args) => globalThis.fetch(...args)) {
  let handlers: AuthHandlers | undefined;
  let rejectedToken: string | null = null;
  const writes = new Set<() => void>();
  function onWriteSuccess(listener: () => void): () => void {
    writes.add(listener);
    return () => { writes.delete(listener); };
  }

  function setAuthHandlers(next: AuthHandlers): () => void {
    handlers = next;
    rejectedToken = null;
    return () => {
      if (handlers === next) {
        handlers = undefined;
        rejectedToken = null;
      }
    };
  }

  async function request(path: string, options: RequestOptions): Promise<unknown> {
    if (!path.startsWith('/') || path.startsWith('//') || /[\\\u0000-\u0020]/u.test(path)) {
      throw new TypeError('API requests require a same-origin absolute path');
    }
    const { auth, json, timeoutMs, readOnly, headers: inputHeaders, ...init } = options;
    if (json !== undefined && init.body != null) {
      throw new TypeError('Use either json or body, not both');
    }
    if (timeoutMs !== undefined && (!Number.isFinite(timeoutMs) || timeoutMs <= 0)) {
      throw new TypeError('timeoutMs must be positive');
    }

    const authHandlers = handlers;
    const sentToken = auth === 'required' ? authHandlers?.getToken() ?? null : null;
    if (auth === 'required' && !sentToken) throw new ApiError('登录已失效', 401);
    const headers = new Headers(inputHeaders);
    if (sentToken) headers.set('X-Emby-Token', sentToken);
    else headers.delete('X-Emby-Token');
    if (json !== undefined) {
      init.body = JSON.stringify(json);
      if (!headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
    }
    if (init.body instanceof FormData) headers.delete('Content-Type');

    // No implicit page scope or default timeout, including for long-running POSTs.
    const parent = init.signal;
    const controller = timeoutMs === undefined ? undefined : new AbortController();
    const forwardAbort = () => controller?.abort(parent?.reason);
    let timer: ReturnType<typeof setTimeout> | undefined;
    if (controller) {
      if (parent?.aborted) forwardAbort();
      else parent?.addEventListener('abort', forwardAbort, { once: true });
      timer = setTimeout(() => controller.abort(new DOMException('请求超时', 'TimeoutError')), timeoutMs);
    }
    const signal = controller?.signal ?? parent;
    try {
      signal?.throwIfAborted();
      const response = await fetcher(path, { ...init, headers, signal });
      signal?.throwIfAborted();
      if (response.status === 401 && sentToken && authHandlers === handlers
        && authHandlers?.getToken() === sentToken && rejectedToken !== sentToken) {
        rejectedToken = sentToken;
        authHandlers.onUnauthorized(sentToken);
      }
      if (response.ok && !readOnly && !['GET', 'HEAD'].includes((init.method ?? 'GET').toUpperCase())) {
        for (const listener of writes) listener();
      }
      if (response.status === 204) return null;

      const text = await response.text();
      signal?.throwIfAborted();
      let payload: unknown = null;
      if (text) {
        try { payload = JSON.parse(text); }
        catch {
          if (response.ok) throw new ApiError('服务返回了无效的 JSON', response.status);
          payload = text;
        }
      }
      if (!response.ok) {
        const fallback = response.status === 401 && auth === 'required'
          ? '登录已失效' : `请求失败 (${response.status})`;
        throw new ApiError(errorMessage(payload, fallback), response.status, payload);
      }
      return payload;
    } catch (error) {
      // Some fetch implementations throw a generic error on abort.
      signal?.throwIfAborted();
      throw error;
    } finally {
      if (timer !== undefined) clearTimeout(timer);
      parent?.removeEventListener('abort', forwardAbort);
    }
  }

  return { request, setAuthHandlers, onWriteSuccess };
}

export const apiClient = createApiClient();
