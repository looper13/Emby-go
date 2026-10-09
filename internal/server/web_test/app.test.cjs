const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../web/app.js'), 'utf8');
const tick = () => new Promise(setImmediate);
const deferred = () => {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};
const movie = id => ({ id, Title: 'Movie ' + id, Status: 'success' });
const detail = id => ({ movie: movie(id), actors: [], images: [], files: [] });

// A small DOM double exercises request/route behavior; layout is checked in a browser.
function harness() {
  const nodes = new Map();
  function node() {
    let html = '';
    const classes = new Set();
    return {
      firstChild: null, textContent: '', listeners: {}, dataset: {}, attributes: {}, style: {},
      classList: {
        add: value => classes.add(value), remove: value => classes.delete(value),
        toggle(value, force) {
          const add = force === undefined ? !classes.has(value) : force;
          if (add) classes.add(value); else classes.delete(value);
          return add;
        }
      },
      get innerHTML() { return html; },
      set innerHTML(value) { html = value; this.firstChild = value ? {} : null; },
      addEventListener(event, callback) { this.listeners[event] = callback; },
      querySelectorAll() { return []; },
      querySelector: selector => getNode(selector),
      getBoundingClientRect() { return { top: 5000 }; },
      insertAdjacentHTML(_, value) { html += value; },
      setAttribute(key, value) { this.attributes[key] = value; },
      getAttribute(key) { return this.attributes[key]; },
      appendChild() {}, remove() {}, replaceWith(value) { this.replacement = value; }
    };
  }
  function getNode(selector) {
    if (!nodes.has(selector)) nodes.set(selector, node());
    return nodes.get(selector);
  }
  const location = { pathname: '/admin', search: '', hash: '#items', protocol: 'http:', replace() {} };
  const timers = new Map();
  const context = vm.createContext({
    localStorage: { getItem: () => null, removeItem() {} },
    document: { querySelector: getNode, querySelectorAll: () => [], body: node(), createElement: node },
    window: { location, innerHeight: 900, scrollY: 0, addEventListener() {}, scrollTo(_, y) { this.scrollY = y; } },
    history: {
      state: {},
      replaceState(state) { this.state = state; },
      pushState(state, _, url) {
        this.state = state;
        const next = new URL(url, 'http://localhost');
        Object.assign(location, { pathname: next.pathname, search: next.search, hash: next.hash });
      }
    },
    location, console, AbortController, FormData, URLSearchParams, URL, TextEncoder, btoa,
    setTimeout(fn, ms) { const key = {}; timers.set(key, { fn, ms }); return key; },
    clearTimeout(key) { timers.delete(key); }, setInterval() {}, clearInterval() {},
    detail, movie, fetch: async () => { throw new Error('unexpected fetch'); }
  });
  const run = code => vm.runInContext(code, context);
  run(source);
  return { context, nodes, node: getNode, makeNode: node, timers, run };
}

test('late detail cannot overwrite a newer page or its title', async () => {
  const h = harness(), pending = deferred();
  h.context.pending = pending.promise;
  h.run('api = path => path.includes("/1/") ? pending : Promise.resolve(detail(2)); embyJSON = async () => null;');
  const first = h.run('page("item", 1)');
  await h.run('page("item", 2)');
  pending.resolve(detail(1));
  await first;
  assert.equal(h.node('#page-title').textContent, 'Movie 2');
  assert.match(h.node('#content').innerHTML, /Movie 2/);
});

test('late non-detail page cannot replace a newer page', async () => {
  const h = harness(), pending = deferred();
  h.context.pending = pending.promise;
  h.run('api = path => path === "/libraries" ? pending : Promise.resolve(detail(2)); embyJSON = async () => null;');
  const first = h.run('page("libraries")');
  await h.run('page("item", 2)');
  pending.resolve({ items: [] });
  await first;
  assert.match(h.node('#content').innerHTML, /Movie 2/);
});

