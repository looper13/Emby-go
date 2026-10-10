// jsonfmt.mjs — stdin JSON -> stdout pretty JSON；--mask-token 把顶层 AccessToken 替换为 "TOKEN"。
const chunks = [];
process.stdin.on('data', (c) => chunks.push(c));
process.stdin.on('end', () => {
  const raw = Buffer.concat(chunks).toString('utf8');
  let parsed;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    console.error('jsonfmt: invalid JSON:', err.message);
    process.exit(1);
  }
  if (process.argv.includes('--mask-token') && parsed && typeof parsed === 'object' && 'AccessToken' in parsed) {
    parsed.AccessToken = 'TOKEN';
  }
  process.stdout.write(JSON.stringify(parsed, null, 2) + '\n');
});
