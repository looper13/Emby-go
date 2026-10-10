#!/usr/bin/env bash
# make-fixtures.sh — 生成 VUE-01 真实服务采集用的媒体数据集（默认 306 个条目目录）。
#
# 用法:
#   bash make-fixtures.sh <MEDIA_ROOT> [--force]
#
# 数据集构成（确定性生成，可重复）:
#   L001..L300  常规条目: <code>.strm + <code>.nfo（ABF-001..ABF-300）
#               - i%10==3 的条目使用中文标题（深夜/中文标题/雨夜/梦幻 轮换）
#               - i==42 标题含 《》&"'<> 特殊字符（XML 转义后写入）
#               - i 为 5/88/142/217/299 的条目附带 poster.png + fanart.png（"有图"分支）
#               - 中文类型/标签/制片商按固定规则分布到若干条目
#   L301..L303  待补录: 只有 .strm 无 NFO（status=pending）
#   L304..L305  不兼容源: .strm 内容为 ftp://（扫描不读 STRM，实际仍按 NFO 计入 success）
#   L306        探测失败样例: .strm 指向 http://127.0.0.1:1/x.mp4（连接立即拒绝）
#   L307        可探测样例: .strm 指向本地媒体源 http://127.0.0.1:18098/probe-sample.mp4
#                （用 tools/live-env/serve-media.mjs 提供；用于采集成功的单条探测响应）
#
# 依赖: bash + ffmpeg（仅 5 个条目生成图片时需要）。全部数据只写入给定的临时目录。

set -euo pipefail

ROOT="${1:-}"
FORCE="${2:-}"
if [ -z "$ROOT" ]; then
  echo "usage: make-fixtures.sh <MEDIA_ROOT> [--force]" >&2
  exit 2
fi
if [ "$FORCE" = "--force" ]; then
  find "$ROOT" -mindepth 1 -maxdepth 1 -name 'L*' -exec rm -rf {} + 2>/dev/null || true
fi
mkdir -p "$ROOT"

CN_TITLES=("深夜的测试影片" "中文标题示例" "雨夜剧场版" "梦幻片段")

# xml_escape: 只处理文本节点必须转义的 & < >
xml_escape() {
  printf '%s' "$1" | sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

emit_nfo() {
  # args: nfo_path code xml_title extra_genres extra_tags studio year runtime plot set_name extra_actor
  local nfo_path="$1" code="$2" xtitle="$3" genres="$4" tags="$5" studio="$6" year="$7" runtime="$8" plot="$9" setname="${10}" extra_actor="${11:-}"
  {
    printf '%s\n' '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    printf '%s\n' '<movie>'
    printf '  <num>%s</num>\n' "$code"
    printf '  <title>%s</title>\n' "$xtitle"
    printf '  <originaltitle>%s</originaltitle>\n' "$xtitle"
    printf '  <plot>%s</plot>\n' "$plot"
    printf '  <year>%s</year>\n' "$year"
    printf '  <premiered>%s-0%s-15</premiered>\n' "$year" "$(( year % 9 + 1 ))"
    printf '  <runtime>%s</runtime>\n' "$runtime"
    printf '  <rating>%s.%s</rating>\n' "$(( 5 + year % 4 ))" "$(( code_num % 10 ))"
    printf '%s\n' "$genres"
    printf '%s\n' "$tags"
    printf '  <studio>%s</studio>\n' "$studio"
    printf '  <director>Director-%s</director>\n' "$code"
    printf '  <actor><name>%s</name></actor>\n' "$( [ $(( code_num % 5 )) -eq 0 ] && echo '演员甲' || echo 'Actor One' )"
    printf '  <actor><name>Actor Two</name></actor>\n'
    if [ -n "$extra_actor" ]; then
      printf '%s\n' "$extra_actor"
    fi
    if [ -n "$setname" ]; then
      printf '  <set><name>%s</name></set>\n' "$setname"
    fi
    printf '%s\n' '</movie>'
  } > "$nfo_path"
}

# ---- L001..L300: 常规条目 ----
for i in $(seq 1 300); do
  code_num=$i
  code=$(printf 'ABF-%03d' "$i")
  dir="$ROOT/L$(printf '%03d' "$i")"
  mkdir -p "$dir"

  # 标题
  if [ "$i" -eq 42 ]; then
    title="$code 《特殊符号》&\"引号'&<尖括号>"
  elif [ $(( i % 10 )) -eq 3 ]; then
    title="$code ${CN_TITLES[$(( (i / 10) % 4 ))]}"
  else
    title="$code Sample Movie"
  fi
  # 图片条目的标题固定为可检索的中文/英文（覆盖"有图+中文标题"分支）
  case "$i" in
    5)   title="ABF-005 深夜的测试影片" ;;
    88)  title="ABF-088 中文标题示例" ;;
    142) title="ABF-142 梦幻片段" ;;
    217) title="ABF-217 Sample Movie" ;;
    299) title="ABF-299 雨夜剧场版" ;;
  esac
  xtitle=$(xml_escape "$title")

  # 类型 / 标签 / 制片商
  genres="  <genre>Drama</genre>"
  [ $(( i % 5 )) -eq 0 ] && genres="$genres
  <genre>剧情</genre>"
  [ $(( i % 11 )) -eq 0 ] && genres="$genres
  <genre>悬疑</genre>"

  tags="  <tag>Tag-A</tag>"
  [ $(( i % 4 )) -eq 0 ] && tags="  <tag>中文标签</tag>"
  [ $(( i % 9 )) -eq 0 ] && tags="$tags
  <tag>Special-Tag</tag>"
  [ "$i" -eq 42 ] && tags="$tags
  <tag>《特殊》标签</tag>"

  if [ $(( i % 6 )) -eq 0 ]; then studio="中文厂商"; elif [ $(( i % 2 )) -eq 0 ]; then studio="Studio B"; else studio="Studio A"; fi

  setname=""
  [ $(( i % 50 )) -eq 0 ] && setname="合集甲"

  # 稀有共享特征簇（约 3% 影片）：Similar 端点会跳过 >10% 的高频特征，
  # 只给这些条目加共享的稀有演员/标签，才能召回非空相似列表（覆盖 Emby Item 形状）。
  rare_actor=""
  case "$i" in
    3|5|43|83|123|163|203|243|283)
      tags="$tags
  <tag>深夜系列</tag>"
      rare_actor="  <actor><name>演员乙</name></actor>"
      ;;
  esac

  year=$(( 2015 + i % 10 ))
  runtime=$(( 60 + i % 90 ))
  plot="L$i 的剧情简介：用于媒体墙与详情页夹具采集（混合中文 plot 内容）。"

  printf 'http://127.0.0.1:1/media/%s.mp4\n' "$code" > "$dir/$code.strm"
  emit_nfo "$dir/$code.nfo" "$code" "$xtitle" "$genres" "$tags" "$studio" "$year" "$runtime" "$plot" "$setname" "$rare_actor"

  # 5 个条目生成本地图片（覆盖"有图"分支；ffmpeg lavfi 纯色图）
  case "$i" in
    5)   ffc="red";    fbc="darkblue" ;;
    88)  ffc="green";  fbc="purple" ;;
    142) ffc="orange"; fbc="teal" ;;
    217) ffc="yellow"; fbc="maroon" ;;
    299) ffc="pink";   fbc="navy" ;;
    *)   ffc=""; fbc="" ;;
  esac
  if [ -n "$ffc" ]; then
    ffmpeg -y -loglevel error -f lavfi -i "color=c=$ffc:s=320x480" -frames:v 1 "$dir/poster.png"
    ffmpeg -y -loglevel error -f lavfi -i "color=c=$fbc:s=1280x720" -frames:v 1 "$dir/fanart.png"
  fi
