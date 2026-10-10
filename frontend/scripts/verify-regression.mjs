import { spawn } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../../', import.meta.url));
const phase = process.env.EMBY_MIGRATION_PHASE ?? 'P1';
if (!/^P[0-7]$/.test(phase)) throw new Error('EMBY_MIGRATION_PHASE must be P0 through P7');
const output = path.join(root, 'vue-migration/artifacts', phase, 'go-regression.json');
// This existing test clears a hard-coded local Redis database, so never run it here.
const args = ['test', '-json', '-count=1', '-skip', '^TestRedisBehavior$', './...'];
const child = spawn('go', args, { cwd: root, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
const report = { command: ['go', ...args], startedAt: new Date().toISOString(), packages: [], failures: [], diagnostics: [], excluded: ['TestRedisBehavior: uses existing 127.0.0.1:6379 DB15'] };
const lines = createInterface({ input: child.stdout });
const pending = new Map();
lines.on('line', line => {
  let event;
  try { event = JSON.parse(line); } catch { report.diagnostics.push(line); return; }
  if (event.Test) {
    const key = `${event.Package}/${event.Test}`;
    if (event.Output) pending.set(key, (pending.get(key) || '') + event.Output);
    if (event.Action === 'fail') report.failures.push({ package: event.Package, test: event.Test, output: pending.get(key) || '' });
    if (['fail', 'pass', 'skip'].includes(event.Action)) pending.delete(key);
  } else if (['pass', 'fail', 'skip'].includes(event.Action)) {
    report.packages.push({ name: event.Package, result: event.Action, elapsed: event.Elapsed });
  }
});
child.stderr.on('data', data => report.diagnostics.push(data.toString()));
child.on('error', error => report.diagnostics.push(error.message));
child.on('close', async code => {
  await mkdir(path.dirname(output), { recursive: true });
  await writeFile(output, `${JSON.stringify({ ...report, exitCode: code }, null, 2)}\n`);
  console.log(JSON.stringify({ packages: report.packages, failures: report.failures.map(failure => ({ ...failure, output: failure.output.slice(0, 1800) })), diagnostics: report.diagnostics, report: output }, null, 2));
  process.exitCode = code ?? 1;
});
