#!/usr/bin/env python3
"""重新抓取并裁剪动漫库夹具（ANI-02）。

公开站点只用于少量连通性与真实字段验证，不作为 CI 依赖：本脚本是维护工具，
平时不运行；夹具已提交到仓库，测试只读夹具、不联网。

用法：
    python3 internal/anime/testdata/refresh_fixtures.py [输出目录]

两点必须遵守，否则夹具不能用：

1. **脱敏**：Mikan 页面会带 ASP.NET 防伪字段 ``__RequestVerificationToken``，
   即使匿名访问也存在。脚本把所有 ``value="..."`` 换成 ``REDACTED``。
2. **真实性**：只做裁剪与脱敏，不重写任何业务字段、不伪造条目。脚本末尾对每个
   夹具做必需标记自检，缺标记直接报错退出——避免产出「看着像夹具、实际解析不了」
   的文件（本脚本第一版就踩过：裁掉了 Bangumi 链接）。

抓取哪些 URL、每个夹具覆盖的解析分支，以及覆盖缺口，见同目录 README.md。
"""

from __future__ import annotations

import gzip
import html
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

# 可识别的 User-Agent：上游可据此定位流量来源（见计划 5.2）。
UA = "emby-go-dev/0.1 (+https://github.com/; anime-library-plan ANI-02)"

MIKAN = "https://mikan.tangbai.cc"
BGM = "https://api.bgm.tv"

# 合集／特别篇命名：这些发布不能自动映射到某一集（见计划 6.3、5.1）。
SPECIAL_TITLE = re.compile(r"合集|全集|BOX|特别篇|OVA|OAD|SP\d|Vol\.\s*\d")

TOKEN_VALUE = re.compile(r'(name="__RequestVerificationToken"[^>]*?value=")[^"]*(")')
SECRET_SHAPE = re.compile(r'value="CfDJ8[^"]*"')


def scrub(text: str) -> str:
    """脱敏：清掉防伪令牌等会话相关值。"""
    text = TOKEN_VALUE.sub(r"\1REDACTED\2", text)
    return SECRET_SHAPE.sub('value="REDACTED"', text)


def get(url: str, *, data: bytes | None = None, headers: dict | None = None) -> bytes:
    hdrs = {"User-Agent": UA, "Accept-Encoding": "gzip"}
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(url, data=data, headers=hdrs)
    last: Exception | None = None
    for attempt in range(3):
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                body = resp.read()
                if resp.headers.get("Content-Encoding") == "gzip":
                    body = gzip.decompress(body)
                return body
        except urllib.error.HTTPError as exc:
            last = exc
            # 429 尊重 Retry-After；5xx 有限退避（见计划 5.3）。
            if exc.code == 429 or 500 <= exc.code < 600:
                delay = exc.headers.get("Retry-After") or str(2 ** attempt)
                time.sleep(min(float(delay), 10))
                continue
            raise
        except (urllib.error.URLError, TimeoutError) as exc:
            last = exc
            time.sleep(2 ** attempt)
    raise RuntimeError(f"抓取失败: {url}: {last}")


def text(raw: bytes) -> str:
    return raw.decode("utf-8", errors="replace")


