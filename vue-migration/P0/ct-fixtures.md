# CT-01/02/03 前后端镜像规则对照夹具（P0 收集）

P0/VUE-01 产物。对应计划 §5.4：这三处前端实现镜像后端逻辑，迁移时**原样移植**再用夹具对照；不允许借迁移"顺手归一化"。
夹具文件在 [fixtures/ct/](../fixtures/ct/)；expected 标注 `source`：
- `go-test`：直接取自现有 Go 测试表（note 给 file:line）；
- `derived`：按 Go 实现推导手算（note 写依据），**P4 由 Go 生成同组输出做最终校验**（计划要求"不靠手工复制两套期望值证明一致"）。

P4 已复核：`TestVueMigrationNumberContract` 调用实际 `scraper.joinNumberTitle`，`TestVueMigrationEntityContract` 调用实际 `server.entityId`；两者读取本目录同组输入，校验并输出 Go 结果。Vue `media-interactions.test.ts` 消费相同 19+19 例，验证展示去重/实体 ID。当前实现的 Go 输出存于 `artifacts/P4/ct-go-output.txt`；夹具的来源标记保留原采集历史。259LUXU 的已知展示差异保留，实体 kind 在 helper 中转小写、名称不 trim，UI 只传规范小写 kind。

## CT-01 番号归一化与标题去重

- 前端实现：`compactNumber / titleCarriesNumber / numberUnlessInTitle`（app.js:52-59，展示层"标题已含番号则不重复显示"）。
- 后端实现：`metatube.SameNumber / Normalize / compact`（metatube/number.go:52-88）、`scraper.leadingNumber / dashedNumber / joinNumberTitle`（scraper/apply.go:301-384）。
- 标签：`<num>` 与 `<title>` 前缀由 `joinNumberTitle(dashedNumber(Normalize(info.Number)), title)` 生成（apply.go:275-276）。

逐字移植要点（Vue 的 `lib/number.ts` 必须保持）：
1. 前端 `compactNumber`：大写后删除 `[-_.\s]`（空白也删）。
2. 前端 `lead` 取 `title.trim()` 后开头的 `[A-Za-z0-9._-]+` 连续段（**不**取第二段、不含空白/中文）。
3. 判定 = `compact(lead) === compact(number)`，两者非空才可能为 true；空番号一律 false。

**已发现的真实差异（记录，不修）**：`joinNumberTitle` 在比较前对 lead 应用 `dashedNumber` 的**系列改写规则**（T28→T28-…、259LUXU→LUXU-…，apply.go:328-354），前端 compact 比较不应用。因此当 NFO 由外部工具写成 `259LUXU1234 标题` 而 `<num>` 是 `LUXU-1234` 时：后端会认定"已带番号"（合并），前端会重复显示番号。对照夹具 case `series-alias-259LUXU` 记录该输入与两端结果；是否在 Vue 中修复由 P4 单独决策（默认保持等价移植）。

## CT-02 实体 ID（entityIdOf）

- 前端：`entityIdOf(kind, name)` = `` `${kind}:${base64url(UTF-8(name))}` ``，用 `btoa` + `+→-`、`/→_`、去 `=`（app.js:899-904）。
- 后端：`entityId(kind, name)` = `strings.ToLower(kind) + ":" + base64.RawURLEncoding.EncodeToString([]byte(name))`（library.go:1215-1218）。
- 等价点：小写 kind、UTF-8 字节、URL-safe base64、无 padding、**不 trim/normalize** 名称（前后空白照编码）。
- 夹具状态：ct-02 全部 expected 已于 2026-10-10 用 Go 标准库 `base64.RawURLEncoding`（后端同一 stdlib 调用）生成（含 `+→-`、`/→_`、中文、emoji、含空白/冒号等 19 例）；P4 用仓库内 `server.entityId` 复核后固定。
- 消费场景：演员头像 `<img src="/Items/{entityId('person',name)}/Images/Primary?...">`（app.js:785）；实体跳转 URL 参数（genre/tag/studio/person/collection 的**名称**而非 id）。
- 验收：同一输入必须产生完全一致字符串，且 `/Items/:entityId/Images/Primary` 能取到图（后端对虚拟实体回代表图，media.go:100-113）。

## CT-03 "只补缺失 / 强制覆盖"

- 请求契约：`POST /api/admin/items/:id/scrape` body `overwrite` 是 `*bool`（scrape.go:503-520）：
  - **缺省（JSON 无该键）** → 取全局配置 `scrape.overwrite`；
  - **显式 false** → 只补缺失（与缺省不同！不得用 `value || default` 实现）；
  - **显式 true** → 强制覆盖。
- 预览端 `inspect.overwrite` 回显当前全局默认（scrape_inspect.go:85），UI 勾选框初始值取自它（app.js:1484）。
- 批量：`POST /scrape/run` 的 `overwrite` 同理（scrape.go:216-218），`only_missing` 缺省 true。
- 设置保存：`PUT /scrape/settings` 的 overwrite 也是指针（scrape_settings.go:136-137）——不传=不改。
- 落盘语义在 `scraper.Apply`（buildFields 的 `Overwrite` 字段，apply.go:277-297；NFO 层 `nfo.UpdateScraped`）。
- 取消语义：预览阶段零写入；不点"确认写入"不产生任何 POST（app.js:1494；计划 §6.4）。

验证清单见 `fixtures/ct/ct-03-overwrite.json`（P5 在临时 NFO/图片目录用真实接口跑通；判据：显式 false 与缺省在"全局 overwrite=true"时行为必须不同）。
