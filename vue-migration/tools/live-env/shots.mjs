// shots.mjs — VUE-01/P0 旧 UI 截图 + 固定请求序列（请求计数用）。
//
// 运行前提：临时 Go 服务已在 SHOTS_URL 运行、媒体数据已入库（见 live-env-notes.md）。
// 依赖：playwright-core（npm i playwright-core，不下载浏览器）+ 系统 Edge（channel:msedge）。
// PW_CORE 指向 playwright-core 包目录（在临时目录安装），本脚本不引入仓库依赖。
//
// 环境变量：
//   PW_CORE    playwright-core 包绝对路径（可选；缺省按 node 解析）
//   SHOTS_URL  默认 http://127.0.0.1:18099
//   OUT        截图输出目录（默认 ./screenshots，应指向 vue-migration/screenshots）
//   LIVE_USER / LIVE_PW  一次性账号（固定序列中的真实 UI 登录）
//   LOG_FILE   服务请求日志路径（log/request-*.log；用于打印序列行号标记）
//   DETAIL_ID  详情页截图使用的影片 id（默认 5，= 有图+中文标题条目）
//
// 输出：desktop-*.png / mobile-*.png，并在 stdout 打印
//   SEQ_LOG_START / SEQ_LOG_END（固定序列在请求日志中的行号区间，供 count-requests.mjs 统计）

import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';

const require = createRequire(import.meta.url);
const pw = process.env.PW_CORE ? require(process.env.PW_CORE) : require('playwright-core');

const BASE = process.env.SHOTS_URL || 'http://127.0.0.1:18099';
const OUT = process.env.OUT || 'screenshots';
const USER = process.env.LIVE_USER;
const PASSWORD = process.env.LIVE_PW;
const LOG_FILE = process.env.LOG_FILE || '';
const DETAIL_ID = process.env.DETAIL_ID || '5';

if (!USER || !PASSWORD) {
  console.error('shots.mjs: set LIVE_USER / LIVE_PW');
  process.exit(2);
}
fs.mkdirSync(OUT, { recursive: true });

