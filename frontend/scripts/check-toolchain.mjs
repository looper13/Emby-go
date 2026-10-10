import { readFile } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';

const expected = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
if (process.versions.node !== expected.engines.node) {
  throw new Error(`Node ${expected.engines.node} required; found ${process.versions.node}`);
}
const command = process.platform === 'win32' ? 'cmd.exe' : 'npm';
const args = process.platform === 'win32' ? ['/d', '/c', 'npm --version'] : ['--version'];
const npm = execFileSync(command, args, { encoding: 'utf8', windowsHide: true }).trim();
if (npm !== expected.engines.npm) throw new Error(`npm ${expected.engines.npm} required; found ${npm}`);
console.log(`Node ${process.versions.node}; npm ${npm}`);
