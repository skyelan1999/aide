# UI 深度走查 Checklist（2026-09-28 · aide v65 · 127.0.0.1:8097 · 最终版）

走查方式：Playwright 真实界面截图；宽屏 1440×900 / 窄屏 900×700 / 很窄 600×800；空闲态 + 流式进行中；light / dark / green 三主题。截图目录：`evidence/ui/`。一轮报 7 条 P3，二轮修复复核全部 PASS。

## 8 维度结论

| # | 维度 | 结论 | 一句话 | 主要截图 |
|---|------|------|--------|----------|
| 1 | 视觉一致性 | **PASS** | token 体系完整、三主题一致；圆角已统一为 8px | FIXED-3-sidebar-seam.png |
| 2 | 组件一致性 | **PASS** | 按钮/dialog/菜单/chip/tab 统一；外观卡重复标签已去除 | FIXED-6-appearance-segmented.png |
| 3 | 交互态完整 | **PASS** | hover/focus-ring/disabled 齐备；logo 焦点环残留已消除 | FIXED-4-brand-no-focus-ring.png |
| 4 | 状态完备 | **PASS** | 空会话/权限三档到位；搜索空态已补「没有匹配的聊天」 | FIXED-5c-full-no-match-palette.png |
| 5 | 布局与响应式 | **PASS** | 900px 隐藏文件栏、600px 三栏折叠；ESC/backdrop 正常 | dim5-narrow-900-green-session.png, dim5-very-narrow-600-green-session.png |
| 6 | 滚动体验 | **PASS** | 流式自动跟随、「回到底部」↓、宽表横向滚动正常 | dim6-scrolled-up.png |
| 7 | 已做细节回归 | **PASS** | 流式绿色光标、发送↔停止同键、插话、排队按钮均在 | dim7-streaming-mid-1.png, dim7-queued-interject.png |
| 8 | 可访问性 | **PASS** | Tab 焦点环清晰、aria-label 齐；三主题主按钮白字对比度全部 ≥4.5 | FIXED-1-light-primary-button.png, FIXED-1b-dark-primary.png, FIXED-1c-green-primary.png |

## 主按钮白字对比度（二轮实测，WCAG）
- light：#0064d2 → **5.59:1** ✓
- dark：#0066d6 → **5.42:1** ✓
- green：#176040 → **7.54:1** ✓
（均 ≥4.5:1 AA 标准）

## 其他关键复测数值
- --faint 已加深 #6e6e76 on white = **5.05:1**（原 3.25）✓
- 选中会话行文字 #0064d2 on #e1eaf8 ≈ **4.9:1**（原 4.05）✓
- workspace-label 上圆角 = assistant-entry 下圆角 = **8px** ✓

## 重点回归项确认（此前反馈）
- **左下角白色块**：不存在。三主题下左下角与侧栏底色统一。
- **侧栏 hover 圆角缺失**：已修复，hover 小秘行整卡高亮、圆角连续。

## 非 bug 说明（刻意设计，未上报）
- 状态语义色（运行中绿点、危险红、选中蓝）为刻意语义色。
- 600px 窄屏 markdown 表格换行密集属自适应而非裁切。
