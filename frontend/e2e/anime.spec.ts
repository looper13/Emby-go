import { DatabaseSync } from "node:sqlite";
import path from "node:path";
import { test, expect } from "./fixtures";
import { seedBangumi } from './bangumi-fixture';

// Only the fixture-owned temporary DB is seeded. Provider parsers and refreshes
// use captured HTTP fixtures in Go tests; browser requests exercise the real API.
test("anime catalog, binding, shared progress and explicit settings save", async ({
  page,
  live,
}, info) => {
  const now = new Date().toISOString();
  const db = new DatabaseSync(path.join(live.root, "emby-go.db"));
  const subject = {
    id: 9001,
    bangumi_id: 418229,
    name: "接口验收番剧：很长的中文标题与特别篇",
    original_name: "Fixture Animation",
    summary: "这是本地验收数据。番剧资料和媒体资源分别管理。",
    platform: "TV",
    image: "",
    date: "2026-10-01",
    rating: 8.5,
    episode_count: 2,
  };
  const entry = {
    id: 9001,
    subject_id: 9001,
    bangumi_id: 418229,
    name: subject.name,
    image: "",
    year: 2026,
    season: 4,
    weekday: 3,
    manual: true,
    revision: 1,
    updated_at: now,
  };
  const episode = {
    id: 9001,
    subject_id: 9001,
    bangumi_id: 900001,
    name: "第一集",
    original_name: "Episode One",
    sort: 1,
    type: 0,
    season: 1,
    index: 1,
    summary: "",
    air_date: "2026-10-01",
  };
  try {
    db.exec(
      "PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; BEGIN IMMEDIATE",
    );
    db.prepare(
      "INSERT INTO anime_subjects(id,bangumi_id,name,platform,metadata,updated_at) VALUES(?,?,?,?,?,?)",
    ).run(
      subject.id,
      subject.bangumi_id,
      subject.name,
      "TV",
      JSON.stringify(subject),
      now,
    );
    db.prepare(
      "INSERT INTO anime_episodes(id,subject_id,bangumi_id,sort,season,emby_index,metadata) VALUES(?,?,?,?,?,?,?)",
    ).run(
      episode.id,
      subject.id,
      episode.bangumi_id,
      "1",
      1,
      1,
      JSON.stringify(episode),
    );
    db.prepare(
      "INSERT INTO anime_mikan_entries(id,subject_id,name,year,season,weekday,metadata,manual,updated_at) VALUES(?,?,?,?,?,?,?,?,?)",
    ).run(
      entry.id,
      subject.id,
      entry.name,
      2026,
      4,
      3,
      JSON.stringify(entry),
      1,
      now,
    );
    db.prepare(
      "INSERT INTO anime_catalog_entries(year,season,mikan_id,weekday) VALUES(2026,4,9001,3)",
    ).run();
    for (const scope of ["catalog:2026:4", "entry:9001"])
      db.prepare(
        "INSERT INTO anime_sync_state(scope,generation,status,updated_at,last_success_at) VALUES(?,1,'success',?,?)",
      ).run(scope, now, now);
    const release = {
      id: "b".repeat(40),
      mikan_id: 9001,
      title: "[测试字幕组] 第一集 [1080P][简体]",
      group: "测试字幕组",
      size: 1000,
      size_text: "1 KB",
      published_at: now,
      url: "https://mikan.tangbai.cc/Home/Episode/" + "b".repeat(40),
      torrent: "",
      magnet: "magnet:?xt=urn:btih:" + "b".repeat(40),
      resolution: "1080P",
      subtitle: "简体",
      episode_hint: "1",
      ambiguous: false,
    };
    db.prepare(
      "INSERT INTO anime_releases(id,mikan_id,metadata,published_at) VALUES(?,?,?,?)",
    ).run(release.id, 9001, JSON.stringify(release), now);
    db.exec("COMMIT");
  } finally {
    db.close();
  }
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await seedBangumi(live.root,subject,episode);
  await live.restart();
  await page.goto(`${live.baseURL}/admin#/anime?year=2026&season=4`);
  await page.locator("#auth-user").fill(live.credentials.username);
  await page.locator("#auth-pw").fill(live.credentials.password);
  await page.locator("#auth-form button[type=submit]").click();
  await expect(page.locator(".anime-card")).toHaveCount(1);
  await expect(page.locator('.anime-cover img')).toBeVisible();
  expect(await page.locator('.anime-grid').evaluate(el=>getComputedStyle(el).display)).toBe('grid');
  await page.screenshot({
    path: info.outputPath("anime-catalog.png"),
    animations: "disabled",
  });
  await page.locator(".anime-card").click();
  await expect(page.locator(".anime-hero h2")).toHaveText(subject.name);
  await page.getByRole('button',{name:'角色与制作人员',exact:true}).click();
  await expect(page.locator('.bangumi-avatar img')).toHaveCount(2);
  await expect.poll(()=>page.locator('.bangumi-avatar img').evaluateAll(nodes=>nodes.every(el=>(el as HTMLImageElement).naturalWidth>0))).toBe(true);
  await page.locator('.bangumi-people').scrollIntoViewIfNeeded();
  await page.screenshot({path:info.outputPath('bangumi-people.png'),animations:'disabled'});
  await page.getByRole('button',{name:'每集剧照',exact:true}).click();
  await expect(page.locator('.stills-grid img')).toBeVisible();
  await page.locator('.episode-stills').scrollIntoViewIfNeeded();
  await expect.poll(()=>page.locator('.episode-stills').evaluate(el=>getComputedStyle(el.parentElement!).opacity)).toBe('1');
  expect(await page.locator('.stills-grid img').evaluate((el:HTMLImageElement)=>el.naturalWidth)).toBeGreaterThan(0);
  await page.screenshot({path:info.outputPath('bangumi-stills.png'),animations:'disabled'});
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await page.getByRole('button',{name:'更改或刷新剧照',exact:true}).click();
  await expect(page.getByRole('dialog')).toBeVisible();
  await expect(page.getByText('TMDB 凭据未配置',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'关闭',exact:true}).click();
  await page.getByRole('button',{name:'发布资源',exact:true}).click();
  await page.getByRole('link',{name:'查看蜜柑发布',exact:true}).click();
  await expect(page.locator(".anime-hero h2")).toHaveText(subject.name);
  await expect(page.getByText("未关联", { exact: false })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "播放", exact: true }),
  ).toHaveCount(0);
  await expect(page.locator(".anime-resource")).toHaveCount(1);
  await page.getByRole("button", { name: "关联媒体", exact: true }).click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page
    .getByRole("dialog")
    .getByPlaceholder("搜索已入库媒体")
    .fill("ABF-005");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "搜索", exact: true })
    .click();
  await expect(page.locator(".anime-candidate")).toHaveCount(1);
  await page.locator(".anime-candidate input").check();
  await page.getByRole("button", { name: "确认关联", exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expect(page.getByText("待检查", { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "检查资源", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "播放", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("上次检查：", { exact: false })).toBeVisible();
  const episodes = await live.request("/api/admin/anime/entries/9001/episodes");
  const movieId = episodes.items[0].binding.movie_id;
  await live.request("/Sessions/Playing/Progress", {
    method: "POST",
    body: { ItemId: "anime-episode:9001", PositionTicks: 10000000 },
    expected: 204,
  });
  expect(
    (await live.request(`/Users/1/Items/${movieId}`)).UserData
      .PlaybackPositionTicks,
  ).toBe(10000000);
  await page.getByRole("button", { name: "播放", exact: true }).click();
  await expect(page.locator("#player-overlay")).toBeVisible();
  await expect
    .poll(() =>
      page
        .locator("#player-stage video")
        .evaluate((v: HTMLVideoElement) => v.readyState),
    )
    .toBeGreaterThanOrEqual(2);
  await page.locator("#player-close").click();
  await expect(page.locator("#player-overlay")).toHaveCount(0);
  await page.screenshot({
    path: info.outputPath("anime-detail.png"),
    animations: "disabled",
  });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  expect(
    await page
      .locator(".anime-search input")
      .evaluateAll((inputs) =>
        inputs.every(
          (input) =>
            input.getBoundingClientRect().right <=
            input.parentElement!.getBoundingClientRect().right + 1,
        ),
      ),
  ).toBe(true);
  await page.getByRole("button", { name: "← 返回动漫库", exact: true }).click();
  await expect(page.locator(".anime-card")).toHaveCount(1);
  await page.getByRole("link", { name: "来源与设置" }).click();
  await page.locator("#anime-name").fill("动漫草稿");
  await expect(page.getByText("· 未保存")).toBeVisible();
  expect((await live.request("/api/admin/anime/settings")).library.Name).toBe(
    "动漫",
  );
  await page.getByRole("button", { name: "恢复已保存值" }).click();
  await expect(page.locator("#anime-name")).toHaveValue("动漫");
  await page.locator("#anime-name").fill("我的动漫");
  await page.getByRole("button", { name: "保存设置", exact:true }).click();
  await expect
    .poll(
      async () =>
        (await live.request("/api/admin/anime/settings")).library.Name,
    )
    .toBe("我的动漫");
  await page.reload();
  await expect(page.locator("#anime-name")).toHaveValue("我的动漫");
  await page.locator("#anime-enabled").uncheck();
  await page.locator('#tmdb-api-key').fill('fixture-key');
  await page.getByRole('button',{name:'保存 TMDB 设置',exact:true}).click();
  await expect(page.locator('#tmdb-api-key')).toHaveValue('');
  expect(await live.request('/api/admin/anime/tmdb/settings')).toEqual({configured:true,revision:2});
  await page.getByRole("button", { name: "保存设置",exact:true }).click();
  await expect
    .poll(
      async () =>
        (await live.request("/api/admin/anime/settings")).library.Enabled,
    )
    .toBe(false);
  await live.request("/Items/anime-episode:9001/PlaybackInfo", {
    expected: 404,
  });
  await live.restart();
  expect(
    (await live.request("/api/admin/anime/settings")).library.Enabled,
  ).toBe(false);
  expect(errors).toEqual([]);
});
