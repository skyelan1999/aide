# 六份 CAD 恢复与 PDF 全量复测

任务：`cad-remaining-pdf-20261115`；关联已有问题 #913。此记录仅含脱敏结论，用户图纸和逐文件名称留在本机报告，不上传公开仓库。

## 最终行为

此前失败的 6 份归档记录均可生成并打开预览。密集 CAD 线型实际需要 1,053,035 个路径点，原 500,000 点预算过低；改为有界的 2,000,000 点，独立的源数据库对象预算为 100,000，展开访问与绘制记录仍为 50,000。每进程 512 MiB 地址空间、15 秒 CPU、16 MiB 文件/输出和超时保持不变。源数据库包含未使用定义和字典，不能等同于可见模型实体。

一份转换后的布局字典异常由空名称 / 空句柄占位对触发：ezdxf 将下一有效名称与前一空句柄配对，最终把 GROUP 当成 LAYOUT。仅在临时 DXF 中移除这类空占位对，保留命名引用及全部几何；修复计入绘制提示。不能解析的代理实体仍明确计入未恢复数，其中 5 份先前失败记录各有 1 个代理实体未恢复。打开成功不代表这些实体已找回。

PDF 维持现有 PDF.js/OpenJPEG 解码保护。匹配 PDF.js 4.4.168 的 185 个离线 CMap / 标准字体 / 许可文件按 npm 完整性及逐文件 SHA-256 固定；设置本地资源 URL 并关闭系统字体探测，修复 CMap 缺失与未嵌入 SimSun 的加载错误。CJK 字形使用可用字体替代，不宣称恢复原字体外观。

含绘图操作但画面空白或解码失败的页面，自动请求同源受保护的 `/api/file/pdf-preview`。独立 Poppler 22.12.0-2+deb12u3 进程只接收私有 PDF 副本，按指定页生成最长边不超过 4096 的 PNG。沿用普通文件 / 来源身份检查、两个共享并发槽和 CAD 子进程资源限制；PNG 大小与尺寸均有界。关闭或缩放取消请求，过期代次不覆盖新画布，Blob URL 及时撤销。未执行云端转换，没有写回原文件。该接口不接收加密 PDF 密码。

## 实际验证

- 重新读取全部 453 份归档记录：285 DWG、140 个识别为 DWG 的 BAK、28 PDF。BAK 使用私有 `.dwg` 副本测试，原扩展名未改，不能据此宣称产品原生识别任意 `.bak` 文件。
- 全新实际 API 请求 425 次，全部 HTTP 200；413 种不同 CAD 内容在 Chromium 中完成 SVG 解码与非透明像素检查，全部可见。6 份修复记录另通过实际查看器加载，7 个平移 / 测量按钮均存在。
- PDF 28 份、40 页通过实际查看器逐页渲染和非白像素检查。此前空白的扫描 PDF 第 1–3 页由兼容进程恢复，第 4 页保留 PDF.js；字体异常 PDF 的中文图签可见、无字体 / CMap 加载错误。另有一份 JPEG 编码有非致命 MCU 提示，9 页均可见。
- 全部 453 个原始副本 SHA-256 与 ZIP SHA-256 复核一致；185 个资源摘要一致；最终二进制实际提供的 CMap 和标准字体摘要匹配。
- Go 专项与 race：`TestDWGPreview*`、`TestPDFPreview*` 通过；覆盖认证、只读、特殊文件拒绝、来源撤销、忙碌、进程取消、无效 PNG 和输出上限。便携 Go 测试中的真实转换器用例依赖显式环境，未设置的跳过不冒充真实解析；归档真实 API 检查另提供证据。
- `test_dwg_svg.py` 10 项通过；测量、直接 DXF 边界、锁屏输入 / 集群回归通过；PDF 画布几何 2240 组、图像预测器、字体程序及 JPX 保护回归通过。构建执行 Go vet。
- 原生 WKWebView：原始吊挂图显示 6888 个模型实体，7 个直接工具按钮；距离测量输出 7009.5102 图纸单位。扫描 PDF 显示“4 页 · 兼容预览”，第三页导航正常；中文 PDF 图签文字可见。临时测量清除并恢复平移，临时标签关闭，原有两个标签和引用浏览目录恢复。
- 重建后曾有旧文件页显示锁屏遮罩，但服务端锁状态实际为未锁；按标准 `start.command` 入口重连恢复，未改密码、空闲时间或服务端锁状态。没有将此现象当成一次真正的空闲锁屏，也未绕过锁屏。

逐文件本机报告：`/Users/skyelan/Downloads/Aide_图纸预览检查_20261115.html`，同名 JSON 包含构建、实体类型和逐页像素计数。结果为 287 份无提示、77 份修复 / 字体 / 编码提示、89 份有未支持实体或填充边界降级；打开失败 0、空白 PDF 页 0。提示与几何缺失分别列出。没有进行独立 CAD 逐实体比对，制造与关键尺寸核对仍属未验收范围。

## 本地交付与保护

标准本地构建，版本保持 `0.1.19.0 RC2`，未推送、合并、升版、打 tag 或创建公开 Release。最终镜像与构建身份记录于任务及私有运行收据。

86 个会话（含 5 个待审批）、36 个插件（ProfessionalProject 0.4.20）、194 个受保护文件及非任务草稿校验通过。备份在 `/Users/skyelan/WorkTrees/aide-runtime-backups/20261115-cad-remaining-pdf`；原镜像保留为 `aide-rollback:cad-remaining-pdf-20261115`。只关闭本轮测试页面和隔离服务，保留用户原生窗口、会话和数据。

原始日志和私有图纸证据位于 `.agent-state/cad-remaining-pdf-20261115/`。完整发行矩阵、Windows 原生组件、其它平台与公开交付未在本轮执行。

Final local runtime: `sha256:5e35dd6e17b5fba5760b86df5e503d6206f1e8dcfef06f848af3a6c8d5c77bfd`; build `bd5c6456162e48346e499dadb41f9e8939bd541f-dirty.2ddd160a30d5ff64bb1ba60eb235e8f9ebadf594e085f8be05b157a1a527b8c1`. After the complete archive rerun, only the English catalog was corrected (duplicate key and new translations). CAD/PDF preview source SHA-256 stayed identical; the final installed image passed six CAD requests and a real PDF page raster smoke. Quick: 345 checks PASS.
