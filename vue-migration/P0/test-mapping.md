# 测试落点映射（P0）

P0/VUE-01 产物。①17 项现有回归 → 新测试架构；②计划 §9.2 新场景 → 阶段；③"现有行为但无测试"补测清单。

P4 接续说明（2026-10-10）：媒体交互相关落点已实现，见 [P4/progress.md](../P4/progress.md)。新测试分别放 `frontend/tests/core-path.test.ts`、`media-interactions.test.ts` 与 `frontend/e2e/core-path.spec.ts`、`media-interactions.spec.ts`；最终单元/组件 153 项，完整桌面/手机 E2E 42 项。原 17 项仍针对冻结旧版运行并通过，不删除原测试；P5/P6 场景继续按下表推进。

P6 接续说明（2026-10-10）：本表 17 项、§2 新场景与 §3 补测清单已逐条对到实际测试，结果与开放项见 [P6/coverage-matrix.md](../P6/coverage-matrix.md)。P6 新增 8 项单测（顶层 Escape 顺序/遮罩关闭、详情 Escape 与输入仲裁、推荐 5s 预算、Router 回退）与 1 项 E2E 断言（GL-22 标签页标题）；单元/组件 189 项，完整 E2E 58 项。

## 1. 现有 17 项回归逐条映射

来源：`internal/server/web_test/app.test.cjs`（VM/DOM 替身直接运行旧 app.js；迁移后不再读取已删除文件）。

| # | 现有测试（行号） | 断言核心 | 新落点 | 关联清单 |
|---|---|---|---|---|
| 1 | late detail cannot overwrite a newer page or its title（74） | 迟到详情不覆盖新页标题/内容 | Vitest：usePageRequest 单测 + ItemDetailPage 组件测 | GL-27 |
| 2 | late non-detail page cannot replace a newer page（86） | 迟到普通页响应丢弃 | 同上（页面级请求域） | GL-12/27 |
| 3 | navigation cancels GET reads, not writes or background progress polling（97） | 只读挂 signal；写与 /progress 不挂；写成功置 dirty | Vitest：api client 单测（记录 signal 传入规则） | GL-13/14 |
| 4 | recommendations never block detail, and late recommendations stay on their page（114） | 推荐并行不阻塞；切页后不写回 | ItemDetailPage 组件测（mock embyJSON） | GL-18/27 |
| 5 | recommendation request times out and releases its timer（128） | 5s 超时且 timers 归零 | Vitest 假时钟：embyJSON 单测 | GL-18 |
| 6 | failed wall stops automatic retries and permits a single explicit retry（140） | 失败停止自动请求；一次重试只一轮 | wall store 单测 + WallLoadState 组件测 | GL-25 邻接 |
| 7 | return restores 300 cached movies; an invalidated snapshot reloads the saved count（163） | 300 条恢复、失效按目标条数重载、请求计数 | wall store 单测（计数）+ **Playwright 历史/滚动** | GL-03/05/25 |
| 8 | a stale wall request cannot append to a newer filter（195） | 旧分页不追加到新筛选 | wall store 代次单测 | GL-30 |
| 9 | Enter and Space on a nested action do not activate the wall card（209） | 嵌套按钮键盘不触发卡片 | MediaCard 组件测 + 浏览器键盘场景 | GL-24 |
| 10 | refresh preserves expanded plot, files and reading position（225） | 刷新保留展开与位置 | ItemDetailPage 组件测 + 浏览器 | GL-32 |
| 11 | failed refresh preserves old callbacks and can be retried（239） | 刷新失败保留旧页面可用 | usePageRequest/页面测 | GL-17 |
| 12 | navigation during refresh cancels both old and refreshing page lifetimes（259） | 刷新中切页取消新旧生命周期 | 请求生命周期单测 | GL-12 |
| 13 | images use versioned thumbnails while still links keep original resolution（276） | URL 形状（tag+maxWidth；原图无参数） | lib/image-url 单测 + 详情/卡片组件测 | GL-33 |
| 14 | a broken image uses one fallback and then a fixed placeholder（292） | 一次回退再占位 | MediaImage 组件测 | GL-10 |
| 15 | trailer and still sections are independent, with cover fallback only when needed（305） | 分区独立与回退链 | ItemDetailPage 组件测 | GL-32 邻接 |
| 16 | player errors distinguish network, decode, unsupported source and mixed content（326） | 4 类错误文案 | lib/playback-error 纯函数单测 | GL-16 邻接 |
| 17 | player falls back once and ignores error callbacks from a destroyed player（336） | 单次代理回退；销毁后回调失效 | PlayerDialog 组件测 + 真实播放冒烟（P4 末） | GL-29 邻接 |

