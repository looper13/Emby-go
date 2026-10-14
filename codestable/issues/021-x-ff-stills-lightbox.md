---
kind: issue
title: 剧照改为页内灯箱，可左右切换浏览
type: ff
status: closed
created: 2026-10-14
---

详情页剧照此前是 `<a target="_blank">`，点开跳到新标签页看单张原图，看完要手动关标签页、回来再点下一张，没法连贯浏览。现改为页内灯箱：点缩略图在弹窗内打开原图，左右按钮或 ← → 循环切换，标题显示「剧照 N / M」，Esc、点背景或 ✕ 关闭；背景滚动锁定、Tab 焦点锁在弹窗内，关闭后焦点回到刚才点的那张缩略图。相邻剧照原图会预取，切换时不必等重新下载。只有一张剧照时不显示左右按钮。

- 改动：`frontend/src/components/media/ArtworkLightbox.vue`（新增，弹窗与键盘交互）、`frontend/src/components/media/ArtworkGallery.vue`（缩略图 `a` → `button`，持有当前索引并在关闭时归还焦点）、`frontend/src/styles/artwork.css`（新增）+ `frontend/src/main.ts` 引入、`frontend/tests/media-interactions.test.ts` 与 `frontend/e2e/media-interactions.spec.ts` 的断言随之改写。无后端、无接口、无数据库改动；打开原图仍走 `/Items/:id/Images/Backdrop/:index?tag=`，不带 `maxWidth`。
- 验证：`npm run typecheck` 通过；`npx vitest run` 214 项中 213 通过（新增用例覆盖循环切换、方向键、Esc、滚动锁、焦点归还、单张不显示导航）；`npm run build` 构建并发布 5 个 UI 文件成功。e2e 未执行——Playwright 的 chromium 二进制未安装（10 个用例均在 `browserType.launch` 阶段失败，与本次改动无关）；灯箱只经 jsdom 单测，未在真实浏览器走查。
- codestable：无 spec/epic 需同步（当前工作区只有 issues 树）。新增一条已生效约束：`frontend/src/styles/legacy.css` 必须与 `internal/server/web/style.css` 逐字节一致（`frontend/tests/baseline.test.ts`「frozen visual assets」），所以 Vue 专有样式不能写进 legacy.css，改放新文件 `src/styles/artwork.css`（与既有的 `src/styles/anime.css` 同一做法）。**已知漂移**：该 baseline 用例在 HEAD 上即失败，`legacy.css` 与 `style.css` 仅差 1 个 CR 字节（37285 vs 37284，去掉 CR 后内容相同），非本次引入。
- 顺手发现：`internal/server/server.go` 仍注册 `/web/app.js`、`/web/style.css` 等旧版静态资源，但 `/`、`/admin`、`/web/index.html` 都已由 Vue 外壳接管，旧版渲染入口 `a.dashboard` 已无调用方 —— 旧版 `internal/server/web/app.js` 里的剧照仍是新标签页打开，属未被引用的死代码，不在本次范围。
