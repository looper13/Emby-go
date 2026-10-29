import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { createRequire } from 'node:module';
import { pipeline } from 'node:stream/promises';
import { createWriteStream } from 'node:fs';
import { repoRoot } from './environment.mjs';

export function loadPlaywright(supplied = process.env.PW_CORE) {
  const require = createRequire(import.meta.url);
  const candidates = [supplied, path.join(repoRoot, 'frontend/node_modules/playwright-core'), 'playwright-core'].filter(Boolean);
  for (const candidate of candidates) {
    try {
      const entry = require.resolve(candidate);
      const directory = path.dirname(entry);
      const pw = require(entry);
      const bundle = require(path.join(directory, 'lib/utilsBundle.js'));
      assert(bundle.yauzl && bundle.yazl, 'Trace ZIP utilities unavailable');
      return { pw, bundle, version: require(path.join(directory, 'package.json')).version };
    } catch { /* Try the next already-installed package. Never install into the frontend here. */ }
  }
  throw new Error('Install Playwright separately or set PW_CORE to its playwright-core package directory');
}

export async function sanitizeTrace(input, output, bundle, redact) {
  const { yauzl, yazl } = bundle;
  const source = await new Promise((resolve, reject) => yauzl.open(input, { lazyEntries: true }, (error, zip) => error ? reject(error) : resolve(zip)));
  const target = new yazl.ZipFile();
  const written = pipeline(target.outputStream, createWriteStream(output, { flags: 'wx' }));
  try {
    await new Promise((resolve, reject) => {
      source.on('error', reject);
      source.on('end', resolve);
      source.on('entry', entry => {
        source.openReadStream(entry, (error, stream) => {
          if (error) return reject(error);
          const chunks = [];
          stream.on('error', reject);
          stream.on('data', chunk => chunks.push(chunk));
          stream.on('end', () => {
            try {
              let data = Buffer.concat(chunks);
              try {
                const text = new TextDecoder('utf-8', { fatal: true }).decode(data);
                data = Buffer.from(redact(text));
              } catch { /* Binary image/video resources contain no text credentials. */ }
              target.addBuffer(data, entry.fileName);
              source.readEntry();
            } catch (err) { reject(err); }
          });
        });
      });
      source.readEntry();
    });
    target.end();
    await written;
  } catch (error) {
    source.close();
    target.end();
    await written.catch(() => {});
    await fs.rm(output, { force: true });
    throw error;
  }
}
