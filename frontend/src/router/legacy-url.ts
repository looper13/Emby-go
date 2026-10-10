const pages = new Set([
  'overview', 'libraries', 'items', 'manual', 'settings', 'apikeys',
  'scrape', 'scheduled', 'tasks', 'probe',
]);
const filterKeys = new Set([
  'library_id', 'status', 'search', 'sort', 'order', 'source_protocol',
  'genre', 'tag', 'studio', 'person', 'collection', 'scrape', 'limit', 'offset',
]);

export function isValidItemId(value: string): boolean {
  return /^[1-9]\d{0,18}$/u.test(value) && BigInt(value) <= 9223372036854775807n;
}

function allowedPath(path: string, login: boolean): boolean {
  return path.startsWith('/') && (pages.has(path.slice(1)) || (login && path === '/login')
    || (path.startsWith('/item/') && isValidItemId(path.slice(6))));
}

function filteredQuery(input: URLSearchParams): URLSearchParams {
  const output = new URLSearchParams();
  for (const [key, value] of input) {
    if (!filterKeys.has(key) || output.has(key)) continue;
    if (key === 'library_id' && value !== '0' && !isValidItemId(value)) continue;
    if ((key === 'limit' || key === 'offset') && !/^\d+$/u.test(value)) continue;
    output.set(key, value);
  }
  return output;
}

function joinRoute(path: string, query: URLSearchParams): string {
  return path + (query.size ? `?${query.toString()}` : '');
}

export function sanitizeReturnTo(value: unknown): string | null {
  if (typeof value !== 'string' || !value.startsWith('/') || value.startsWith('//')
    || /[\\#\u0000-\u001f\u007f]/u.test(value)) return null;
  const separator = value.indexOf('?');
  const path = separator < 0 ? value : value.slice(0, separator);
  if (!allowedPath(path, false)) return null;
  const query = new URLSearchParams(separator < 0 ? '' : value.slice(separator + 1));
  return joinRoute(path, filteredQuery(query));
}

export function resolveLoginTarget(returnTo: unknown): string {
  return sanitizeReturnTo(returnTo) ?? '/overview';
}

type LegacyLocation = Pick<Location, 'pathname' | 'search' | 'hash'>;

export function convertLegacyUrl(location: LegacyLocation): string | null {
  // Router owns new-style URLs, including their query precedence and history.
  if (location.hash.startsWith('#/')) return null;
  const hash = location.hash.replace(/^#/u, '');
  const separator = hash.indexOf('?');
  const name = separator < 0 ? hash : hash.slice(0, separator);
  const candidate = `/${name || 'overview'}`;
  const path = allowedPath(candidate, true) ? candidate : '/overview';
  const input = new URLSearchParams(location.search);
  const inner = new URLSearchParams(separator < 0 ? '' : hash.slice(separator + 1));
  for (const [key, value] of inner) input.set(key, value);
  const query = filteredQuery(input);
  if (path === '/login') {
    const returnTo = sanitizeReturnTo(input.get('returnTo'));
    if (returnTo) query.set('returnTo', returnTo);
  }
  return `${location.pathname}#${joinRoute(path, query)}`;
}

export function replaceLegacyUrl(
  location: LegacyLocation = window.location,
  history: Pick<History, 'state' | 'replaceState'> = window.history,
): boolean {
  const converted = convertLegacyUrl(location);
  if (converted === null) return false;
  history.replaceState(history.state, '', converted);
  return true;
}
