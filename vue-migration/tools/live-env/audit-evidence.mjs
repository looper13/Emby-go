import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { loadPlaywright } from './browser-baseline.mjs';

export async function auditEvidence(directory) {
  const { bundle } = loadPlaywright();
  const result = { files: 0, zipEntries: 0, jsonFiles: 0, violations: [] };
  const inspect = (name, data) => {
    let text;
    try { text = new TextDecoder('utf-8', { fatal: true }).decode(data); } catch { return; }
    const patterns = {
      authToken: /\b[0-9a-f]{48}\b/i,
      temporaryUser: /\blive_[0-9a-f]{10}\b/,
      windowsUserPath: /[A-Z]:(?:\\+|\/)Users(?:\\+|\/)[^\\/"\s]+(?:\\+|\/)AppData/i,
      cygwinUserPath: /\/cygdrive\/[a-z]\/Users\/[^/]+\/AppData/i,
      password: /"Pw"\s*:\s*"(?!REDACTED"|TOKEN"|"\s*[,}])[^"\n]+"/,
    };
    for (const [kind, pattern] of Object.entries(patterns)) if (pattern.test(text)) result.violations.push({ file: name, kind });
  };
  const inspectZip = async file => {
    const zip = await new Promise((resolve, reject) => bundle.yauzl.open(file, { lazyEntries: true }, (error, opened) => error ? reject(error) : resolve(opened)));
    await new Promise((resolve, reject) => {
      zip.on('error', reject);
      zip.on('end', resolve);
      zip.on('entry', entry => {
        zip.openReadStream(entry, (error, stream) => {
          if (error) return reject(error);
          const chunks = [];
          stream.on('error', reject);
          stream.on('data', chunk => chunks.push(chunk));
          stream.on('end', () => {
            result.zipEntries++;
            inspect(`${path.relative(directory, file)}:${entry.fileName}`, Buffer.concat(chunks));
            zip.readEntry();
          });
        });
      });
      zip.readEntry();
    });
  };
  async function visit(dir) {
    for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name);
      if (entry.isDirectory()) await visit(file);
      else {
        result.files++;
        if (entry.name.endsWith('.zip')) await inspectZip(file);
        else {
          const data = await fs.readFile(file);
          if (entry.name.endsWith('.json')) { JSON.parse(data); result.jsonFiles++; }
          inspect(path.relative(directory, file), data);
        }
      }
    }
  }
  await visit(directory);
  return result;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  assert(process.argv[2], 'Pass an evidence directory');
  const result = await auditEvidence(path.resolve(process.argv[2]));
  console.log(JSON.stringify(result, null, 2));
  if (result.violations.length) process.exitCode = 1;
}
