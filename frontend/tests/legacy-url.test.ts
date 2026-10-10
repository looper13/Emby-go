import { describe, expect, it, vi } from 'vitest';
import {
  convertLegacyUrl, isValidItemId, replaceLegacyUrl, resolveLoginTarget, sanitizeReturnTo,
} from '../src/router/legacy-url';

function location(url: string) {
  return new URL(url, 'http://localhost');
}

describe('legacy URL conversion', () => {
  it('moves only allowlisted external query fields and encodes Chinese and symbols once', () => {
    const old = '/admin-vue?library_id=3&search=%E7%A4%BA%E4%BE%8B%20%2B%20%26&api_key=secret&unknown=1#items';
    const converted = convertLegacyUrl(location(old));
    expect(converted).toBe('/admin-vue#/items?library_id=3&search=%E7%A4%BA%E4%BE%8B+%2B+%26');
    expect(convertLegacyUrl(location(converted!))).toBeNull();
  });

  it('keeps the current entry and detail filter context', () => {
    expect(convertLegacyUrl(location('/web/index.html?library_id=3#item/123')))
      .toBe('/web/index.html#/item/123?library_id=3');
  });

  it('prefers query inside an old hash and leaves already-new URLs alone', () => {
    expect(convertLegacyUrl(location('/admin-vue?search=outer&library_id=3#items?search=inner')))
      .toBe('/admin-vue#/items?search=inner&library_id=3');
    expect(convertLegacyUrl(location('/admin-vue?search=outer#/items?search=inner'))).toBeNull();
  });

  it.each(['', '#unknown', '#item/0', '#item/-1', '#item/1.2', '#item/person-a', '#item/9223372036854775808'])
  ('falls back for absent/invalid legacy pages: %s', (hash) => {
    expect(convertLegacyUrl(location(`/admin-vue${hash}`))).toBe('/admin-vue#/overview');
  });

  it('does not turn encoded paths into valid route IDs', () => {
    expect(convertLegacyUrl(location('/admin-vue#item/%31'))).toBe('/admin-vue#/overview');
    expect(isValidItemId('9223372036854775807')).toBe(true);
    expect(isValidItemId('01')).toBe(false);
  });

  it('replaces once without losing existing history state', () => {
    window.history.replaceState({ position: 4, custom: { value: 1 } }, '', '/admin-vue?search=test#items');
    const spy = vi.spyOn(window.history, 'replaceState');
    const length = window.history.length;
    expect(replaceLegacyUrl()).toBe(true);
    expect(replaceLegacyUrl()).toBe(false);
    expect(spy).toHaveBeenCalledTimes(1);
    expect(window.history.state).toEqual({ position: 4, custom: { value: 1 } });
    expect(window.history.length).toBe(length);
    expect(window.location.pathname + window.location.hash).toBe('/admin-vue#/items?search=test');
    spy.mockRestore();
  });

  it('drops invalid numeric filter IDs while preserving other filters', () => {
    expect(convertLegacyUrl(location('/admin-vue?library_id=bad&search=ok&person=%E4%B8%AD%E6%96%87#items')))
      .toBe('/admin-vue#/items?search=ok&person=%E4%B8%AD%E6%96%87');
  });
});

describe('authentication destinations', () => {
  it.each([
    '//example.com', 'https://example.com', 'javascript:alert(1)', '/\\example.com',
    '/%2f%2fexample.com', '/items/../overview', '/item/0', '/item/-1', '/item/1.5',
    '/item/1/other', '/item/9223372036854775808', '/login', '/unknown', '/overview#external',
    ' /overview', '\n/overview', null, ['/overview'],
  ])('rejects external or invalid returnTo: %s', (target) => {
    expect(sanitizeReturnTo(target)).toBeNull();
    expect(resolveLoginTarget(target)).toBe('/overview');
  });

  it('preserves valid routes and text queries without interpreting URLs in search text', () => {
    expect(sanitizeReturnTo('/item/123?library_id=3')).toBe('/item/123?library_id=3');
    expect(sanitizeReturnTo('/items?search=中文 +&api_key=secret')).toBe('/items?search=%E4%B8%AD%E6%96%87++');
    expect(sanitizeReturnTo('/overview')).toBe('/overview');
    expect(sanitizeReturnTo('/items?search=https%3A%2F%2Fexample.com')).toBe('/items?search=https%3A%2F%2Fexample.com');
  });

  it('converts a legacy login returnTo only when it is an internal allowed route', () => {
    expect(convertLegacyUrl(location('/admin-vue?returnTo=%2Fitem%2F123#login')))
      .toBe('/admin-vue#/login?returnTo=%2Fitem%2F123');
    expect(convertLegacyUrl(location('/admin-vue?returnTo=%2F%2Fexample.com#login')))
      .toBe('/admin-vue#/login');
  });
});
