#!/usr/bin/env bash
# aide 版本管理（FR-65 / LIM-24）
#
# 用法：
#   bash scripts/version.sh                     显示当前版本
#   bash scripts/version.sh show                同上
#   bash scripts/version.sh bump <档位> -m "说明"  升级版本：product | major | feature | daily
#   bash scripts/version.sh patch -m "说明"       同一版本补丁：仅 RC+1
#   bash scripts/version.sh note -m "说明"        在当前版本节下追加 release note 条目
#   bash scripts/version.sh tag                  为当前版本在 HEAD 打 annotated tag（幂等）
#   bash scripts/version.sh check                校验 version.md 结构
#   bash scripts/version.sh install-hooks        安装 prepare-commit-msg 钩子（提交自动标注版本号）
#
# 规则（LIM-24）：
#   - 版本号格式 X.Y.Z.W RCn：产品级.重大.大版本.日常 + RC 补丁号；bump 高位 → 低位清零、RC1
#   - bump/patch 成功后：更新 version.md → git commit（消息带版本号）→ git tag -a vX.Y.Z.W-RCn
#   - git tag 仅允许打在 main 分支：bump/patch/note/tag 在非 main 分支直接报错退出
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION_FILE="$ROOT/version.md"
HOOK_SRC="$ROOT/scripts/git-hooks/prepare-commit-msg"
HOOK_DST="$ROOT/.git/hooks/prepare-commit-msg"
VER_RE='[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+ RC[0-9]+'

usage() {
  echo "用法："
  echo "  bash scripts/version.sh [show]                       显示当前版本"
  echo "  bash scripts/version.sh bump <档位> -m \"说明\"         升级：product | major | feature | daily"
  echo "  bash scripts/version.sh patch -m \"说明\"              同一版本补丁：仅 RC+1"
  echo "  bash scripts/version.sh note -m \"说明\"               在当前版本下追加 release note"
  echo "  bash scripts/version.sh tag                          为当前版本在 HEAD 打 tag（仅 main）"
  echo "  bash scripts/version.sh check                        校验 version.md 结构"
  echo "  bash scripts/version.sh install-hooks                安装提交自动标注钩子"
}

current_version() {
  grep -oE "$VER_RE" "$VERSION_FILE" 2>/dev/null | head -1 || true
}

require_main() {
  local branch
  branch="$(git -C "$ROOT" branch --show-current 2>/dev/null || true)"
  if [ "$branch" != "main" ]; then
    echo "错误：版本管理操作（bump/patch/note/tag）仅允许在 main 分支执行；当前分支：${branch:-未知}" >&2
    echo "提示：请先完成功能开发并合并回 main，再在 main 上执行版本升级。" >&2
    exit 1
  fi
}

tag_name() { # X.Y.Z.W RCn -> vX.Y.Z.W-RCn
  echo "v$(echo "$1" | sed 's/ RC/-RC/')"
}

bump_digits() { # $1=level（product|major|feature|daily|patch），输出全局 NEW
  local level="$1" cur v rc P M F D
  cur="$(current_version)"
  if [ -z "$cur" ]; then
    echo "错误：version.md 中未找到版本号（期望格式 X.Y.Z.W RCn）" >&2
    exit 1
  fi
  v="${cur%% *}"
  rc="${cur##*RC}"
  IFS='.' read -r P M F D <<< "$v"
  case "$level" in
    product) P=$((P+1)); M=0; F=0; D=0; rc=1 ;;
    major)   M=$((M+1)); F=0; D=0; rc=1 ;;
    feature) F=$((F+1)); D=0; rc=1 ;;
    daily)   D=$((D+1)); rc=1 ;;
    patch)   rc=$((rc+1)) ;;
    *) echo "错误：未知档位 $level（可选：product / major / feature / daily）" >&2; exit 2 ;;
  esac
  NEW="$P.$M.$F.$D RC$rc"
}

note_text() {
  if [ -n "${NOTE:-}" ]; then
    echo "$NOTE"
  else
    echo "日常更新"
  fi
}

rewrite_version() { # 改写当前版本行为新版本并插入新一节
  local new="$1" date note
  date="$(date +%Y-%m-%d)"
  note="$(note_text)"
  awk -v new="$new" -v date="$date" -v note="$note" '
    $0 ~ /\*\*当前版本：/ {
      print "**当前版本：" new "**"
      print ""
      print "## " new "（" date "）"
      print ""
      print "- " note
      print ""
      next
    }
    { print }
  ' "$VERSION_FILE" > "$VERSION_FILE.tmp"
  mv "$VERSION_FILE.tmp" "$VERSION_FILE"
}

