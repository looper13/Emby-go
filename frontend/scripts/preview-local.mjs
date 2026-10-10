import { createServer } from 'vite';
import { fileURLToPath } from 'node:url';
import { startLiveEnvironment } from '../../vue-migration/tools/live-env/environment.mjs';

const live = await startLiveEnvironment();
let vite;
let stopping;
async function stop() {
  if (stopping) return stopping;
  stopping = (async () => {
    try { await vite?.close(); }
    finally { await live.close(); }
  })();
  return stopping;
}
try {
  process.env.EMBY_DEV_PROXY = live.baseURL;
  vite = await createServer({
    configFile: fileURLToPath(new URL('../vite.config.ts', import.meta.url)),
    server: { host: '127.0.0.1', port: 5173, strictPort: false },
  });
  await vite.listen();
  const url = vite.resolvedUrls.local[0];
  console.log(JSON.stringify({ url, backend: `${live.baseURL}/admin-vue`, username: live.credentials.username, password: live.credentials.password, pid: process.pid, temporaryData: live.root }));
  for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => { void stop().finally(() => process.exit()); });
} catch (error) {
  await stop();
  throw error;
}
