# 星图原文定位与后台扫描整合

任务：[starmap-native-preview-20261016](../../tasks/starmap-native-preview-20261016.json)。总目标仍是连续工作、历史、授权、成果和知识星图十二项；Windows 由用户验收，真实向量测试暂缓。

本轮从实际干净 main `0ac5f757` 及原预览分支 `43d1f759` 继续，合并为开发提交 `4e260323`。此前后台扫描批次已发布；本轮仍在原有开发分支和外置工作树中验收，没有把未验候选发布到 main。

## 已完成整合

- 保留服务端后台续扫、隐藏页面有界同步、容量受限状态和显式引用预算修复，同时保留当前标签的只读预览、返回星图以及原文行号和完整文件指纹。
- 合并元数据回归的两组用例，额外覆盖已有命中且请求在途时新增节点仍不取消检索，以及仅来源列表移除、节点不变时立即清空旧片段、游标和选择。
- 星图脚本缓存版本升为66，预览脚本为2。预构建索引保留当前已公开的3c6f镜像，避免指向尚未发布的旧预览候选；旧59ebe镜像只作为合并前历史证据，不能证明整合代码。

## 实际检查与待办

`starmap-live-metadata.test.cjs`、`starmap-file-preview.test.cjs`、`starmap-background.test.cjs`、`starmap-document-continuation.test.cjs`、`test_i18n.cjs` 和星图语法检查通过。文档检查通过272份 Markdown、886条本地链接。合并后完整回归已启动，原始日志保存在本工作树 `.agent-state/starmap-native-preview-integrated-full.log`；未完成前不标为PASS。

后续必须等待当前完整回归的真实结果，再构建整合候选、从指定 start.command 启动并在 Safari 完整工作台验证预览、精确行号、指纹变化拒绝以及移除来源清理。最终发布门禁、两种架构完整镜像包与公开下载校验仍需完成。生产9999保持08e257修复版本，没有新增浏览器标签，没有改动用户凭据、配置或来源。

## 整合候选构建

源码 `199a24cc8aba1491efa0e8caa927c0672f943541f7500076c6683f6241c3270c` 的 ARM64 镜像 `950bcbc2d79e` 已在禁网构建中完成 vet/build；普通用户 uid1000 的23项分发资源读取和 DOCX 往返冒烟通过，见[候选构建收据](evidence/starmap-preview-integrated-build-proof.json)。生产仍为08e257，未改用户配置、凭据或挂载。Safari9999原入口只读检查可用，无新增标签；这不是整合候选的页面验收。完整Go回归仍运行，候选尚未导出或发布。

同一源码的 AMD64 镜像 `79765161aef2` 也已禁网构建，vet/build通过；Docker ARM64 主机上的 AMD64 模拟执行完成 uid1000、23项资源读取及 DOCX 往返。该证据不是 Windows 原生验收。原生验收的专用两份只读 Markdown 样例与保护状态检查脚本已备妥，尚未注册来源或替换生产。

## 完整离线包与新目录启动

两份整合包已通过内外 SHA-256、精确启动器和 CAD 许可证核对、隐私数据排除及实际 Docker 导入，见[包收据](evidence/starmap-preview-integrated-packages-proof.json)。新数据目录使用包内原始 start.command 无构建/拉取启动，通过编译版本的 CRLF/空行第三行定位与原始文件完整哈希、120三角形原生 CAD，见[启动收据](evidence/starmap-preview-integrated-fresh-start-proof.json)。临时实例已停止并恢复原 slot 标签，生产仍为08e257。测试使用现有 Docker 引擎与单独导入，不能称为空白引擎或接收设备验收；完整 Safari 与公开发布仍待。

整合后的完整回归106项全部通过，服务端 race899.155秒及vet通过，指纹 `e3e77c8fabcd71736324498185d9413f7da3b8c489e77fae354dbaccb2dd7c31`，见[整合回归收据](evidence/starmap-preview-integrated-full-proof.json)。这是当前实现与现有发布索引组合的证据；实际Safari与最终交付索引冻结后的门禁分开记录，不能提前宣称发布完成。

