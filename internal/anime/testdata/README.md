# 动漫库夹具（ANI-02）

这些文件是**真实上游响应的、经过裁剪与脱敏的子集**，供 `internal/anime` 的解析与
映射测试离线使用。测试只读夹具、不联网；`refresh_fixtures.py` 是维护工具，平时不运行。

## 取得方式

- 取得日期：2026-10-12（UTC+8）。
- 抓取脚本：同目录 `refresh_fixtures.py`（记录全部 URL 与 User-Agent）。
- User-Agent：`emby-go-dev/0.1 (+https://github.com/; anime-library-plan ANI-02)`
  —— 上游可据此定位流量来源。
- 来源：`https://mikan.tangbai.cc`、`https://api.bgm.tv`。两者都是公共只读接口，
  未使用账号、Cookie 或个人 RSS token。

重新抓取：

```bash
python3 internal/anime/testdata/refresh_fixtures.py
```

脚本对每个夹具做必需标记自检，缺标记直接失败。自检是在第一版裁剪把
`bgm.tv/subject/...`（映射来源）和字幕组列表视图裁掉之后加的——没有自检的夹具
会「看起来像夹具、实际解析不了」。

## 脱敏

Mikan 页面即使匿名访问也带 ASP.NET 防伪字段 `__RequestVerificationToken`。
脚本把该字段的 `value` 及 `value="CfDJ8..."` 形状的值替换为 `REDACTED`；
自检会拒绝仍含 `CfDJ8` 的产物。除该令牌外没有做字段级改写。

裁剪只删减重复段落（表格行、分块条目），并留下
`<!-- 夹具裁剪：此处省略其余 N 个条目 -->` 注释，不伪造或重写任何业务字段。

## 夹具清单与覆盖

| 文件 | 覆盖 |
| --- | --- |
| `mikan/bangumi-page-tv-4063.html` | TV 番剧页整页（75KB）：Bangumi 明确链接、字幕组列表视图、发布表格、磁力 `data-*`、种子链接 |
| `mikan/bangumi-page-long-run-227.html` | 长寿番（342 条发布）：年度合集 `[1-42]`/`[01-06TV全集]`、`BOX`、`特别篇`；Bangumi 链接是 `http://`（非 https） |
| `mikan/home-season.html` | 季度目录首页：`data-dayofweek` 分块，含 0=星期日、1..6=星期一..六、7=剧场版、8=OVA |
| `mikan/search-keyword.html` | 搜索结果表：表头、`js-search-results-row`、每条含磁力与种子 |
| `mikan/rss-bangumi-4063.xml` | 公开番组 RSS：`guid` 为标题、`torrent` 自定义命名空间、`enclosure`、`pubDate` 无时区 |
| `bangumi/subject-tv-418229.json` | TV 条目：`type=2` 与 `platform="TV"` 并存；续作（别名含「第二季」） |
| `bangumi/subject-movie-3485.json` | 剧场版条目：`platform="剧场版"`、`total_episodes=1`、`eps=1` |
| `bangumi/episodes-tv-418229-p1.json` / `-p2.json` | 章节分页信封 `{data,total,limit,offset}`；第 2 集含空字段（`name_cn=""`、`duration=""`、`duration_seconds=0`） |
| `bangumi/episodes-movie-3485.json` | 剧场版章节 |
| `bangumi/episodes-sp-899.json` | 特别篇：`type=1`、**`ep=0`**、`sort` 为小数（570.5／762.5／1076.5） |
| `bangumi/search-subjects.json` | 搜索信封 `{data,total,limit,offset}`，`total=447` 远大于 `limit`；同时含 `platform="TV"` 与 `platform="剧场版"` |

## 覆盖缺口

仍未取得真实样本，实施 P2 时需补：

- **无映射条目**：抽样的 5 个当季蜜柑条目全部带 `bgm.tv/subject/...` 链接，未找到
  真实「无 Bangumi 链接」的番剧页。不能靠删锚点伪造——需要抓到真实样本后补入。
- **字幕组维度**：RSS 中没有字幕组字段，字幕组只出现在番剧页（`/Home/PublishGroup/{id}`）；
  已由 `bangumi-page-tv-4063.html` 覆盖，但缺一个字幕组 ID 变更／改名的样本。

## 实测得到的关键事实

以下都是本次抓取实测，不是推断；实施必须按此处理：

1. **`type` 与 `platform` 是两件事。** Bangumi 搜索与详情里所有动画条目的 `type` 恒为 `2`；
   TV 与剧场版靠 `platform`（`"TV"`／`"剧场版"`）区分。`search-subjects.json` 里
   3485 是 `platform="剧场版"`、其余是 `"TV"`，两者 `type` 都是 2。
2. **Mikan 的 40 位十六进制 ID 就是 infohash。** 页面路径 `/Home/Episode/{hash}` 与
   `magnet:?xt=urn:btih:{hash}` 同值，可直接作为发布唯一键。
3. **RSS 的 `guid` 是发布标题，不是稳定 ID。** 必须从 `link`／`enclosure` 解析发布 ID。
4. **同一发布的三种时间渲染互相差 8 小时。** 表格视图 `2026/10/10 08:08` 与 RSS
   `pubDate` `2026-10-10T08:08:00` 一致（UTC+8），而列表视图 `10/10/2026 12:08 AM 发布`
   相差 −8 小时（UTC）。不能混用；规范值取 RSS／表格视图并固定解释为 UTC+8。
5. **磁力只在 HTML 的 `data-*` 属性里**（`data-clipboard-text` / `data-magnet`），
   没有 `href="magnet:"`；`href` 指向第三方 `keepshare.org` 跳转，不是发布源。
6. **表格里的「MB」实为 MiB。** `578.4MB` 对应 `contentLength=606496384` 字节，即 ÷1024²。
7. **`data-dayofweek` 不是纯星期**：0=星期日、1..6=星期一..六、**7=剧场版、8=OVA**，
   且分块不按值排序。按 ISO 星期（7=周日）解析会错。
8. **OVA 分块只有前 2 个条目带 `data-bangumiid`**，其余 82 个只有
   `href="/Home/Bangumi/N"`。以 `data-bangumiid` 为条目锚点会静默漏掉绝大多数 OVA 条目。
9. **特别篇 `ep=0`、`sort` 是小数**（`570.5`／`762.5`／`1076.5`）。章节编号必须按
   十进制保存，且不能把 `ep` 当作集号。
10. **体积上限**：Mikan 搜索一次 `?searchstr=魔法少女` 返回 1000 行、4.4MB；长寿番页
    1.5MB。响应必须有大小上限，搜索结果不能全量长期入库。
