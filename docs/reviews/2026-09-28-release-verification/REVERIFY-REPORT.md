# aide 两项缺陷修复后复核报告

- 分支 HEAD：f9c4e02（前端已修复并重建容器）
- 容器：aide-aide-1（up 56s 时确认 healthy；/healthz 200、/api/config 带 token 200）
- 角色：只复核，未改源码、未 commit
- 证据根目录：`/Users/skyelan/Library/Mobile Documents/com~apple~CloudDocs/WorkStation/AI WorkStation/aide/artifacts/`

---

## 1. 会话桶布局（原 #7 FAIL）—— 复核 PASS

实测 `.run`（消息流）与 `.composer`（输入区）getBoundingClientRect：

| 窗口宽 | .run left–right（宽） | composer left–right（宽） | 边缘 |
|--------|------------------------|----------------------------|------|
| 1680 | 398–1238（**840**） | 398–1238（**840**） | **完全重合** ✓ |
| 1280 | 260–976（716） | 260–976（716） | **完全重合** ✓ |
| 600 | 20–580（560） | 20–580（560） | **完全重合** ✓ |

- `.run` 有效 max-width 现为 **840px**（原 macos.css 覆盖的 780px 已消除）；`.welcome` max-width 现为 **840px**（原 640px，已与 composer 同列协调）。
- 三种宽度下左右边缘坐标逐像素一致，**不再出现 composer 比消息列各外探 30px**（1680）或 4–6px（1280/600）的偏差。
- 截图：`artifacts/t7r-layout-1680-aligned.png`（1680 同屏对比，消息列与 composer 左右边缘对齐）。
- **结论：PASS。**

## 2. docx 批注锚点 + docHash（原 #6 部分）—— 复核 PASS

文件：工作区根 qa-comment-anchor.docx（同句出现 3 次）。

- **高亮落点**：`<mark class="comment-anchor-hl">` 现已包裹**正文 SPAN 内**的真实短语（`inStyle=false`，不再落进 docx-preview 注入的 `<style>`）。
  - 旧批注 anchorIndex=1 → 重载后 mark 落在**第 2 处**（occurrenceIndex=1）✓
  - 本次新建批注 anchorIndex=2 → 重载后 mark 落在**第 3 处**（occurrenceIndex=2，截图中「结尾段」上方那句）✓
- **docHash 修复**：新建批注 POST 发送的 hash 为 64 位 SHA-256 hex（`d6421e35cab75543c24e1806c47f168f3d85f4ca8cd43dd90aac7daaaafda99a`），与服务端对齐；文档未改动时该批注 **stale=false**（批注卡不再显示「锚点可能失效」）。
- API 实锤（`GET /api/comments?path=qa-comment-anchor.docx`）：
  - `QA-测试批注-第二处` | docHash=`MzY3Njg=`（旧 btoa 文件大小）| anchorIndex=1 | **stale=True**
  - `QA-复核批注-第三处` | docHash=`d6421e35…`（SHA-256）| anchorIndex=2 | **stale=False**
- 截图：`artifacts/t6r-docx-highlight-3rd.png`（第 3 处蓝色高亮 + 批注面板：新卡无「锚点可能失效」）。
- 备注：旧批注（上一轮用旧哈希创建）仍 stale=true，属预期的数据迁移残留（docHash 格式从 btoa(大小) 换为 SHA-256），不影响新批注；如需旧批注解除 stale，需重新批注或做哈希迁移。
- **结论：PASS。**

---

## 汇总

| 项 | 原结论 | 复核结论 |
|----|--------|----------|
| #7 会话桶布局 | FAIL（1600+ 外探 30px） | **PASS**（三宽度边缘逐像素重合，welcome 840 协调） |
| #6 docx 批注锚点 | 部分（anchorIndex 对，重载高亮错位+stale 恒真） | **PASS**（高亮落在正确正文处；新批注 SHA-256 下 stale=false） |

两项修复均已生效，无新发现问题。
