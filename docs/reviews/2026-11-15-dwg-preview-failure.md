# DWG 展开与异常样条预览修复验收（#913）

## 原因与修复

用户报告的真实只读引用图纸为 959,630 字节（AC1032）。固定 LibreDWG 0.14 转为 ASCII DXF 后为 6,685,450 字节、804,847 行，超出原 4 MiB 文件限制；CLI 在输出触及限制后仍可能以 0 退出，导致后端误报“未生成完整的二维预览”。直接提高后端容量仍会被前端原 4 MiB / 100,000 行限制拒绝。

DWG 转换输出改为小于 16 MiB，前端读取上限 16 MiB、1,000,000 行。转换结果触及文件容量上限优先返回 413；原有进程内存、CPU、取消、并发和来源身份校验保留。直接 DXF 的 4 MiB / 100,000 行及共用实体、几何、节点、文本和块递归限制保持原值。

原生验收随后发现，转换输出中少量 SPLINE 的拟合点含约 10²⁷⁸ 量级坐标，使视图范围扩张到几乎看不到图形。非有限或绝对值超过 10¹² 的二维样条坐标现在导致整条曲线跳过，不写入 SVG 和视图边界，并继续提示“部分实体未显示”。这不修复原 DWG、不承诺样条或完整 CAD 几何的准确性。

## 实际验证

- SDK 容器运行 `go test -mod=vendor ./internal/server -run TestDWGPreview -count=1 -timeout=3m` 通过。
- 同一固定真实转换器和本地图纸运行专项 `-race -v`，全部 `TestDWGPreview*` 通过，真实转换测试实际执行、未跳过。包含 6 MiB 输出、容量边界成功/失败退出、损坏输出、取消、并发、只读来源及失效身份回归。
- `node scripts/test_dxf_preview_bounds.cjs` 通过；新增 DWG 字节/行边界、保留几何预算，以及有限极值/Infinity/NaN 样条不能污染全图范围的回归。
- 用真实 DXF 调用现有解析器和实际渲染函数：6,841 个顶层实体、404 个块、23,415 个源实体；最终约 8,908 个 SVG 节点，视图范围恢复到正常量级。该检查自身使用 DOM 替身，原生视觉验收另列如下。
- 最终代码的 `python3 scripts/agent-route.py verify quick` 通过；标准构建内的 `go vet -mod=vendor ./...` 和应用编译通过。未运行本次修复的全量 Go suite，不把专项 race 视作全量门禁。
- 最终运行接口针对原引用返回 HTTP 200、`application/dxf`、6,685,450 字节并含完整 EOF；原文件下载 SHA-256 与更新前相同。
- 通过原 `start.command` 打开 Aide，在原引用目录双击原图纸；原生 WKWebView 显示主体多视图、图框、标注及材料表，显示“6841 个实体 · 部分实体未显示”。实际放大并点“适应”恢复全图后观察画面，不以实体计数或接口成功冒充视觉验收。
- 最终镜像的 `scripts/test_runtime_assets.py` 通过：非 root 资产可读性、离线运行和真实 GNU 2000/2018 转换等检查。

## 运行身份与保留

标准原目录 `bash scripts/aide.sh build` 已完成，最终镜像 `sha256:ff9691565f0a755a584ce38b4b4b7a466114c2b7b6921330cd0580ca601bb334`，源码身份为 `bd5c6456162e48346e499dadb41f9e8939bd541f-dirty.c1dea4bed47691537fe4c4ebf0cf491b8f4062787ed89358126b746d936cc3a9`；版本仍为 `0.1.19.0 RC2`。既有未提交构建输入保留，本次没有推送、合并、版本提升、tag 或 GitHub Release；修复源码留在本地工作目录，集中 Issue 仍用于跟踪上游交付。

更新前保留回退镜像 `aide-rollback:dwg-preview-failure-20261115` 和私有备份 `/Users/skyelan/WorkTrees/aide-runtime-backups/20261115-dwg-preview-failure`。备份跳过可再生成的 Go 构建缓存；源缓存不删除。更新后 health/integrity 均为 ok，86 个会话（含 5 个待审批）、36 个插件的启用/设置/版本、22 个既有未跟踪文件和 194 个受保护文件核对通过，只有预期的完整性基线因新二进制更新。挂载边界保持一致。

本次自建图纸标签页作为可查看结果保留，原工作台标签页保留。真实客户图纸及转换结果只留在被忽略的本机私有证据目录，没有加入 Git 或上传转换服务。没有以 CAD 软件对图纸作工程校核，也未复验本次未改变的所有布局、触屏和全部 CAD 版本。

详细本地证据：`.agent-state/dwg-preview-failure-20261115/` 下的 `go-real-race.log`、`quick-geometry.log`、`build-final.log`、`actual-api-result.json`、`production-verification.json` 与 `runtime-assets-final.log`。
