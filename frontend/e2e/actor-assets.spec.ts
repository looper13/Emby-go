import { createServer } from 'node:http';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { test, expect } from './fixtures';
import { login, navigate } from './p5-fixtures';

test('actor source save/restore, read-only preview, import and GFriends completion', async ({page,live},info) => {
  const previous = await live.request('/api/admin/scrape/settings');
  let libraryId = 0;
  const root=path.join(live.root,'actor-acceptance'), legacy=path.join(root,'legacy');await mkdir(legacy,{recursive:true});
  const png=await readFile(path.join(live.root,'media','L005','poster.png'));
  const nfo='<movie><title>Portrait fixture</title><actor><name>Local Portrait</name></actor><actor><name>New Portrait</name></actor><custom>preserve</custom></movie>';
  await writeFile(path.join(root,'TEST-100.strm'),'https://example.invalid/movie.mp4');await writeFile(path.join(root,'TEST-100.nfo'),nfo);
  const legacyName=createHash('sha256').update('local portrait').digest('hex').slice(0,16)+'.webp';await writeFile(path.join(legacy,legacyName),png);
  let downloads=0, holdDownload=false, finishDownload=()=>{};const upstream=createServer((req,res)=>{
    if(req.url==='/Filetree.json'){res.setHeader('Content-Type','application/json');res.end(JSON.stringify({Information:{Timestamp:1.5},Content:{'0-Hand-Storage':{'New Portrait.jpg':'New Portrait.jpg'}}}));return;}
    if(decodeURI(req.url ?? '')==='/Content/0-Hand-Storage/New Portrait.jpg'){downloads++;res.setHeader('Content-Type','image/png');if(holdDownload) finishDownload=()=>res.end(png);else res.end(png);return;}
    res.writeHead(404);res.end();
  });await new Promise<void>(resolve=>upstream.listen(0,'127.0.0.1',resolve));const address=upstream.address();if(!address || typeof address==='string')throw new Error('fixture address');const base=`http://127.0.0.1:${address.port}`;
  try {
    const lib=await live.request('/api/admin/libraries',{method:'POST',body:{Name:'Actor acceptance',Path:root}});
    libraryId = lib.Id ?? lib.id;
    await live.request('/api/admin/scan',{method:'POST'});
    await login(page,live,'/actors');
    await expect(page.locator('[data-page=actors]')).toHaveAttribute('aria-current','page');
    await page.locator('#actor-library').selectOption(String(libraryId));
    await expect(page.locator('.actor-card')).toHaveCount(2);
    await expect(page.locator('#actor-stats')).toContainText('缺少头像');
    if (!await page.locator('#actor-config').evaluate(el => (el as HTMLDetailsElement).open)) await page.locator('#actor-config > summary').click();
    await page.locator('#sc-avatar-source').selectOption('gfriends');await page.locator('#gf-index').fill(base+'/Filetree.json');await page.locator('#gf-base').fill(base);await page.locator('#sc-avatars').fill(legacy);
    await expect(page.locator('#scrape-dirty')).toContainText('未保存');await expect(page.locator('#test-gfriends')).toBeDisabled();
    const unsaved=await live.request('/api/admin/scrape/settings');expect(unsaved.avatar_source).toBe('metatube');
    await page.locator('#scrape-restore').click();await expect(page.locator('#sc-avatar-source')).toHaveValue('metatube');
    await page.locator('#sc-avatar-source').selectOption('gfriends');await page.locator('#gf-index').fill(base+'/Filetree.json');await page.locator('#gf-base').fill(base);await page.locator('#sc-avatars').fill(legacy);await page.locator('#scrape-save').click();await expect(page.locator('#scrape-dirty')).toContainText('已保存');
    await page.locator('#test-gfriends').click();await expect(page.locator('#gf-status')).toContainText('1 个来源');expect(downloads).toBe(0);
    await page.locator('#actor-import-preview').click();await expect(page.locator('#actor-import-summary')).toContainText('待导入 1');expect(await readFile(path.join(root,'TEST-100.nfo'),'utf8')).toBe(nfo);
    await page.locator('#actor-import-start').click();await expect(page.locator('#scrape-progress-title')).toContainText('旧头像导入结束');expect(await readFile(path.join(legacy,legacyName))).toEqual(png);expect(await readFile(path.join(root,'TEST-100.nfo'),'utf8')).toBe(nfo);
    await page.locator('#actor-status').selectOption('missing');
    await expect(page.locator('.actor-card')).toHaveCount(1);
    await expect(page.locator('.actor-card')).toContainText('New Portrait');
    await page.locator('#actor-search').fill('no match'); await page.locator('.actor-filters button').click();
    await expect(page.locator('.actor-empty')).toContainText('没有符合');
    // Batch scope is the selected library even when search hides every card.
    holdDownload=true;
    await page.locator('#avatar-start').click(); await expect.poll(()=>downloads).toBe(1);
    await page.locator('#scrape-cancel-global').click(); await expect(page.locator('#scrape-progress-title')).toHaveText('头像任务已中止');
    await page.locator('#avatar-start').click(); await expect.poll(()=>downloads).toBe(2);
    await navigate(page,'overview'); await expect(page.locator('#scan')).toBeDisabled();
    await navigate(page,'actors'); await page.reload(); await expect(page.locator('#scrape-progress-title')).toContainText('正在补演员头像');
    await page.locator('#actor-library').selectOption(String(libraryId));
    holdDownload=false; finishDownload();
    await expect(page.locator('#scrape-progress-title')).toContainText('头像任务结束');expect(downloads).toBe(2);
    await page.locator('#actor-search').fill(''); await page.locator('.actor-filters button').click();
    await page.locator('#actor-status').selectOption('ready'); await expect(page.locator('.actor-card')).toHaveCount(2);
    await expect.poll(() => page.locator('.actor-card img').evaluateAll(images => images.every(img => (img as HTMLImageElement).naturalWidth > 0))).toBe(true);
    await page.locator('#avatar-start').click(); await expect(page.locator('#toasts')).toContainText('没有缺头像'); expect(downloads).toBe(2);
    expect(await readFile(path.join(root,'TEST-100.nfo'),'utf8')).toContain('<custom>preserve</custom>');
    await expect.poll(()=>page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
    await expect(page.locator('#toasts .toast')).toHaveCount(0);
    await page.screenshot({path:info.outputPath('actor-assets.png'),fullPage:true,animations:'disabled',mask:[page.locator('[data-private-path]:visible')]});
    await page.locator('.actor-card').filter({hasText:'New Portrait'}).click();
    await expect(page).toHaveURL(/#\/items\?.*person=New(?:%20|\+)Portrait/);
    await expect(page.locator('#page-title')).toHaveText('媒体墙');
  } finally {
    try {
      if ((await live.request('/api/admin/scrape/progress')).running) {
        await live.request('/api/admin/scrape/cancel',{method:'POST'});
        await expect.poll(async () => (await live.request('/api/admin/scrape/progress')).running).toBe(false);
      }
      await live.request('/api/admin/scrape/settings',{method:'PUT',body:{...previous,metatube_token:'',translate:{...previous.translate,api_key:''}}});
      if (libraryId) await live.request(`/api/admin/libraries/${libraryId}`,{method:'DELETE',expected:204});
    } finally {
      upstream.closeAllConnections();
      await new Promise<void>((resolve,reject)=>upstream.close(err=>err?reject(err):resolve()));
    }
  }
});
