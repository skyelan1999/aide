# 长混合 Markdown PDF 重叠修复

任务：[markdown-pdf-mixed-layout-20261015](../../tasks/markdown-pdf-mixed-layout-20261015.json)。用户报告较长混合内容的 Markdown 导出 PDF 时部分内容叠在一起；未提供原始问题文档。

本机浏览器以实际 `markdown-export.js`、marked、html2canvas 和 jsPDF 生成旧版 16 页夹具 PDF。第 5 页直接复现列表项目 50–60 与后续标题／正文重叠，行内代码被压成窄列。原因是分页复制的大容器使用绝对定位，小块却保持普通流，且克隆宽度未固定；跨页格式子树还会被拆成各自占整行的片段。

修复统一源坐标与尺寸，完整保留短格式子树；超高文本按测量行分片，保留字体／位置／空白规则；冻结表格列宽，保留列表原生标记。一次规划文字行、短列表项／表格行及图像的避切区间，仍逐页裁剪和释放，不改权限、来源资源请求或后端。PDF 仍为栅格页；超过整页的单体对象无法保证不跨页。

## 验收范围

- 定向脚本覆盖同页块的绝对定位、源宽度与偏移、可见文本行分片和行内字体／代码空白，以及已有分页数、直接下载、失效文件守卫和挂起渲染取消。
- 实际浏览器使用仓库渲染库及导出模块：60 项嵌套列表、两段超高混合强调／斜体／行内代码／链接文字、70 行表格、120 行代码、SVG、真实 Mermaid 和 PNG 图片。先完成 16 页基础夹具；加入 Mermaid／PNG 后生成 17 页 A4，已通过 Poppler 渲染检查基础各页及新增图形末页。最终源码／运行镜像复测结果以任务账本为准。
- 证据位于 `.agent-state/markdown-pdf-mixed-layout/`：旧版 PDF 和第 5 页重叠图、修复版 PDF、逐页 PNG 与拼图。夹具是自建合成内容，不是用户原始问题文档或完整已鉴权工作台验收，也未测峰值内存。

## 交付与回滚

补查发现超高表格行的遍历在遇到第一个跨页单元格后提前退出，导致右侧列遗漏。新增回归先得到 1 列而非 2 列的失败，再修复为遍历所有相交单元格、缓存最早未完成位置；回归还检查下一页两列均继续且之前内容已裁剪。最终夹具增加两列各 90 行的单行表格，实际导出 19 页；Poppler 检查第 17–19 页，左右列 0–89 均保留，表格后段落正常。旧版／中间版验收不充当最终输入收据，最终 quick/full 和镜像另行更新。期间始终每次只开一个自建验收页，完成立即关闭。

当前基线 main `f07a4f9`，继承五个 vendor 删除不属于本任务，保持未提交。运行实例、完整离线包、主线合入和最终回归分别记录在任务账本；没有证据的阶段保持待完成。回退本批前端提交并恢复上一完整镜像 `997eda4026b5`，无需数据迁移；不升版、tag 或创建正式 Release。

## Final clean verification

Final snapshot `026e16440ab2683d3a12e1990ca11d34c8294ec0` passed all84 checks, including server race 520.569 seconds and vet. Fingerprint `d8a0e04baebe96014f5387165c39b807083ef3236376a814d28899c41e2ab505`. This is the final input receipt, replacing the initial cd588b3 acceptance. Main delivery is recorded separately in the task ledger.

Main release-check passed; pushed main `ac74412a896518c56aa971ceace1c20229e7a125` matched the remote. Final receipt changes ordinary docs only; no product/build input changes, version bump, tag or formal Release. Final runtime candidate `bf701a670f6a` retains the documented inherited vendor deletion scope.