## 2026-10-17 实际Safari与后续修复

原启动入口先因主目录已安装、被Git忽略的插件 node_modules 进入源码摘要而重建；与候选工作树对比，受控源码无差异。随后默认预构建路径准确启用完整包950b，无编译/拉取，并核对实际资源字节、原鉴权/设置/14会话和挂载。详见[原生分范围收据](evidence/starmap-preview-native-partial-proof.json)。

Safari 原有星图页查询专用只读来源，命中 CRLF 文件第三行；点击预览在同一标签直接打开只读页并实际选中第三行，返回恢复同 channel、关键词、来源与逐字检索模式。移除专用来源后旧命中自动清空并提示重新探索。来源注册表逐字节恢复，原受保护文件与无关挂载保持，查询/筛选/默认模式复原后回原工作台，无新增标签和模型调用。

同时现场发现，原文命中替换 selected 时丢失计算后的 stellarBand 等字段，详情出现 undefined。已添加失败前置回归，改为保留选中节点的计算字段并以命中文件覆盖来源/定位，明确清空缺失origin；修复后预览、元数据、后台和星级尺度回归通过，脚本缓存升为67。该改动发生在106项回归及950b验收之后，因此前述full与镜像均不证明新修复已交付；新候选、原生复验、过期哈希拒绝与最终门禁/公开发布继续待办。

## 最终v67候选与实际复验

代码 `26256161`、受控源码 `9286121f6a299e302655ed8e270df3ef0d26cef318718f6e7264ef3d3bbad035` 的 ARM64 d025448dd174／AMD64 301d9d75ffbc 完整包已通过内外哈希、启动器/许可证和实际导入，见[最终包收据](evidence/starmap-preview-final-packages-proof.json)。ARM64 新目录原始启动器、原文行3/完整哈希和原生CAD120再次通过，见[最终新目录启动](evidence/starmap-preview-final-fresh-start-proof.json)。

本机原始start.command以默认预构建路径启用准确d025镜像，无编译/拉取，实际资源字节匹配。Safari第三行选中、同标签只读预览、返回状态及星级说明均通过；详情显示1个有效关联、1级暖红星和下一级3个关联。仅修改专用样例，在旧行3地址看到“源码已变更，请刷新星图后重新定位。”且头部取消旧L3范围；重新查询得到行5。移除专用来源自动清除旧片段，详见[最终原生收据](evidence/starmap-preview-final-native-proof.json)。

注册表逐字节恢复，原14会话/鉴权/设置和无关挂载保持，查询、全部来源及默认节点检索复原后返回原工作台，没有新增标签或模型调用。经公开附件名称排除和文件哈希核对，只删除本轮两份未发布的950b/7976旧包，释放1,545,037,937字节；已发布回滚及最终包保留，见[清理收据](evidence/starmap-preview-final-cleanup-proof.json)。当前交付输入已冻结，等待最终完整回归与main/Release门禁，不复用前一版full作为最终通过。

## 当前回退说明

本机回退基线为已发布的 ARM64 `08e257e0ad39`（源码3c6f4e12），镜像及更新前保护状态/有界历史备份已确认可读。停止活跃工作并核对无挂起/排队任务后，可在外置 aide 根目录显式选择旧镜像：

```sh
AIDE_IMAGE=sha256:08e257e0ad3929495c991bcddb9ae1ca271dacc286910074b177e19d72d12204 AIDE_OPEN_BROWSER=0 bash scripts/aide.sh start-image
```

该指令只选择旧运行镜像，绕开新预构建索引的候选选择；现有 `.aide-data`、凭据、会话、来源和挂载保留。不可自动用旧备份覆盖更新后的数据。本轮仅核对和编写说明，没有执行回退，也未验证另一设备回退；原启动器候选验收仍使用用户指定 start.command。总账本旧8097说明作为历史保留，当前说明以外置目录9999为准。
