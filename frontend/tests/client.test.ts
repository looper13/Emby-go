import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, createApiClient } from '../src/api/client';
import { parseItemsTotal, parseLibraries, parseLogin, parseStatus } from '../src/api/contracts';
import { createPageRequestScope } from '../src/composables/usePageRequest';
import librariesFixture from '../../vue-migration/fixtures/json/libraries.json';
import itemsFixture from '../../vue-migration/fixtures/json/items-wall.json';

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), {
  status, headers: { 'Content-Type': 'application/json' },
});

afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers(); });

describe('API client', () => {
  it('preserves mixed-case DTOs and supports Headers without case-sensitive merging', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(json(itemsFixture));
    const client = createApiClient(fetcher);
    client.setAuthHandlers({ getToken: () => 'current-token', onUnauthorized: vi.fn() });
    const headers = new Headers({ 'x-custom': 'value', 'x-emby-token': 'caller-token' });
    const result = await client.request('/api/admin/items', { auth: 'required', headers });
    expect(result).toEqual(itemsFixture);
    const sent = new Headers(fetcher.mock.calls[0]?.[1]?.headers);
    expect(sent.get('X-Custom')).toBe('value');
    expect(sent.get('X-Emby-Token')).toBe('current-token');
    expect(headers.get('X-Emby-Token')).toBe('caller-token');
    expect(sent.has('Content-Type')).toBe(false);
  });

  it('sends public credentials without an existing token and retains JSON headers', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 204 }));
    const client = createApiClient(fetcher);
    const unauthorized = vi.fn();
    client.setAuthHandlers({ getToken: () => 'private-token', onUnauthorized: unauthorized });
    await client.request('/api/auth/initialize', {
      auth: 'none', method: 'POST', json: { Username: 'admin', Pw: 'test-password' },
      headers: new Headers({ 'content-type': 'application/json; charset=utf-8', 'X-Emby-Token': 'private-token' }),
    });
    const sent = new Headers(fetcher.mock.calls[0]?.[1]?.headers);
    expect(sent.has('X-Emby-Token')).toBe(false);
    expect(sent.get('Content-Type')).toBe('application/json; charset=utf-8');
    expect(fetcher.mock.calls[0]?.[1]?.body).toBe(JSON.stringify({ Username: 'admin', Pw: 'test-password' }));
    expect(unauthorized).not.toHaveBeenCalled();
  });

  it('does not read a 204 response body', async () => {
    const response = new Response(null, { status: 204 });
    const read = vi.spyOn(response, 'text');
    const client = createApiClient(vi.fn<typeof fetch>().mockResolvedValue(response));
    expect(await client.request('/api/auth/initialize', { auth: 'none', method: 'POST' })).toBeNull();
    expect(read).not.toHaveBeenCalled();
  });

  it.each([
    { response: json({ error: '管理员已初始化' }, 409), message: '管理员已初始化', status: 409 },
    { response: new Response('upstream unavailable', { status: 503 }), message: 'upstream unavailable', status: 503 },
    { response: new Response('<html>proxy failure</html>', { status: 502 }), message: '请求失败 (502)', status: 502 },
    { response: new Response(null, { status: 500 }), message: '请求失败 (500)', status: 500 },
  ])('handles JSON, text, HTML and empty errors', async ({ response, message, status }) => {
    const client = createApiClient(vi.fn<typeof fetch>().mockResolvedValue(response));
    await expect(client.request('/api/auth/status', { auth: 'none' })).rejects.toMatchObject({ message, status });
  });

  it('does not turn public login 401 into session expiration', async () => {
    const unauthorized = vi.fn();
    const client = createApiClient(vi.fn<typeof fetch>().mockResolvedValue(json({ error: '账号或密码错误' }, 401)));
    client.setAuthHandlers({ getToken: () => 'existing-token', onUnauthorized: unauthorized });
    await expect(client.request('/Users/AuthenticateByName', { auth: 'none', method: 'POST' }))
      .rejects.toMatchObject({ message: '账号或密码错误', status: 401 });
    expect(unauthorized).not.toHaveBeenCalled();
  });

  it('lets the browser own the FormData boundary, even with caller Content-Type', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(null, { status: 204 }));
    const client = createApiClient(fetcher);
    const body = new FormData();
    body.append('image', new Blob(['image'], { type: 'image/png' }), 'poster.png');
    await client.request('/upload', {
      auth: 'none', method: 'POST', body, headers: { 'Content-Type': 'multipart/form-data', 'X-Custom': 'kept' },
    });
    expect(fetcher.mock.calls[0]?.[1]?.body).toBe(body);
    const sent = new Headers(fetcher.mock.calls[0]?.[1]?.headers);
    expect(sent.has('Content-Type')).toBe(false);
    expect(sent.get('X-Custom')).toBe('kept');
  });

  it('rejects malformed successful JSON and invalid API paths', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response('<html>not an API</html>'));
    const client = createApiClient(fetcher);
    await expect(client.request('/api/auth/status', { auth: 'none' })).rejects.toBeInstanceOf(ApiError);
    for (const path of ['//example.com/api', 'https://example.com/api', '/\\example.com/api']) {
      await expect(client.request(path, { auth: 'none' })).rejects.toBeInstanceOf(TypeError);
    }
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('deduplicates concurrent 401 responses for the current token', async () => {
    const fetcher = vi.fn<typeof fetch>().mockImplementation(async () => json({ error: 'unauthorized' }, 401));
    const client = createApiClient(fetcher);
    const unauthorized = vi.fn();
    client.setAuthHandlers({ getToken: () => 'old-token', onUnauthorized: unauthorized });
    const results = await Promise.allSettled([
      client.request('/api/admin/status', { auth: 'required' }),
      client.request('/api/admin/libraries', { auth: 'required' }),
      client.request('/api/admin/scan/progress', { auth: 'required' }),
    ]);
    expect(results.every((result) => result.status === 'rejected')).toBe(true);
    expect(unauthorized).toHaveBeenCalledExactlyOnceWith('old-token');
  });

  it('never invalidates a newer token on a delayed old-token 401', async () => {
    const response = deferred<Response>();
    const client = createApiClient(vi.fn<typeof fetch>().mockReturnValue(response.promise));
    let token = 'old-token';
    const unauthorized = vi.fn();
    client.setAuthHandlers({ getToken: () => token, onUnauthorized: unauthorized });
    const request = client.request('/api/admin/status', { auth: 'required' });
    token = 'new-token';
    response.resolve(json({ error: 'unauthorized' }, 401));
    await expect(request).rejects.toMatchObject({ status: 401 });
    expect(unauthorized).not.toHaveBeenCalled();
  });

  it('honors cancellation before fetching and after body decoding has started', async () => {
    const body = deferred<string>();
    const response = json({ initialized: true });
    vi.spyOn(response, 'text').mockReturnValue(body.promise);
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response);
    const client = createApiClient(fetcher);
    const controller = new AbortController();
    const request = client.request('/api/auth/status', { auth: 'none', signal: controller.signal });
    await Promise.resolve();
    controller.abort();
    body.resolve('{"initialized":true}');
    await expect(request).rejects.toMatchObject({ name: 'AbortError' });
    await expect(client.request('/api/auth/status', { auth: 'none', signal: controller.signal }))
      .rejects.toMatchObject({ name: 'AbortError' });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it('cancels explicit page reads without canceling writes or progress', async () => {
    const response = deferred<Response>();
    const fetcher = vi.fn<typeof fetch>().mockReturnValue(response.promise);
    const client = createApiClient(fetcher);
    client.setAuthHandlers({ getToken: () => 'token', onUnauthorized: vi.fn() });
    const scope = createPageRequestScope();
    const page = scope.begin();
    const read = client.request('/api/admin/status', { auth: 'required', signal: page.signal });
    const write = client.request('/api/admin/scan', { auth: 'required', method: 'POST' });
    const progress = client.request('/api/admin/scan/progress', { auth: 'required' });
    const next = scope.begin();
    expect(page.signal.aborted).toBe(true);
    expect(page.isCurrent()).toBe(false);
    expect(next.isCurrent()).toBe(true);
    expect(fetcher.mock.calls[1]?.[1]?.signal).toBeUndefined();
    expect(fetcher.mock.calls[2]?.[1]?.signal).toBeUndefined();
    response.resolve(new Response(null, { status: 204 }));
    await expect(read).rejects.toMatchObject({ name: 'AbortError' });
    await expect(write).resolves.toBeNull();
    await expect(progress).resolves.toBeNull();
    scope.dispose();
    expect(next.isCurrent()).toBe(false);
    expect(scope.begin().signal.aborted).toBe(true);
  });

  it('only times out when requested and cleans up timeout timers', async () => {
    vi.useFakeTimers();
    const fetcher = vi.fn<typeof fetch>().mockImplementation((_path, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () => reject(init.signal?.reason), { once: true });
    }));
    const client = createApiClient(fetcher);
    const request = client.request('/api/auth/status', { auth: 'none', timeoutMs: 50 });
    const assertion = expect(request).rejects.toMatchObject({ name: 'TimeoutError' });
    await vi.advanceTimersByTimeAsync(50);
    await assertion;
    expect(vi.getTimerCount()).toBe(0);

    const successful = createApiClient(vi.fn<typeof fetch>().mockResolvedValue(json({ initialized: true })));
    await successful.request('/api/auth/status', { auth: 'none', timeoutMs: 1000 });
    expect(vi.getTimerCount()).toBe(0);
  });

  it('does not retry failed writes or turn network failures into 401', async () => {
    const fetcher = vi.fn<typeof fetch>().mockRejectedValue(new TypeError('Failed to fetch'));
    const client = createApiClient(fetcher);
    const unauthorized = vi.fn();
    client.setAuthHandlers({ getToken: () => 'token', onUnauthorized: unauthorized });
    await expect(client.request('/api/admin/scan', { auth: 'required', method: 'POST' }))
      .rejects.toThrow('Failed to fetch');
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(unauthorized).not.toHaveBeenCalled();
  });
});

describe('raw DTO boundaries', () => {
  it('accepts the actual empty Go slices encoded as null', () => {
    expect(parseLibraries({ items: null, total: 0 })).toEqual({ items: null, total: 0 });
    expect(parseItemsTotal({ items: null, total: 0, limit: 1, offset: 0, userdata: {}, image_tags: {} })).toBe(0);
  });
  it('accepts the collected library DTO and rejects invented casing', () => {
    expect(parseLibraries(librariesFixture)).toEqual(librariesFixture);
    expect(() => parseLibraries({ items: [{ id: 1, name: 'wrong', path: '/media' }], total: 1 })).toThrow();
    expect(() => parseLogin({ accessToken: 'wrong', user: {} })).toThrow();
    expect(() => parseStatus({ Success: 1, Manual: 0, Pending: 0, Incompatible: 0 })).toThrow();
  });
});