test('navigation cancels GET reads, not writes or background progress polling', async () => {
  const h = harness(), pending = deferred(), signals = new Map();
  h.context.fetch = (url, options) => { signals.set(url, options.signal); return pending.promise; };
  h.run('activePage = { name: "item", param: "1", controller: new AbortController() }; pages.empty = async () => {}; wallState = { dirty: false };');
  const read = h.run('api("/items/1/detail")');
  const rejected = assert.rejects(read, { name: 'AbortError' });
  const write = h.run('api("/items/1/reread", { method: "POST" })');
  const progress = h.run('api("/scan/progress")');
  await h.run('page("empty")');
  assert.equal(signals.get('/api/admin/items/1/detail').aborted, true);
  assert.equal(signals.get('/api/admin/items/1/reread'), undefined);
  assert.equal(signals.get('/api/admin/scan/progress'), undefined);
  pending.resolve({ ok: true, status: 200, json: async () => ({ ok: true }) });
  await Promise.all([rejected, write, progress]);
  assert.equal(h.run('wallState.dirty'), true);
});

test('recommendations never block detail, and late recommendations stay on their page', async () => {
  const h = harness(), pending = deferred();
  h.context.pending = pending.promise;
  h.run('api = async () => detail(1); embyJSON = () => pending;');
  await h.run('page("item", 1)');
  assert.match(h.node('#content').innerHTML, /Movie 1/);
  h.run('pages.empty = async () => { content.innerHTML = "empty"; };');
  await h.run('page("empty")');
  pending.resolve({ Items: [{ Id: '9', Name: 'Late recommendation' }] });
  await tick();
  assert.equal(h.node('#item-similar').innerHTML, '');
  assert.equal(h.node('#content').innerHTML, 'empty');
});

test('recommendation request times out and releases its timer', async () => {
  const h = harness();
  h.context.fetch = (_, { signal }) => new Promise((_, reject) => {
    signal.addEventListener('abort', () => reject(signal.reason), { once: true });
  });
  h.run('activePage = { controller: new AbortController() };');
  const result = h.run('embyJSON("/Items/1/Similar")');
  [...h.timers.values()].find(timer => timer.ms === 5000).fn();
  assert.equal(await result, null);
  assert.equal(h.timers.size, 0);
});

test('failed wall stops automatic retries and permits a single explicit retry', async () => {
  const h = harness();
  let requests = 0;
  h.context.mockAPI = async url => {
    if (url === '/libraries') return { items: [] };
    requests++;
    if (requests === 1) throw new Error('503 unavailable');
    return { items: [movie(1)], total: 1 };
  };
  h.run('api = mockAPI;');
  h.node('#wall-more').getBoundingClientRect = () => ({ top: 0 });
  await h.run('page("items")');
  await tick();
  h.run('wallMaybeLoadMore(); wallMaybeLoadMore();');
  assert.equal(requests, 1);
  assert.match(h.node('#wall-more').innerHTML, /data-wall-retry/);
  h.node('#wall-more').listeners.click({ target: { closest: () => ({}) } });
  await tick();
  assert.equal(requests, 2);
  assert.equal(h.run('wallState.items.length'), 1);
  assert.equal(h.run('wallState.failed'), false);
});

test('return restores 300 cached movies; an invalidated snapshot reloads the saved count', async () => {
  const h = harness();
  let requests = 0;
  h.context.mockAPI = async url => {
    if (url === '/libraries') return { items: [] };
    requests++;
    const offset = Number(new URL(url, 'http://localhost').searchParams.get('offset'));
    return { items: Array.from({ length: 100 }, (_, i) => movie(offset + i + 1)), total: 300 };
  };
  h.run('api = mockAPI; pages.empty = async () => {};');
  await h.run('page("items");');
  await h.run('loadWallPage()');
  await h.run('loadWallPage()');
  h.context.window.scrollY = 4200;
  h.run('rememberPage("250")');
  assert.equal(h.context.history.state.wallCount, 300);
  assert.equal(h.context.history.state.focusID, '250');
  await h.run('page("empty")');
  await h.run('page("items", "", { restore: history.state })');
  assert.equal(requests, 3);
  assert.equal(h.run('wallState.items.length'), 300);
  assert.equal(h.context.window.scrollY, 4200);
  h.run('wallState.dirty = true;');
  await h.run('page("items", "", { restore: history.state })');
  assert.equal(requests, 6);
  assert.equal(h.run('wallState.items.length'), 300);
  h.run('wallState.loadedAt = Date.now() - 61000;');
  await h.run('page("items", "", { restore: history.state })');
  assert.equal(requests, 9);
  assert.equal(h.run('wallState.items.length'), 300);
});