const logLines = () => {
  if (!LOG_FILE) return -1;
  try {
    return fs.readFileSync(LOG_FILE, 'utf8').split('\n').length;
  } catch {
    return -1;
  }
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const browser = await pw.chromium.launch({ channel: 'msedge', headless: true });

const shoot = async (page, name) => {
  await page.screenshot({ path: path.join(OUT, name) });
  console.log(`shot ${name}`);
};

const waitIdle = async (page, timeout = 15000) => {
  try {
    await page.waitForLoadState('networkidle', { timeout });
  } catch {
    // networkidle 超时不代表失败（可能存在长轮询）；继续。
  }
  await sleep(250);
};

try {
  // ---------- 桌面上下文（1280x800）----------
  const desktop = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 1 });
  const page = await desktop.newPage();

  // 固定请求序列：登录 -> 媒体墙 -> 进入详情 -> 返回 -> 切一个筛选
  const seqStart = logLines();
  console.log(`SEQ_LOG_START=${seqStart}`);

  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('#auth-form');
  await sleep(300);
  // 登录页截图必须先于登录（登录成功后会被替换跳转）——保存为桌面登录页
  await shoot(page, 'desktop-login.png');

  await page.fill('#auth-user', USER);
  await page.fill('#auth-pw', PASSWORD);
  await Promise.all([
    page.waitForURL(/\/admin/, { timeout: 15000 }),
    page.click('#auth-form button[type=submit]'),
  ]);
  await page.waitForSelector('.nav-item[data-page="overview"]');
  await waitIdle(page); // boot 的 scan/probe progress 检查

  // 媒体墙（首屏 100 条）
  await page.click('.nav-item[data-page="items"]');
  await page.waitForFunction(() => document.querySelectorAll('.wall-card').length >= 100, null, { timeout: 30000 });
  await waitIdle(page);
  console.log('wall cards:', await page.locator('.wall-card').count());

  // 进入详情（点第一张卡的元数据区；卡片中心的海报区可能命中"直接播放"按钮）
  await page.locator('.wall-card .wall-meta').first().click();
  await page.waitForFunction(() => location.hash.startsWith('#item/'), null, { timeout: 10000 });
  await waitIdle(page);
  console.log('detail hash:', await page.evaluate(() => location.hash));

  // 返回媒体墙（应用历史）
  await page.goBack();
  await page.waitForFunction(() => document.querySelectorAll('.wall-card').length >= 100, null, { timeout: 15000 });
  await waitIdle(page);

  // 切一个筛选（待补录）
  await page.click('button[data-status="pending"]');
  await page.waitForFunction(() => document.querySelectorAll('.wall-card').length === 3, null, { timeout: 15000 });
  await waitIdle(page);
  console.log('pending cards:', await page.locator('.wall-card').count());

  const seqEnd = logLines();
  console.log(`SEQ_LOG_END=${seqEnd}`);

  // ---------- 桌面截图（序列之外）----------
  // 回到"全部"（默认口径）截图媒体墙
  await page.click('button[data-status=""]');
  await page.waitForFunction(() => document.querySelectorAll('.wall-card').length >= 100, null, { timeout: 15000 });
  await waitIdle(page);
  await shoot(page, 'desktop-items.png');

  const consolePages = [
    ['overview', 'desktop-overview.png'],
    ['libraries', 'desktop-libraries.png'],
  ];
  for (const [name, file] of consolePages) {
    await page.click(`.nav-item[data-page="${name}"]`);
    await waitIdle(page);
    await shoot(page, file);
  }

  // 详情页（有图+中文标题条目，直链加载）
  await page.goto(`${BASE}/admin#item/${DETAIL_ID}`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('.item-hero', { timeout: 15000 }).catch(() => console.log('warn: .item-hero not found, screenshot anyway'));
  await waitIdle(page);
  await shoot(page, 'desktop-item-detail.png');

  for (const name of ['manual', 'settings', 'apikeys', 'scrape', 'scheduled', 'tasks', 'probe']) {
    await page.click(`.nav-item[data-page="${name}"]`);
    await waitIdle(page);
    await shoot(page, `desktop-${name}.png`);
  }

  const token = await page.evaluate(() => localStorage.getItem('emby_token'));
  console.log('token captured for mobile context:', token ? 'yes' : 'no');
  await desktop.close();

  // ---------- 手机上下文（390x844）----------
  const mobile = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 2,
    isMobile: true,
    hasTouch: true,
  });
  const mpage = await mobile.newPage();
  // 登录页用全新上下文（无 token）
  await mpage.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' });
  await mpage.waitForSelector('#auth-form');
  await waitIdle(mpage);
  await shoot(mpage, 'mobile-login.png');

  // 之后注入已登录 token（page.addInitScript）
  await mobile.addInitScript((t) => localStorage.setItem('emby_token', t), token);
  await mpage.goto(`${BASE}/admin#items`, { waitUntil: 'domcontentloaded' });
  await mpage.waitForFunction(() => document.querySelectorAll('.wall-card').length >= 100, null, { timeout: 30000 });
  await waitIdle(mpage);
  await shoot(mpage, 'mobile-items.png');

  await mpage.goto(`${BASE}/admin#item/${DETAIL_ID}`, { waitUntil: 'domcontentloaded' });
  await mpage.waitForSelector('.item-hero', { timeout: 15000 }).catch(() => {});
  await waitIdle(mpage);
  await shoot(mpage, 'mobile-item-detail.png');

  await mpage.click('.nav-item[data-page="scheduled"]');
  await waitIdle(mpage);
  await shoot(mpage, 'mobile-scheduled.png');

  await mobile.close();
  console.log('ALL SHOTS DONE');
} finally {
  await browser.close();
}
