import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomBytes, createHash } from 'node:crypto';
import { once } from 'node:events';
import fs from 'node:fs/promises';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
export const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const forbiddenPorts = new Set([6379, 18080, 6391, 18098, 18099]);

export async function executable(name, supplied) {
  const candidates = supplied ? [path.resolve(supplied)] : (process.env.PATH || '').split(path.delimiter)
    .map(dir => path.join(dir, name + (process.platform === 'win32' ? '.exe' : '')));
  for (let candidate of candidates) {
    try {
      if (!(await fs.stat(candidate)).isFile()) continue;
      // Launch the executable, not Scoop's shim and its extra child process.
      if (process.platform === 'win32') {
        const shim = await fs.readFile(candidate.replace(/\.exe$/i, '.shim'), 'utf8').catch(() => '');
        const target = /^path\s*=\s*"([^"]+)"\s*$/m.exec(shim);
        if (target) candidate = target[1];
      }
      return await fs.realpath(candidate);
    } catch { /* Try the next PATH entry. */ }
  }
  throw new Error(`Executable not found: ${supplied || name}`);
}

async function unusedPort() {
  for (;;) {
    const server = net.createServer();
    server.listen(0, '127.0.0.1');
    await once(server, 'listening');
    const port = server.address().port;
    await new Promise(resolve => server.close(resolve));
    if (!forbiddenPorts.has(port)) return port;
  }
}

export async function portOpen(port) {
  return new Promise(resolve => {
    const socket = net.createConnection({ host: '127.0.0.1', port });
    const finish = open => { socket.destroy(); resolve(open); };
    socket.once('connect', () => finish(true));
    socket.once('error', () => finish(false));
    socket.setTimeout(500, () => finish(false));
  });
}

async function waitFor(check, children, label, timeout = 20000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const dead = children.find(child => child.error || child.exited);
    if (dead) throw new Error(`${dead.name} exited during ${label}: ${dead.error?.message || dead.log.slice(-1500)}`);
    try { if (await check()) return; } catch { /* Readiness can race startup. */ }
    await sleep(100);
  }
  throw new Error(`Timed out waiting for ${label}`);
}

async function redisPing(port, password) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({ host: '127.0.0.1', port });
    let reply = '';
    socket.on('error', reject);
    socket.setTimeout(800, () => socket.destroy(new Error('Redis readiness timeout')));
    socket.on('connect', () => socket.write(`*2\r\n$4\r\nAUTH\r\n$${password.length}\r\n${password}\r\n*1\r\n$4\r\nPING\r\n`));
    socket.on('data', chunk => {
      reply += chunk;
      if (reply.includes('+PONG\r\n') || reply.startsWith('-')) {
        socket.destroy();
        resolve(reply.startsWith('+OK\r\n') && reply.includes('+PONG\r\n'));
      }
    });
  });
}

export async function snapshotTree(directory) {
  const result = {};
  async function visit(dir) {
    for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name);
      if (entry.isDirectory()) await visit(file);
      else result[path.relative(directory, file).replaceAll('\\', '/')] = createHash('sha256').update(await fs.readFile(file)).digest('hex');
    }
  }
  await visit(directory);
  return result;
}

