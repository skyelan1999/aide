# Docker MCP：规范镜像与离线包验收

冻结源码：`e58dcb201f3d1ab1b7e6ccf1e067d22a2cb50eb2`。对应产品输入full109已通过；后续提交仅补普通验收文档。原总目标12项继续进行，不据此宣布整体完成。

| 平台 | 完整离线包 | 字节数 | SHA-256 |
| --- | --- | ---: | --- |
| linux/arm64 | [aide-0.1.17.0-RC2-linux-arm64-6a8a2dcb290b-offline.tar.gz](../../../docker-images/aide-0.1.17.0-RC2-linux-arm64-6a8a2dcb290b-offline.tar.gz) | 770960523 | `252f364df84a4b36bffc823e0320631e83d30715dd36a3295198d9bd2ab443aa` |
| linux/amd64 | [aide-0.1.17.0-RC2-linux-amd64-e4742f02031a-offline.tar.gz](../../../docker-images/aide-0.1.17.0-RC2-linux-amd64-e4742f02031a-offline.tar.gz) | 916895313 | `72ee665d1f4679d89b3b63d75249267dfa0dd4c9963c0b198ff7891a58f6e23e` |

两套包均完成外层摘要、内层镜像tar摘要、原始启动脚本、资源保护辅助脚本、CAD许可证及不含私有运行目录的校验，并实际导入。生产容器在导入时保持原样。AMD64仅核对包与镜像身份并导入；未在原生AMD64/Windows硬件启动，Windows由用户验收。

ARM64另从完整包建立全新隔离安装，经原始`start.command`启动，无构建/拉取；鉴权读取唯一标记文件、连接宿主机Docker MCP并发现10工具、读取虚拟工具说明通过。没有模型密钥、模型调用或工具执行。首次脚本错误重交内置来源得到400，修正为真实UI过滤方式后通过；不是产品源码修复。临时实例、网络和目录已清理，原容器和22个保护文件不变。

本机9999已经由原始入口切换到规范ARM镜像`6a8a2dcb290b`，API版本与构建提交正确，原生Safari复验发现10工具、0只读。22保护文件和挂载保持；不把独立协议Time调用或说明预览记成AIDE工具执行。

构建和验证进程均已终态。随后运行原有`clean-docker-cache.command`，Docker报告清理12.98GB构建缓存，当前构建缓存0；具名镜像、Time镜像、原容器和保护文件保留。这是Docker内部计数，不声称宿主机物理空间恰好增加12.98GB。

当前两套包仅本地交付就绪，公开Release附件、默认预构建清单更新及干净main门禁仍待完成。仓库旧默认清单不自动选择新候选；本机本次验收使用显式规范候选。未新建版本或tag。可选MCP服务镜像/profile自动生命周期不属于完整AIDE运行包，详见[接入边界](../../plugins/docker-mcp.md)。

证据：[包校验](docker-mcp-packages-proof.json)、[规范运行](docker-mcp-canonical-runtime-proof.json)、[全新安装](docker-mcp-fresh-proof.json)、[清理](docker-mcp-fresh-cleanup-proof.json)、[构建缓存](docker-mcp-cache-clean-proof.json)、[全量](docker-mcp-full-proof.json)。


## 一文件直接导入镜像

以下是可直接传给`docker load -i`的Docker镜像归档；它们不同于包含启动器的完整离线包。两个架构分别实际单次导入、逐blob摘要和无重复tar条目验证通过；操作后原`aide:local`/CAD标签和运行容器恢复。可选Time服务镜像未包含，用户配置/数据不在其中。

| 平台 | 直接导入文件 | 字节数 | SHA-256 |
| --- | --- | ---: | --- |
| linux/arm64 | [aide-images-0.1.17.0-RC2-linux-arm64-6a8a2dcb290b.tar.gz](../../../docker-images/aide-images-0.1.17.0-RC2-linux-arm64-6a8a2dcb290b.tar.gz) | 770965882 | `e3d58d7a4375e7c1a907b330b3016900be888d88eaf7265a329abd81299fc21b` |
| linux/amd64 | [aide-images-0.1.17.0-RC2-linux-amd64-e4742f02031a.tar.gz](../../../docker-images/aide-images-0.1.17.0-RC2-linux-amd64-e4742f02031a.tar.gz) | 916897033 | `b92d119e351d9b1893bacbd6fb96d5f809165de7ef79618230d16d866df4f6a5` |

公开上传仍未确认成功：RC2公开API保持22个原有附件，新ARM64文件已在选择器中明确选中，页面显示Uploading；未观察到成功回执，不把等待状态视作终态失败。未修改原Release标题、说明、tag或已有附件，也未更新默认清单。进一步网络诊断所需的Safari开发者设置确认仍待用户答复，尚未更改设置。