test('a stale wall request cannot append to a newer filter', async () => {
  const h = harness(), pending = deferred();
  h.context.pending = pending.promise;
  h.run('api = async path => path === "/libraries" ? {items: []} : pending;');
  const old = h.run('page("items")');
  await tick();
  h.run('api = async path => path === "/libraries" ? {items: []} : {items: [movie(2)], total: 1};');
  await h.run('page("items")');
  pending.resolve({ items: [movie(1)], total: 1 });
  await old;
  assert.equal(h.run('wallState.items[0].id'), 2);
  assert.doesNotMatch(h.node('#wall').innerHTML, /Movie 1</);
});

test('Enter and Space on a nested action do not activate the wall card', async () => {
  const h = harness();
  h.run('api = async path => path === "/libraries" ? {items: []} : {items: [movie(1)], total: 1};');
  await h.run('page("items")');
  const opened = [];
  h.context.recordOpen = id => opened.push(id);
  h.run('openItemPage = recordOpen;');
  const card = { dataset: { play: '1' }, closest() { return this; } };
  for (const key of ['Enter', ' ']) {
    h.node('#wall').listeners.keydown({ key, target: { closest: () => card }, preventDefault() { assert.fail('nested action intercepted'); } });
  }
  assert.deepEqual(opened, []);
  h.node('#wall').listeners.keydown({ key: 'Enter', target: card, preventDefault() {} });
  assert.deepEqual(opened, [1]);
});