## 2. 计划 §9.2 新增场景 → 阶段映射

| 场景组 | 阶段 | 落点 |
|---|---|---|
| 初始化/已有 token/失效 token/认证网络异常/并发 401/登录恢复合法目的页 | P2 | auth store + LoginPage 单测；差异记录见 global-behaviors §C1/C2 |
| 新旧地址、中文查询、前进/后退、直开详情、被删除的库、非法 ID、未知路由 | P2/P4 | legacy-url 单测 + Playwright 历史场景 |
| 嵌入资源 MIME/缓存/HEAD、缺失 chunk 404、未知 Emby API 原协议错误 | P1/P6 | Go httptest |
| 扫描中切路由继续、浏览器刷新取消手扫、后台探测/刮削刷新恢复、无重叠轮询 | P5 | tasks store 单测 + 真服务 E2E |
| 204、非 JSON 错误、FormData、取消、重复提交、失败后输入保留 | P1/P3 | api client 单测 + 各表单组件测 |
| 批量任务占用/部分失败、取消预览无写入、确认后保持详情、cron 后端校验 | P5 | 组件测 + CT-03 临时目录真实验证 |
| 升级旧 chunk 缺失提示；回滚二进制可登录浏览 | P7 | E2E + 手工发布验证 |
| GL 编号全量对照；CT-01/02/03；登录两条路径（迁移入口/正式入口） | P4/P5/P6 | 汇总矩阵（P6 输出） |

## 3. "现有行为但无测试覆盖"补测清单（迁移必须补）

1. 登录/初始化全流程（status/initialize/AccessToken/两条跳转）。
2. 401 处理与并发合并为一次跳转；网络失败与 401 区分。
3. `runScan` 全流程：按钮态、toast 文案、结束按 hash 刷新当前页、boot 恢复、library_id 参数路径。
4. 批量探测：8s 自动隐藏（无失败时）、有失败保留、toast 汇总与失败样例；单条探测摘要文案。
5. 刮削：配置保存（留空不覆盖）、测试分支、候选数、run/avatars payload、取消不误报成功、轮询恢复与自停。
6. 单条刮削预览：候选切换、覆盖开关、确认写入 payload（overwrite 显式布尔）、取消零写入、迟到 inspect 不覆盖。
7. 计划任务：cron 校验（合法/非法/空）、编辑回填与跨路由保留、类型显隐、toggle/run/delete 编辑态清理。
8. 手动补录：逗号与全角逗号拆分、year 数字、成功 reset、失败保留、重复提交锁。
9. API 密钥：创建/删除（编码）/复制成功与失败提示。
10. 媒体库：添加（失败保留输入）、删除确认语义。
11. 总览：统计卡与状态表、重建按钮流程。
12. 接口探针：清空记录。
13. 实体跳转 URL 形状（清其他实体参数、保留 library_id、push 历史）。
14. 无效 library_id 清洗（replace，保留其余参数与 hash）。
15. Esc 优先级链（刮削→播放器→详情返回）；遮罩点击关闭判定。
16. toast 3.2s+260ms 生命周期。
17. 未知路由 → 落到 overview；非法 item ID。
18. 快捷播放与卡片点击的分支（quickplay 不跳详情）。
19. 卡片行内 reread/delete/probe 动作与写后刷新。
20. 写成功置 dirty → 返回媒体墙触发重载（快照失效链）。

## 4. P0 退出条件核对（VUE-01）

| 退出条件 | 状态 |
|---|---|
| 每个当前 UI 操作都有目标页面、接口和验收项 | behavior-inventory.md 完成（约 130 项交互）；接口细节以 api-inventory.md（进行中）为准 |
| 每个页面的子组件、状态及事件归属已列明 | component-split.md 完成 |
| 所有全局行为有编号 | global-behaviors.md 完成（GL-01..GL-35 + C1..C4 差异） |
| 历史文档与代码差异已标注 | 计划 §2 已核对属实；dev-plan.md 偏差沿用计划 §2 结论 |
| 未知的写入/任务生命周期已核实 | 待 api-inventory.md 完成（reread/probe/scan 的副作用与 context 语义） |