def write(out_dir: str, rel: str, payload: bytes | str) -> None:
    path = os.path.join(out_dir, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    if isinstance(payload, str):
        payload = payload.encode("utf-8")
    with open(path, "wb") as fh:
        fh.write(payload)
    print(f"  {rel:46} {len(payload):>9,} bytes")


def head_of(page: str) -> str:
    end = page.find("</head>")
    return page[: end + len("</head>")] if end > 0 else ""


# ------------------------------------------------------------- 裁剪与自检

def require(rel: str, content: str, markers: list[str]) -> None:
    """自检：夹具必须含全部必需标记，否则宁可失败也不提交无用夹具。"""
    plain = html.unescape(content)
    missing = [m for m in markers if m not in plain]
    if missing:
        raise SystemExit(f"夹具自检失败 {rel}: 缺少 {missing}")
    for secret in ("CfDJ8",):
        if secret in content:
            raise SystemExit(f"夹具自检失败 {rel}: 仍含疑似令牌 {secret}")


def trim_bangumi_page(page: str, *, special: int, ordinary: int) -> str:
    """番剧页：保留真实头部（含 Bangumi 链接）与表格中的代表性发布行。

    表格行优先保留合集／特别篇等特殊命名，再补少量普通行；不重写任何字段。
    """
    page = scrub(page)
    info = re.findall(r'<p class="bangumi-info">.*?</p>', page, re.S)

    rows = re.findall(r"<tr\b.*?</tr>", page, re.S)
    special_rows, ordinary_rows = [], []
    for row in rows:
        plain = html.unescape(row)
        if SPECIAL_TITLE.search(plain):
            special_rows.append(row)
        else:
            ordinary_rows.append(row)
    kept = special_rows[:special] + ordinary_rows[:ordinary]

    tstart = page.find("<table")
    tclose = page.find(">", tstart) + 1 if tstart >= 0 else 0
    table_open = page[tstart:tclose] if tstart >= 0 else "<table>"

    return "\n".join(
        [head_of(page), "<body>", *info, table_open, *kept, "</table>", "</body>"]
    )


ITEM_LINK = 'href="/Home/Bangumi/'


def cap_day_block(block: str, n: int) -> tuple[str, int]:
    """把一天的分块截到前 n 个条目。

    条目锚点用 href="/Home/Bangumi/N"，不用 data-bangumiid：实测 OVA 分块
    只有前 2 个条目带 data-bangumiid，其余 82 个只有 href，按前者裁剪（或解析）
    会静默丢掉绝大多数 OVA 条目。返回被略过的条目数，在夹具里留下显式痕迹。
    """
    cut = 0
    for _ in range(n):
        j = block.find(ITEM_LINK, cut)
        if j < 0:
            return block, 0
        cut = j + 1
    nxt = block.find(ITEM_LINK, cut)
    if nxt < 0:
        return block, 0
    omitted = block.count(ITEM_LINK) - n
    return (
        block[:nxt] + f'<!-- 夹具裁剪：此处省略其余 {omitted} 个条目 -->\n',
        omitted,
    )


def trim_home_page(page: str, *, want_days: list[str], items_per_day: int) -> str:
    """首页季度目录：按 data-dayofweek 分块，各保留前若干条目。

    注意：data-dayofweek 不是纯星期。实测取值与含义为 0=星期日、1..6=星期一..六、
    7=剧场版、8=OVA，且页面上并非按值排序（1,0,6,5,4,3,2,7,8）。因此这里按值选取，
    不假设顺序，也不把它当 0..6 的星期处理。
    """
    page = scrub(page)
    chunks = page.split('<div class="sk-bangumi"')
    by_day: dict[str, str] = {}
    for chunk in chunks[1:]:
        m = re.match(r'\s*data-dayofweek="([^"]*)"', chunk)
        if not m:
            continue
        block, _ = cap_day_block('<div class="sk-bangumi"' + chunk, items_per_day)
        by_day.setdefault(m.group(1), block)
    out = [chunks[0].split("</head>")[0] + "</head>", "<body>"]
    for day in want_days:
        if day in by_day:
            out.append(by_day[day])
    out.append("</body>")
    return "\n".join(out)


def trim_search_page(page: str, *, rows: int) -> str:
    """Mikan 搜索是客户端渲染的一张 1000 行表；保留表头与前若干行。"""
    page = scrub(page)
    thead = re.search(r"<thead>.*?</thead>", page, re.S)
    body_rows = re.findall(r'<tr class="js-search-results-row".*?</tr>', page, re.S)
    return "\n".join(
        [
            head_of(page),
            "<body>",
            '<table class="table table-striped tbl-border fadeIn">',
            thead.group(0) if thead else "",
            "<tbody>",
            *body_rows[:rows],
            "</tbody></table>",
            "</body>",
        ]
    )


def pick_search_items(obj: dict, n: int) -> str:
    """搜索响应：保留真实信封（total/limit/offset），并保证涵盖非 TV 媒介。

    响应里 type 恒为 2，区分 TV／剧场版只能看 platform；若只截前 n 条就会丢掉
    剧场版样本（它在第 6 位），所以显式把首个非 TV 条目一起留下。
    """
    data = obj.get("data", [])
    off_tv = next((it for it in data if it.get("platform") != "TV"), None)
    chosen = [it for it in data[:n]]
    if off_tv is not None and off_tv not in chosen:
        chosen.append(off_tv)
    chosen.sort(key=lambda it: data.index(it))
    trimmed = dict(obj)
    trimmed["data"] = chosen
    return json.dumps(trimmed, ensure_ascii=False, indent=2) + "\n"


def main() -> int:
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.abspath(__file__))
    checks: dict[str, list[str]] = {}

    print("Mikan HTML")
    # TV 番剧页只有 ~74KB，且三个相关区域（Bangumi 链接、表格视图、字幕组列表视图）
    # 分散在页面不同位置，裁剪容易漏掉其一，因此整页保留、只做脱敏。
    write(out, "mikan/bangumi-page-tv-4063.html", scrub(text(get(f"{MIKAN}/Home/Bangumi/4063"))))
    checks["mikan/bangumi-page-tv-4063.html"] = [
        "https://bgm.tv/subject/418229",          # 明确映射来源（计划 2.3）
        'class="m-bangumi-item js-sort-item"',   # 字幕组列表视图
        "10/10/2026 12:08 AM 发布",               # 列表视图时间（UTC 渲染）
        'class="magnet-link-wrap"',              # 表格视图标题链接
        'data-clipboard-text="magnet',            # 磁力：HTML 只在 data-* 属性里
        'class="js-magnet magnet-link"',
        "/Download/",                             # 种子链接
        "2026/10/10 08:08",                       # 表格视图时间（UTC+8 渲染）
        "578.4MB",                                # 大小（MiB 语义）
        "__RequestVerificationToken",             # 令牌字段保留，值已脱敏
    ]

    # 长寿番：342 条发布，含年度合集/BOX/特别篇，且 Bangumi 链接是 http（不是 https）。
    long_run = text(get(f"{MIKAN}/Home/Bangumi/227"))
    write(out, "mikan/bangumi-page-long-run-227.html", trim_bangumi_page(long_run, special=10, ordinary=2))
    checks["mikan/bangumi-page-long-run-227.html"] = [
        "http://bgm.tv/subject/899",       # scheme 与 4063 不同
        "合集",
        "特别篇",
        'class="magnet-link-wrap"',
    ]

    write(out, "mikan/home-season.html", trim_home_page(text(get(f"{MIKAN}/")), want_days=["1", "0", "7", "8"], items_per_day=3))
    checks["mikan/home-season.html"] = [
        'data-dayofweek="1"',   # 星期一
        'data-dayofweek="0"',   # 星期日（非 ISO 的 7）
        'data-dayofweek="7"',   # 剧场版：与星期共用同一维度
        'data-dayofweek="8"',   # OVA：同上
        "星期一",
        "星期日",
        "剧场版",
        "OVA",
        "/Home/Bangumi/",
        "an-text",
    ]

    write(out, "mikan/search-keyword.html", trim_search_page(text(get(f"{MIKAN}/Home/Search?searchstr=%E9%AD%94%E6%B3%95%E5%B0%91%E5%A5%B3")), rows=5))
    checks["mikan/search-keyword.html"] = [
        'class="js-search-results-row"',
        "js-episode-select",
        "data-magnet=",
        "magnet-link-wrap",
        "<thead>",
    ]

    print("Mikan RSS")
    write(out, "mikan/rss-bangumi-4063.xml", scrub(text(get(f"{MIKAN}/RSS/Bangumi?bangumiId=4063"))))

    print("Bangumi API")
    write(out, "bangumi/subject-tv-418229.json", get(f"{BGM}/v0/subjects/418229"))
    write(out, "bangumi/subject-movie-3485.json", get(f"{BGM}/v0/subjects/3485"))
    write(out, "bangumi/episodes-tv-418229-p1.json", get(f"{BGM}/v0/episodes?subject_id=418229&limit=2&offset=0"))
    write(out, "bangumi/episodes-tv-418229-p2.json", get(f"{BGM}/v0/episodes?subject_id=418229&limit=2&offset=2"))
    write(out, "bangumi/episodes-movie-3485.json", get(f"{BGM}/v0/episodes?subject_id=3485&limit=10&offset=0"))
    # 特别篇：ep=0 且 sort 为小数（570.5/762.5/1076.5）——「编号不能转整数」的证据。
    write(out, "bangumi/episodes-sp-899.json", get(f"{BGM}/v0/episodes?subject_id=899&type=1&limit=5&offset=0"))

    search = json.loads(get(
        f"{BGM}/v0/search/subjects",
        data=json.dumps({"keyword": "魔法少女育成计划", "filter": {"type": [2]}}).encode(),
        headers={"Content-Type": "application/json"},
    ))
    write(out, "bangumi/search-subjects.json", pick_search_items(search, 4))
    checks["bangumi/search-subjects.json"] = ["魔法少女奈叶 The MOVIE 1st", '"platform": "剧场版"']

    print("自检")
    for rel, markers in checks.items():
        path = os.path.join(out, rel)
        with open(path, "rb") as fh:
            require(rel, fh.read().decode("utf-8", "replace"), markers)
        print(f"  ok  {rel}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