export function makeRedactor(root, secrets = []) {
  return value => {
    let text = typeof value === 'string' ? value : JSON.stringify(value);
    for (const secret of secrets.filter(Boolean).sort((a, b) => b.length - a.length)) text = text.split(secret).join('REDACTED');
    // Go authentication tokens are 24 random bytes encoded as 48 hex characters.
    // This also covers login responses lost during a browser navigation.
    text = text.replace(/\b[0-9a-f]{48}\b/gi, 'TOKEN');
    const unixRoot = root.replaceAll('\\', '/');
    const variants = [unixRoot, unixRoot.replace(/^([A-Z]):/i, (_, drive) => `/cygdrive/${drive.toLowerCase()}`), root];
    for (let i = 0; i < 3; i++) variants.push(variants.at(-1).replaceAll('\\', '\\\\'));
    for (const variant of variants.sort((a, b) => b.length - a.length)) text = text.split(variant).join('TEMP_LIVE');
    text = text.replace(/([?&](?:api_key|ApiKey|X-Emby-Token)=)[^&"\s<>]+/g, '$1TOKEN');
    return text;
  };
}

/** Each call creates a NEW environment. No existing database, config, or token is read. */
export async function startLiveEnvironment(options = {}) {
  const { binaryPath, redisPath, goPath, ffprobePath, faultAfter, mediaCount = 307 } = options;
  assert(Number.isInteger(mediaCount) && mediaCount >= 307 && mediaCount <= 10000, 'Invalid media fixture count');
  assert(!faultAfter || ['root', 'redis', 'media', 'go', 'seed'].includes(faultAfter), 'Unknown fault stage');
  const parent = await fs.realpath(os.tmpdir());
  const root = await fs.mkdtemp(path.join(parent, 'emby-go-vue-live-'));
  const owner = randomBytes(24).toString('hex');
  const children = [];
  const ports = {};
  const credentials = { username: `live_${randomBytes(5).toString('hex')}`, password: randomBytes(24).toString('base64url') };
  const redisPassword = randomBytes(24).toString('hex');
  const secrets = [credentials.username, credentials.password, redisPassword];
  const redact = makeRedactor(root, secrets);
  let closing;
  let marked = false;
  const onSignal = () => { void close().then(() => { process.exitCode = 130; }, () => { process.exitCode = 1; }); };

  function launch(name, binary, args, ipc = false, cwd = root) {
    assert(!closing, 'Environment is closing or closed');
    const child = { name, binary, args, exited: false, error: null, log: '' };
    const env = { ...process.env };
    delete env.EMBY_CONFIG;
    const proc = spawn(binary, args, { cwd, env, shell: false, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe', ...(ipc ? ['ipc'] : [])] });
    child.proc = proc;
    children.push(child);
    proc.on('error', error => { child.error = error; });
    proc.on('exit', () => { child.exited = true; });
    for (const stream of [proc.stdout, proc.stderr]) stream.on('data', data => { child.log = (child.log + data).slice(-2000000); });
    child.done = new Promise(resolve => proc.once('close', resolve));
    return child;
  }

  async function close() {
    if (closing) return closing;
    closing = (async () => {
      process.off('SIGINT', onSignal);
      process.off('SIGTERM', onSignal);
      const errors = [];
      for (const child of [...children].reverse()) {
        try {
          if (!child.exited && !child.error) {
            // Only the still-owned child PID is targeted; never kill by process name.
            if (process.platform === 'win32') {
              const killer = spawn('taskkill.exe', ['/PID', String(child.proc.pid), '/T', '/F'], { windowsHide: true, shell: false, stdio: 'ignore' });
              await once(killer, 'close');
            } else child.proc.kill('SIGTERM');
          }
          let timer;
          try { await Promise.race([child.done, new Promise(resolve => { timer = setTimeout(resolve, 8000); })]); }
          finally { clearTimeout(timer); }
          if (!child.exited && !child.error) throw new Error(`${child.name} did not exit`);
        } catch (error) { errors.push(error.message); }
      }
      const portStates = {};
      for (const [name, port] of Object.entries(ports)) {
        portStates[name] = { port, released: !await portOpen(port) };
        if (!portStates[name].released) errors.push(`${name} port ${port} is still listening`);
      }
      if (!errors.length) {
        const actual = await fs.realpath(root);
        assert.equal(path.dirname(actual), parent, 'Cleanup must stay inside its original temp parent');
        assert(path.basename(actual).startsWith('emby-go-vue-live-'), 'Unexpected cleanup path');
        assert(!(await fs.lstat(root)).isSymbolicLink(), 'Refuse symlink cleanup');
        if (marked) assert.equal(await fs.readFile(path.join(root, '.owner'), 'utf8'), owner, 'Ownership marker changed');
        await fs.rm(actual, { recursive: true, maxRetries: 5, retryDelay: 150 });
      }
      const result = { root, rootRemoved: !await fs.stat(root).then(() => true, () => false), processes: children.map(c => ({ name: c.name, pid: c.proc.pid, exited: c.exited || Boolean(c.error) })), ports: portStates, errors };
      if (errors.length) throw Object.assign(new Error(errors.join('; ')), { cleanup: result });
      return result;
    })();
    return closing;
  }

  function fail(stage) {
    if (faultAfter === stage) throw new Error(`Injected startup failure after ${stage}`);
  }

  try {
    await fs.writeFile(path.join(root, '.owner'), owner, { flag: 'wx' });
    marked = true;
    process.once('SIGINT', onSignal);
    process.once('SIGTERM', onSignal);
    fail('root');
    for (const dir of ['redis', 'media', 'source']) await fs.mkdir(path.join(root, dir));
    const binary = path.join(root, process.platform === 'win32' ? 'emby-go-live.exe' : 'emby-go-live');
    if (binaryPath) await fs.copyFile(path.resolve(binaryPath), binary);
    else {
      const build = launch('go-build', await executable('go', goPath), ['build', '-o', binary, './cmd/metatube'], false, repoRoot);
      await build.done;
      if (build.error || build.proc.exitCode !== 0) throw new Error(`Go build failed: ${build.error?.message || build.log}`);
      children.splice(children.indexOf(build), 1);
    }
    if (process.platform !== 'win32') await fs.chmod(binary, 0o700);
    const binarySHA256 = createHash('sha256').update(await fs.readFile(binary)).digest('hex');
    const ffprobe = await executable('ffprobe', ffprobePath);
    ports.redis = await unusedPort();
    const redisConfig = path.join(root, `redis-${path.basename(root)}.conf`);
    await fs.writeFile(redisConfig, `bind 127.0.0.1\nport ${ports.redis}\ndir "redis"\nsave ""\nappendonly no\nrequirepass ${redisPassword}\n`);
    // Relative paths also work with the Cygwin Redis distribution on Windows.
    launch('redis', await executable('redis-server', redisPath), [path.basename(redisConfig)]);
    await waitFor(() => redisPing(ports.redis, redisPassword), children, 'dedicated Redis authentication');
    fail('redis');
    await fs.copyFile(path.join(repoRoot, 'internal/server/testdata/probe-sample.mp4'), path.join(root, 'source', 'sample.mp4'));
    const media = launch('media', process.execPath, [fileURLToPath(new URL('./media-server.mjs', import.meta.url)), path.join(root, 'source', 'sample.mp4')], true);
    media.proc.on('message', message => { if (message?.port) ports.media = message.port; });
    await waitFor(() => Boolean(ports.media), children, 'media source');
    assert(!forbiddenPorts.has(ports.media));
    const mediaURL = `http://127.0.0.1:${ports.media}`;
    fail('media');
    const { seedMedia } = await import('./seed-media.mjs');
    await seedMedia(path.join(root, 'media'), mediaURL, mediaCount);
    ports.go = await unusedPort();
    assert.notEqual(ports.go, ports.redis);
    const serverID = randomBytes(16).toString('hex');
    const config = { port: ports.go, db_path: path.join(root, 'emby-go.db'), server_name: 'Vue Live Fixture', server_id: serverID, debug: false, redis_addr: `127.0.0.1:${ports.redis}`, redis_password: redisPassword, redis_db: 0, disable_library_monitor: true, library_monitor_mode: 'polling', ffprobe_path: ffprobe, probe_timeout_seconds: 5, probe_concurrency: 1 };
    const configPath = path.join(root, 'config.yaml');
    // JSON is a YAML subset; let the existing Go YAML parser consume it.
    await fs.writeFile(configPath, JSON.stringify(config, null, 2));
    let goChild = launch('go', binary, ['-c', configPath]);
    const baseURL = `http://127.0.0.1:${ports.go}`;
    const raw = async (pathname, init = {}) => {
      assert(!closing, 'Environment is closing or closed');
      assert(pathname.startsWith('/') && !pathname.startsWith('//'), 'Only paths on this environment are allowed');
      const target = new URL(pathname, baseURL);
      assert.equal(target.origin, baseURL);
      return fetch(target, { ...init, redirect: 'error', signal: AbortSignal.timeout(30000) });
    };
    await waitFor(async () => {
      const response = await raw('/System/Info/Public');
      return response.ok && (await response.json()).Id === serverID;
    }, children, 'owned Go ServerId');
    fail('go');
    let token = '';
    async function request(pathname, { method = 'GET', body, auth = true, expected = 200 } = {}) {
      // Re-check instance identity before writes, including initial account creation.
      if (method !== 'GET' && method !== 'HEAD') {
        const identity = await (await raw('/System/Info/Public')).json();
        assert.equal(identity.Id, serverID, 'Refuse to write to a different Go instance');
      }
      const response = await raw(pathname, { method, headers: { ...(body === undefined ? {} : { 'Content-Type': 'application/json' }), ...(auth && token ? { 'X-Emby-Token': token } : {}) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
      const text = await response.text();
      assert.equal(response.status, expected, `${method} ${pathname}: ${redact(text)}`);
      return text ? JSON.parse(text) : null;
    }
    async function restart(replacementBinary) {
      assert(!closing, 'Environment is closing or closed');
      const identity = await (await raw('/System/Info/Public')).json();
      assert.equal(identity.Id, serverID, 'Restart requires the owned instance identity');
      assert(children.includes(goChild) && !goChild.exited, 'Restart requires the owned live process');
      if (process.platform === 'win32') {
        const killer = spawn('taskkill.exe', ['/PID', String(goChild.proc.pid), '/T', '/F'], {windowsHide:true,shell:false,stdio:'ignore'});
        const [code] = await once(killer, 'close');
        assert.equal(code, 0, 'Owned Go process termination failed');
      } else goChild.proc.kill('SIGTERM');
      let timer;
      try { await Promise.race([goChild.done, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('Owned Go process did not exit during restart')), 8000); })]); }
      finally { clearTimeout(timer); }
      assert(!await portOpen(ports.go), 'Owned Go port must be released before restart');
      children.splice(children.indexOf(goChild), 1);
      if (replacementBinary) {
        await fs.copyFile(path.resolve(replacementBinary), binary);
        if (process.platform !== 'win32') await fs.chmod(binary, 0o700);
      }
      goChild = launch('go', binary, ['-c', configPath]);
      await waitFor(async () => { const response = await raw('/System/Info/Public'); return response.ok && (await response.json()).Id === serverID; }, children, 'restarted owned Go ServerId');
    }
    const authBefore = await request('/api/auth/status', { auth: false });
    assert.equal(authBefore.initialized, false);
    // P2 initialization uses the real account endpoints in the browser.
    if (options.bootstrap === false) {
      return { baseURL, credentials, root, close, restart, request, mediaURL, ports: { ...ports }, binaryPath: binary, binarySHA256,
        status: null, scan: null, library: null, redact,
        addSecret: value => { if (value) secrets.push(value); }, diagnostics: () => children.map(c => ({ name: c.name, log: redact(c.log) })) };
    }
    await request('/api/auth/initialize', { method: 'POST', body: { Username: credentials.username, Pw: credentials.password }, expected: 204, auth: false });
    const login = await request('/Users/AuthenticateByName', { method: 'POST', body: { Username: credentials.username, Pw: credentials.password }, auth: false });
    token = login.AccessToken;
    assert(token, 'Fresh login did not return a token');
    secrets.push(token);
    const library = await request('/api/admin/libraries', { method: 'POST', body: { Name: 'Live Fixtures', Path: path.join(root, 'media') } });
    const scan = await request('/api/admin/scan', { method: 'POST' });
    assert.equal(scan.success, mediaCount - 3);
    assert.equal(scan.pending, 3);
    assert.equal(scan.failed, 0);
    const status = await request('/api/admin/status');
    assert.equal(status.success, mediaCount - 3);
    assert.equal(status.pending, 3);
    fail('seed');
    return { baseURL, credentials, root, close, restart, request, mediaURL, ports: { ...ports }, binaryPath: binary, binarySHA256, status, scan, library, redact, addSecret: value => { if (value) secrets.push(value); }, diagnostics: () => children.map(c => ({ name: c.name, log: redact(c.log) })) };
  } catch (error) {
    const diagnostics = children.map(c => ({ name: c.name, log: redact(c.log) }));
    try { error.cleanup = await close(); } catch (cleanupError) { error.cleanup = cleanupError.cleanup; }
    error.diagnostics = diagnostics;
    error.message = redact(error.message);
    throw error;
  }
}
