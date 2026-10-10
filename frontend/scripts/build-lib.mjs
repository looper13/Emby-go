import { createHash } from 'node:crypto';
import { cp, lstat, mkdir, readFile, readdir, rename, rm, stat, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { parse } from 'parse5';

const extensions = new Set(['.html', '.js', '.css', '.svg', '.ico', '.png', '.jpg', '.jpeg', '.webp', '.avif', '.gif', '.woff', '.woff2', '.ttf', '.otf']);
const digest = bytes => createHash('sha256').update(bytes).digest('hex');

export function buildPaths(root) {
  root = path.resolve(root);
  return {
    root,
    frontend: path.join(root, 'frontend'),
    dist: path.join(root, 'frontend', 'dist'),
    target: path.join(root, 'internal', 'server', 'web_dist'),
    next: path.join(root, 'internal', 'server', 'web_dist.next'),
    previous: path.join(root, 'internal', 'server', 'web_dist.previous'),
    lock: path.join(root, 'frontend', '.ui-build.lock'),
  };
}

export async function assertBuildPaths(paths) {
  const expected = buildPaths(paths.root);
  for (const key of ['frontend', 'dist', 'target', 'next', 'previous', 'lock']) {
    if (paths[key] !== expected[key]) throw new Error(`Refusing unexpected ${key} path: ${paths[key]}`);
    // Check every existing ancestor, including junctions, before clearing a directory.
    for (let current = paths[key]; ; current = path.dirname(current)) {
      try {
        if ((await lstat(current)).isSymbolicLink()) throw new Error(`Refusing symlink: ${current}`);
      } catch (error) {
        if (error.code !== 'ENOENT') throw error;
      }
      if (current === path.dirname(current)) break;
    }
  }
}

export async function withBuildLock(paths, action) {
  await assertBuildPaths(paths);
  try {
    await mkdir(paths.lock);
  } catch (error) {
    if (error.code === 'EEXIST') throw new Error('Another UI build owns .ui-build.lock; do not run concurrent builds.');
    throw error;
  }
  try {
    await writeFile(path.join(paths.lock, 'owner.json'), JSON.stringify({ pid: process.pid, startedAt: new Date().toISOString() }));
    return await action();
  } finally {
    await assertBuildPaths(paths);
    await rm(paths.lock, { recursive: true });
  }
}

function safeRelative(name) {
  return typeof name === 'string' && name !== '' && !name.includes('\\') && !name.includes('%') &&
    !name.includes(':') && !name.startsWith('/') && name.split('/').every(part => part && part !== '.' && part !== '..');
}

async function listFiles(directory, prefix = '') {
  const output = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const name = prefix + entry.name;
    if (entry.isSymbolicLink()) throw new Error(`Refusing generated symlink: ${name}`);
    if (entry.isDirectory()) output.push(...await listFiles(path.join(directory, entry.name), `${name}/`));
    else if (entry.isFile()) output.push(name);
    else throw new Error(`Unsupported generated entry: ${name}`);
  }
  return output.sort();
}

export async function inspectDistribution(directory, source) {
  const all = await listFiles(directory);
  const files = all.filter(name => name !== '.vite/manifest.json');
  for (const name of files) {
    if (!safeRelative(name) || name.split('/').some(part => part.startsWith('.')) || !extensions.has(path.extname(name).toLowerCase())) {
      throw new Error(`Unexpected generated file: ${name}`);
    }
  }
  const fileSet = new Set(files);
  const requireFile = name => {
    if (!safeRelative(name) || !fileSet.has(name)) throw new Error(`Missing or unsafe referenced asset: ${name}`);
  };
  requireFile('index.html');
  const manifest = JSON.parse(await readFile(path.join(directory, '.vite', 'manifest.json'), 'utf8'));
  if (!manifest['index.html']?.isEntry) throw new Error('Vite entry manifest missing');
  const hashed = new Set();
  for (const entry of Object.values(manifest)) {
    for (const name of [entry.file, ...(entry.css || []), ...(entry.assets || [])]) {
      requireFile(name);
      if (/^assets\/[^/]+-[A-Za-z0-9_-]{8,}\.[a-z0-9]+$/i.test(name)) hashed.add(name);
    }
    for (const key of [...(entry.imports || []), ...(entry.dynamicImports || [])]) {
      if (!manifest[key]) throw new Error(`Missing chunk manifest: ${key}`);
    }
  }
  const html = parse(await readFile(path.join(directory, 'index.html'), 'utf8'));
  let scripts = 0;
  let styles = 0;
  const visit = node => {
    const attrs = Object.fromEntries((node.attrs || []).map(attr => [attr.name, attr.value]));
    const resource = node.tagName === 'script' ? attrs.src : node.tagName === 'link' ? attrs.href : undefined;
    if (resource) {
      if (!resource.startsWith('/web/ui/')) throw new Error(`Unexpected resource base: ${resource}`);
      requireFile(resource.slice('/web/ui/'.length));
      if (node.tagName === 'script' && attrs.type === 'module') scripts++;
      if (node.tagName === 'link' && attrs.rel === 'stylesheet') styles++;
    }
    for (const child of node.childNodes || []) visit(child);
  };
  visit(html);
  if (scripts < 1 || styles < 1) throw new Error('Built HTML must load a real module and stylesheet');
  const records = [];
  for (const name of files) {
    const bytes = await readFile(path.join(directory, name));
    records.push({ path: name, sha256: digest(bytes), size: bytes.length, immutable: hashed.has(name) });
  }
  return { schemaVersion: 1, builtAt: new Date().toISOString(), source, files: records };
}

export async function verifyPublished(directory) {
  const info = JSON.parse(await readFile(path.join(directory, 'build-info.json'), 'utf8'));
  for (const file of info.files) {
    if (!safeRelative(file.path)) throw new Error('Unsafe build manifest path');
    const bytes = await readFile(path.join(directory, file.path));
    if (bytes.length !== file.size || digest(bytes) !== file.sha256) throw new Error(`Asset changed: ${file.path}`);
  }
  return info;
}

export async function publishDistribution(paths, info) {
  await assertBuildPaths(paths);
  for (const directory of [paths.next, paths.previous]) {
    try {
      await stat(directory);
      throw new Error(`Previous interrupted publish found at ${directory}; inspect before retrying`);
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
  }
  await mkdir(paths.next);
  let movedOld = false;
  try {
    for (const file of info.files) {
      if (!safeRelative(file.path)) throw new Error('Unsafe build manifest path');
      await mkdir(path.dirname(path.join(paths.next, file.path)), { recursive: true });
      await cp(path.join(paths.dist, file.path), path.join(paths.next, file.path));
    }
    await writeFile(path.join(paths.next, 'build-info.json'), `${JSON.stringify(info, null, 2)}\n`);
    await verifyPublished(paths.next);
    await assertBuildPaths(paths);
    try {
      await rename(paths.target, paths.previous);
      movedOld = true;
    } catch (error) {
      if (error.code !== 'ENOENT') throw error;
    }
    try {
      await rename(paths.next, paths.target);
    } catch (error) {
      if (movedOld) await rename(paths.previous, paths.target);
      throw error;
    }
    if (movedOld) await rm(paths.previous, { recursive: true });
  } finally {
    await assertBuildPaths(paths);
    await rm(paths.next, { recursive: true, force: true });
  }
}
