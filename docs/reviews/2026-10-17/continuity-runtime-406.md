# 406源码双架构完整离线包与公开上传记录

本轮产品提交 `75278e2fcb43ac116176e3510949112bfc0350b2`、源码 `406b1984556fdfd416b3d1e5755aeac18bf203e2c9dea8e9ae38b85141c403ae`，已包含成果总览外部活动刷新修复。两个完整包均已在本机制作、实际导入并分范围验收，公开上传失败，默认清单和生产未替换。见[结构化证据](continuity-runtime-406-proof.json)。源码已在main，本报告的文档推送不能代替镜像公开发布。

## 包身份与内容

| 平台 | 本地完整包（活动仓库docker-images目录） | 字节数 | 外层SHA-256 |
| --- | --- | ---: | --- |
| ARM64 | `aide-0.1.17.0-RC2-linux-arm64-5dc1e42624fc-offline.tar.gz` | 919243067 | `df0e8395399ca29e678262df1602ebf312eeb20ebbb0f5018bdedb3fd1e3a1f5` |
| AMD64 | `aide-0.1.17.0-RC2-linux-amd64-14cb43f2c1ed-offline.tar.gz` | 923326306 | `1cd3ef4b84471292d0151f941cc7e7dcb05f093acf98fd729dae0960aa29533c` |

ARM镜像 `sha256:5dc1e42624fc9812362970e7163a0a89d7cf0eac6750a60fceafa581ac73c720`，AMD镜像 `sha256:14cb43f2c1ede5d9e182e9e82b9702f3f8b81c5d2e151cc5866a7581203ed562`，共同sourceSHA和buildCommit逐项一致。每个外层包含三平台启动／停止／缓存清理脚本、Compose、许可证、BUILD.txt、镜像标记和一个合并的Docker镜像tar及其校验和；ARM包还含同平台本地依赖镜像标签，公共层由Docker save合并。AMD应用镜像本身已含工具链和CAD，不能因没有额外ARM依赖标签而误判缺少AMD依赖。

按已有export-bundle导出，没有另造镜像归档格式。逐项检查路径安全、无宿主.env／.data／.aide-data，包内启动脚本与受控源码逐字节相同，BUILD身份正确。独立重算内部镜像tar摘要，流式送入实际docker image load，返回0；导入后的镜像ID、平台及source标签一致。外层SHA亦独立计算。完整包留在本机，未把近1.84GB二进制加入Git。

## ARM复用、包内导入与推理强度

复用已有精确5dc ARM构建与完整Safari验收，避免重建相同二进制；既有证据为[总览修复](outcome-external-events-proof.json)。本轮重新实际导入完整包后，只移除启动器使用的offline别名，保留同镜像的构建标签；原始start.command因此走“校验并导入包内镜像”，不是把整个Docker引擎或镜像所有层清空。不冒称新设备或全冷引擎测试。

从完整包解压到独立数据区，通过包内原始启动器启动，真实宿主TLS就绪检查通过。健康、鉴权配置、未授权401、CAD原生OCP导入、管理权限默认关闭和插件工坊自动安装均通过；确认实际镜像为5dc，所有bind仅属于本轮目录，未挂生产数据。

再次实际写入两会话的推理强度：A依次auto／off／low／medium／high，B依次high／medium／low／off／auto，总计10次并发跨会话写入；API与规范磁盘逐项一致。旧版本409未覆盖新值，全局模型和强度保持。原始stop/start后同容器、同镜像、新进程，最终high／auto及revision5／5保留。没有真实模型请求、源码编译或远程镜像下载。本轮包启动不再重复浏览器，当前同镜像的完整Safari证据单独引用。

## AMD离线构建与检查范围

ARM验证并清理后才构建AMD。复用现有本地AMD工具链／CAD基础镜像，pull=false、network=none；未与全量race或另一架构构建并行，沿用现有Docker全局约5.9GiB／4CPU预算，不声称逐构建有额外cgroup限额。

在ARM CPU仿真下，以镜像正常非root用户读取25个应用资源文件，五个随包插件资源存在，实际DOCX创建并回读成功。安装到/opt/aide/cad的真实转换器通过装配放置、面／零件颜色、全部面网格、带修剪NURBS、混合线点、退化边界、毫米／米／英寸和法向／边界等原几何回归，network=none，1GiB、2CPU限制。

额外启动AMD编译应用本身：只读根、1GiB、2CPU、无网络、仅临时目录可写；最小启动时禁用内置插件宿主，插件资源由前项另验。TLS健康、鉴权配置中的752提交、未授权401，以及实际HTTP返回app.js／task-outcome.js摘要均与当前源码相同。该证据不是Windows原生或完整插件工作流验收。

AMD完整包导入后，通过它自己的start.command在ARM引擎上启动，准确拒绝linux/amd64与linux/arm64不匹配，没有误报已就绪或创建容器。没有绕过架构保护；Windows按用户要求仍由用户验收。

## 公开上传失败与默认保护

当前两行候选清单由导出器生成，源码相同，但公开附件门禁返回1：没有唯一的当前ARM已上传附件。Safari打开独立RC2编辑页，用原生文件选择器选择已校验5dc包，实际出现“Something went really wrong, and we can’t process that file. Try another file.”。后续重新打开可见新文件名的Upload failed占位项，公开API没有有效的新资产。

为排除外置文件路径及同名失败项影响，在本机临时区复制并校验同一919243067字节包，使用带src406标识的新文件名再次上传；截图仍为同一错误。因此仅改变位置／名称没有解决问题，不据此推定准确网络或服务端原因。未删除旧成功附件或失败占位项，未点击Update release，未改Release正文、tag或默认清单；AMD及其他元数据没有盲目追加上传。

默认下载仍是已经公开核验的07c旧包，不能将其称为包含752修复。接下来需要可用的授权上传通道或上传服务恢复，先上传当前两包和相应摘要，核对公开state／size／digest后再提升默认。默认构建输入改变须新精确full和main门禁。

## 清理、交接与限制

原始stop清理本轮ARM两个已停止容器和两个空网络；AMD各一次性检查容器退出即清理，架构拒绝未生成容器。删除已重算摘要的923243008字节重复ARM内层tar，保留外层包、镜像和测试数据。上传重试的919243067字节内部临时副本也在校验后删除。只关闭自建发布页，保留用户原标签和GitHub页。

最终仅原生产两个容器；生产身份、启动时间和22项保护文件全部相同。当前完整验证指纹仍为 `68a145c0293a65d338460a6b54bc459ee2dba1d7505511f51a3a55a9227be3ff`，复用已有full127成功收据；本轮只新增普通验收文档，没有变更受控构建输入。

其他设备实际导入未运行；Windows用户验收，真实向量暂缓。原12项继续推进。文件可从活动仓库docker-images目录复制给接收端，先核对本表SHA、解压并用包内平台启动器启动；Docker及Compose需预先可用。本机包合格不等于公开下载已经交付。