append_note() { # 在当前版本节标题后插入一条 release note
  local note
  note="$(note_text)"
  awk -v note="$note" '
    done == 0 && $0 ~ /^## / {
      print $0
      print ""
      print "- " note
      print ""
      done = 1
      next
    }
    { print }
  ' "$VERSION_FILE" > "$VERSION_FILE.tmp"
  mv "$VERSION_FILE.tmp" "$VERSION_FILE"
}

commit_version() { # $1=版本号（写入提交消息）
  git -C "$ROOT" add version.md
  git -C "$ROOT" commit -m "version: $1 — $(note_text)"
}

make_tag() { # $1=版本号
  local tag existing head tagged
  tag="$(tag_name "$1")"
  if existing="$(git -C "$ROOT" tag -l "$tag")" && [ -n "$existing" ]; then
    head="$(git -C "$ROOT" rev-parse HEAD)"
    tagged="$(git -C "$ROOT" rev-list -n 1 "$tag" 2>/dev/null || true)"
    if [ "$head" = "$tagged" ]; then
      echo "tag $tag 已存在且指向当前提交，跳过"
      return 0
    fi
    echo "错误：tag $tag 已存在但指向其他提交（$tagged），请先处理冲突" >&2
    exit 1
  fi
  git -C "$ROOT" tag -a "$tag" -m "$1 — $(note_text)"
  echo "已打 tag: $tag"
}

check() {
  local cur
  cur="$(current_version)"
  if [ -z "$cur" ]; then
    echo "校验失败：未找到当前版本行" >&2
    exit 1
  fi
  if ! grep -q "^# aide 版本记录" "$VERSION_FILE"; then
    echo "校验失败：缺少标题行" >&2
    exit 1
  fi
  if ! grep -qE '^## '"$VER_RE"'（[0-9-]+）' "$VERSION_FILE"; then
    echo "校验失败：缺少版本节" >&2
    exit 1
  fi
  echo "校验通过：当前版本 $cur"
}

install_hooks() {
  mkdir -p "$(dirname "$HOOK_DST")"
  cp "$HOOK_SRC" "$HOOK_DST"
  chmod +x "$HOOK_DST"
  echo "钩子已安装：.git/hooks/prepare-commit-msg（每次提交自动标注版本号）"
}

COMMAND="${1:-show}"
shift || true
NOTE=""
LEVEL=""
while [ $# -gt 0 ]; do
  case "$1" in
    -m) NOTE="${2:-}"; shift 2 ;;
    *)
      if [ "$COMMAND" = "bump" ] && [ -z "$LEVEL" ]; then
        LEVEL="$1"
        shift
      else
        echo "错误：未知参数 $1" >&2
        usage
        exit 2
      fi
      ;;
  esac
done

case "$COMMAND" in
  show|"")
    v="$(current_version)"
    if [ -z "$v" ]; then
      echo "错误：version.md 中未找到版本号" >&2
      exit 1
    fi
    echo "$v"
    ;;
  check)
    check
    ;;
  install-hooks)
    install_hooks
    ;;
  tag)
    require_main
    cur="$(current_version)"
    [ -n "$cur" ] || { echo "错误：version.md 中未找到版本号" >&2; exit 1; }
    make_tag "$cur"
    ;;
  note)
    require_main
    cur="$(current_version)"
    [ -n "$cur" ] || { echo "错误：version.md 中未找到版本号" >&2; exit 1; }
    append_note
    commit_version "$cur"
    echo "已为当前版本 $cur 追加 release note"
    ;;
  bump)
    require_main
    [ -n "$LEVEL" ] || { echo "错误：bump 需要指定档位（product / major / feature / daily）" >&2; usage; exit 2; }
    bump_digits "$LEVEL"
    rewrite_version "$NEW"
    commit_version "$NEW"
    make_tag "$NEW"
    echo "版本已升级：$(current_version)"
    ;;
  patch)
    require_main
    bump_digits "patch"
    rewrite_version "$NEW"
    commit_version "$NEW"
    make_tag "$NEW"
    echo "版本已升级：$(current_version)"
    ;;
  *)
    usage
    exit 2
    ;;
esac