done

# ---- L301..L303: 待补录（无 NFO）----
for i in 301 302 303; do
  code=$(printf 'ABF-%03d' "$i")
  dir="$ROOT/L$i"
  mkdir -p "$dir"
  printf 'http://127.0.0.1:1/media/%s.mp4\n' "$code" > "$dir/$code.strm"
done

# ---- L304..L305: ftp 源（不兼容协议；附 NFO）----
for i in 304 305; do
  code=$(printf 'ABF-%03d' "$i")
  dir="$ROOT/L$i"
  mkdir -p "$dir"
  printf 'ftp://example.invalid/media/%s.mp4\n' "$code" > "$dir/$code.strm"
  code_num=$i
  emit_nfo "$dir/$code.nfo" "$code" "$code FTP 源测试" "  <genre>Drama</genre>" "  <tag>Tag-A</tag>" "Studio A" 2020 90 "ftp 源不兼容测试条目。" ""
done

# ---- L306: 探测失败样例（http 死链，连接立即拒绝）----
code_num=306
dir="$ROOT/L306"
mkdir -p "$dir"
printf 'http://127.0.0.1:1/x.mp4\n' > "$dir/ABF-306.strm"
emit_nfo "$dir/ABF-306.nfo" "ABF-306" "ABF-306 探测失败样例" "  <genre>Drama</genre>" "  <tag>Tag-A</tag>" "Studio A" 2021 88 "探测失败样例：strm 指向 http://127.0.0.1:1/x.mp4。" ""

# ---- L307: 可探测样例（本地媒体源 18098，成功探测响应夹具）----
code_num=307
dir="$ROOT/L307"
mkdir -p "$dir"
printf 'http://127.0.0.1:18098/probe-sample.mp4\n' > "$dir/ABF-307.strm"
emit_nfo "$dir/ABF-307.nfo" "ABF-307" "ABF-307 本地可探测样例" "  <genre>Drama</genre>" "  <tag>Tag-A</tag>" "Studio A" 2022 93 "本地媒体源样例：strm 指向 http://127.0.0.1:18098/probe-sample.mp4。" ""

echo "fixtures written to: $ROOT"
echo "  L001..L300 regular (5 with images), L301..L303 pending, L304..L305 ftp, L306 probe-fail, L307 probe-ok"