test('refresh preserves expanded plot, files and reading position', async () => {
  const h = harness();
  h.run('api = async () => ({ ...detail(1), movie: { ...movie(1), Plot: "plot ".repeat(100) } }); embyJSON = async () => null;');
  await h.run('page("item", 1)');
  h.node('#plot-toggle').setAttribute('aria-expanded', 'true');
  h.node('.item-details').open = true;
  h.context.window.scrollY = 900;
  await h.run('refreshPage(activePage)');
  assert.match(h.node('#content').innerHTML, /aria-expanded="true"/);
  assert.match(h.node('#content').innerHTML, /item-details" open/);
  assert.doesNotMatch(h.node('#content').innerHTML, /detail-plot is-clamped/);
  assert.equal(h.context.window.scrollY, 900);
});

test('failed refresh preserves old callbacks and can be retried', async () => {
  const h = harness();
  h.run('api = async () => detail(1); embyJSON = async () => null;');
  await h.run('page("item", 1)');
  const oldView = h.run('activePage');
  const html = h.node('#content').innerHTML;
  h.run('api = async () => { throw new Error("offline"); };');
  await h.run('refreshPage(activePage)');
  assert.equal(h.run('activePage'), oldView);
  assert.equal(oldView.controller.signal.aborted, false);
  assert.equal(h.node('#content').innerHTML, html);
  assert.equal(h.node('#page-title').textContent, 'Movie 1');
  h.run('api = async path => path.endsWith("/reread") ? {status: "success"} : detail(2);');
  const button = h.node('#detail-reread');
  await button.listeners.click({ currentTarget: button });
  assert.equal(h.node('#page-title').textContent, 'Movie 2');
  assert.equal(oldView.controller.signal.aborted, true);
  assert.equal(button.disabled, false);
});

test('navigation during refresh cancels both old and refreshing page lifetimes', async () => {
  const h = harness(), pending = deferred();
  h.context.pending = pending.promise;
  h.run('api = async () => detail(1); embyJSON = async () => null; pages.empty = async () => {};');
  await h.run('page("item", 1)');
  const old = h.run('activePage');
  h.run('api = () => pending;');
  const refresh = h.run('refreshPage(activePage)');
  const next = h.run('activePage');
  await h.run('page("empty")');
  assert.equal(old.controller.signal.aborted, true);
  assert.equal(next.controller.signal.aborted, true);
  pending.resolve(detail(1));
  await refresh;
  assert.equal(h.run('activePage.name'), 'empty');
});

test('images use versioned thumbnails while still links keep original resolution', async () => {
  const h = harness();
  h.context.fixture = { ...detail(1), movie: { ...movie(1), PosterPath: 'poster.jpg', BackdropPath: 'fanart.jpg', trailer_url: 'https://example.test/trailer.mp4' }, images: [
    { ImageType: 'Primary', ImageTag: 'poster-v2' },
    { ImageType: 'Backdrop', ImageTag: 'still-v3', ImageIndex: 0 }
  ] };
  h.run('api = async () => fixture; embyJSON = async () => null;');
  await h.run('page("item", 1)');
  const html = h.node('#content').innerHTML;
  assert.match(html, /Primary\?tag=poster-v2&amp;maxWidth=320/);
  assert.match(html, /Backdrop\/0\?tag=still-v3&amp;maxWidth=840/);
  assert.match(html, /Backdrop\/0\?tag=still-v3&amp;maxWidth=480/);
  assert.match(html, /href="\/Items\/1\/Images\/Backdrop\/0\?tag=still-v3"/);
  assert.match(h.run('wallCard({ ...movie(1), PosterPath: "p" }, {}, { Primary: "v2" })'), /Primary\?tag=v2&amp;maxWidth=320/);
});

test('a broken image uses one fallback and then a fixed placeholder', () => {
  const h = harness(), img = h.makeNode();
  Object.assign(img, { tagName: 'IMG', className: 'item-hero-poster', alt: 'Poster', dataset: { fallback: 'https://example.test/fallback.jpg' } });
  img.setAttribute('src', '/broken.jpg');
  const onError = h.node('#content').listeners.error;
  onError({ target: img });
  assert.equal(img.src, 'https://example.test/fallback.jpg');
  assert.equal(img.dataset.fallback, undefined);
  onError({ target: img });
  assert.match(img.replacement.className, /item-hero-poster image-placeholder/);
  assert.equal(img.replacement.attributes['aria-label'], 'Poster');
});

test('trailer and still sections are independent, with cover fallback only when needed', async () => {
  for (const trailer of [false, true]) for (const still of [false, true]) {
    const h = harness();
    h.context.fixture = { ...detail(1), movie: { ...movie(1), cover_url: '/cover.jpg', trailer_url: trailer ? '/trailer.mp4' : '' }, images: still ? [{ ImageType: 'Backdrop', ImageIndex: 5, ImageTag: 'tag&value' }] : [] };
    h.run('api = async () => fixture; embyJSON = async () => null;');
    await h.run('page("item", 1)');
    const html = h.node('#content').innerHTML;
    assert.equal(html.includes('id="item-trailer"'), trailer);
    assert.equal(html.includes('class="detail-artwork-grid"'), still);
    if (trailer) {
      const thumbnail = html.match(/<span class="trailer-thumb">[\s\S]*?<\/span>/)[0];
      assert.match(thumbnail, still ? /Backdrop\/5\?tag=tag%26value&amp;maxWidth=840/ : /src="\/cover.jpg"/);
      let played;
      h.context.recordTrailer = (_, url) => { played = url; };
      h.run('openTrailer = recordTrailer;');
      h.node('#item-trailer').listeners.click();
      assert.equal(played, '/trailer.mp4');
    }
  }
});

test('player errors distinguish network, decode, unsupported source and mixed content', () => {
  const h = harness();
  const network = h.run('playbackErrorMessage(2, "https://example.test/a.mp4")');
  const decode = h.run('playbackErrorMessage(3, "https://example.test/a.mp4")');
  const unsupported = h.run('playbackErrorMessage(4, "https://example.test/a.mp4")');
  assert.equal(new Set([network, decode, unsupported]).size, 3);
  h.context.location.protocol = 'https:';
  assert.match(h.run('playbackErrorMessage(2, "http://example.test/a.mp4")'), /HTTPS.*HTTP/);
});

test('player falls back once and ignores error callbacks from a destroyed player', () => {
  const h = harness(), players = [], toasts = [];
  h.context.Artplayer = class {
    constructor(options) { Object.assign(this, options); this.video = { error: { code: 2 } }; players.push(this); }
    on(_, fn) { this.error = fn; }
    play() { return Promise.resolve(); }
    destroy() { this.destroyed = true; }
  };
  h.context.recordToast = message => toasts.push(message);
  h.run('toast = recordToast; openPlayer(movie(1));');
  const first = players[0];
  first.error();
  assert.equal(first.url, '/Videos/1/proxy');
  assert.equal(toasts.length, 0);
  first.error(); first.error();
  assert.equal(toasts.length, 1);
  h.run('openTrailer(movie(2), "https://example.test/trailer.mp4")');
  first.error();
  assert.equal(first.destroyed, true);
  assert.equal(toasts.length, 1);
  players[1].error();
  assert.equal(players[1].url, 'https://example.test/trailer.mp4');
  assert.equal(toasts.length, 2);
});
