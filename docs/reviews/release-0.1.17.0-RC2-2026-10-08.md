# Aide 0.1.17.0 RC2 发布记录

状态：本地完整门禁、镜像和实际发行包启动、八项附件验收通过；GitHub prerelease待远端核对。用户授权：“所有的内容都推上去，然后release”。

## 本次内容

- 辅助审批：本地新文件提案由独立模型审核后复用原应用流程；已有文件覆盖、SSH、只读、取消和变更继续转人工。按会话保存审批选择，重试继承，等待文件应用时可调整模式；审核获得实际工作目录与60秒时限。
- 星际日志：索引范围、来源、预算与证据迁至右上角图标；Escape回焦点、点外部关闭，内容有界滚动。
- 星色与晕染：以唯一非结构关联数划分0／1–2／3–7／8–15／16–31／32+六档。孤立灰点，高连接蓝色强光，调用层距离仍独立调节星等；差量更新保留选择并更新星色。
- 文档、截图和任务账本一并入库；之前的SSH、多来源等RC1能力保留。

## 实际证据与边界

[辅助审批真实模型记录](2026-10-08-assisted-approval/README.md)：deepseek-flash 新文件审核通过并实际落盘；删除命令转人工且哨兵保留。临时模型配置、vault和访问令牌已逐字节恢复，重启后没有测试模型驻留。

[星际日志](../tasks/starmap-logbook-20261008.json)和[星色验收](../tasks/starmap-stellar-colors-20261008.json)：实际Safari隔离18201桌面日志打开／Escape／外部关闭，灰点及40／20／10度颜色、135→138关系自动差量通过。测试并非生产数据或所有颜色边界。本轮恢复Safari控制后：刷新页面回到新任务，重新打开原会话，显示辅助审批及已应用文件修改；日志打开、Escape关闭回焦点、高关联40度蓝色节点再次通过。截图在2026-10-08-rc2-release目录。该检查验证最近任务回退，不声称用户显式模式覆盖的浏览器流程全部覆盖；窄屏、所有主题、量化帧率未完成。未绕过IAB证书错误。

完整回归、固定版本镜像、隔离发行包启动、附件与远端核对的实际结果将在产生收据后追加。本次不替换9999，不部署Pages；三平台ARM64启动器不表示三个真实宿主设备都已测试。

## 回滚

保留 v0.1.17.0-RC1 及原数据卷。候选只使用隔离18201与原QA挂载；回退切换旧镜像，不删除数据卷或用户工作区。

## 补充浏览器与阈值检查

- Safari18201最终审批候选：刷新后重新打开会话#2，辅助审批与已应用状态保持；新任务默认手动。没有切换审批权限或再次发送模型任务。
- 实际星图64节点138关系：星际日志打开，Escape关闭并回焦点；blue-hub为40有效关联、蓝色强光星。
- Node VM读取当前实际星色函数：0/1/2/3/7/8/15/16/31/32/100000阈值、半径/光晕有界、结构/自身/重复排除、符号跨文件投影均通过。
- 截图：[审批刷新](2026-10-08-rc2-release/approval-after-reload.png)、[日志](2026-10-08-rc2-release/logbook.png)、[星色](2026-10-08-rc2-release/star-degree.png)。

## 升版前完整门禁

19项全部exit0；server race335.010秒、tts3.293秒及vet通过。收据 `.agent-state/verify-20261008T080224159593Z.log`，指纹 `b72ce78b4d6d24832292a32641b8c5e41b495167c1a453b1ee85a51201468ad8`。范围与截图证据不扩大到未验收设备。

## 固定版本构建

- 标签 `v0.1.17.0-RC2`，版本提交 `a4a13aa41c98775bc2c63a1b2f4a8413ed9a2ad3`。
- `scripts/docker-release.sh`，AIDE_RUN_TESTS=1，server128.886秒、tts1.913秒、vet通过，固定信任证书的HTTPS healthz通过。
- 镜像 `sha256:820efc829a7833fdbb1ad712084628b7895962bf8ee1a4cac771fa50cb2b0a8b`，平台 `linux/arm64`，源码标签 `c07e58de7aae716165a97573014b661370b05880768c3b3dbf35f3d72755f105`。
- 原始构建日志 `.agent-state/release-rc2-20261008/build.log`；发行导出更新了docker-images/SHA256SUMS，需对最终输入再次运行完整门禁。

## 实际发行包启动及本地附件

完整ZIP保留未改的解压原件，工作副本仅设置QA18201、原QA工作区/引用目录/数据卷与独立镜像引用，未挂载候选二进制。用户要求的start.command成功导入包内镜像并启动；HTTPS固定证书、未鉴权401、鉴权200；version与buildCommit均匹配RC2标签，容器镜像ID等于包内manifest。模型为空、hasKey=false，未再复制模型凭据。

Safari实际发行包刷新：工作台显示0.1.17.0 RC2，重新打开原会话显示辅助审批和已应用；星图64节点138关系、星际日志可打开。截图：[发行工作台](2026-10-08-rc2-release/final-package.png)、[发行星图](2026-10-08-rc2-release/final-package-starmap.png)。临时浏览器标签已关闭。

### 完整性边界

复用QA卷保留RC1基线commit8a2ddb21，binarySha256 d61deb9315aa7dbf4b19db2b6dba451a9c5448e33dba64956b0b1d87029cc3c7；RC2实际二进制05d1b440a018c3feecc7aed0c08ff75b5da106435a599fe823040fb2b794977d，因此healthz.status=ok、integrity=degraded。未重置或删除该安全基线。独立空数据一次性容器的RC2 healthz.status=ok且integrity=ok、401/200及版本提交核对通过，测试容器已停止移除。旧卷换版需在部署流程核对新镜像并处理基线；新装健康不代替原卷升级完整性验收。

原始收据：.agent-state/release-rc2-20261008/runtime-receipt.json、fresh-runtime-receipt.json、start-package.log。隔离服务继续保留18201，生产9999本轮未停止或替换。

八项本地附件通过ZIP CRC、七载荷SHA256、包内manifest/启动器镜像ID与tag、无.env/access-token/vault文件名检查。ARM64专用，不声称Windows/Ubuntu真实设备启动或amd64发行。

| 附件 | 字节 | SHA256 |
| --- | ---: | --- |
| RELEASE-ASSETS.txt | 522 | `c4052b7e8533f8aaded9d4ded4a5b0b0bcf5678b5a8576d918f15afbd407cb0b` |
| SHA256SUMS | 725 | `0f6c2384dc4063cdc94fee418139d4242ee0f2026061a8a7b528b55f8b19400a` |
| aide-v0.1.17.0-RC2-full-linux-arm64.zip | 552850035 | `aff1675573440272aecb3cc27f104e70efac92a1c52778901de8d026443bbed6` |
| aide-v0.1.17.0-RC2-linux-arm64-image.tar.gz | 552726810 | `f0f97a61b6b95695b64deca72a88a6f3598594ea5409822f9d2e43c34df29a21` |
| aide-v0.1.17.0-RC2-macos-arm64.zip | 39780 | `f133f93802ac2863211aeee5125eff10a94d683c5aabe23e7621321e0b4167c1` |
| aide-v0.1.17.0-RC2-ubuntu-arm64.zip | 40112 | `97658f73fb4be6ce048183ca87ed2f0d51d3c2000518b4929c142dd384826905` |
| aide-v0.1.17.0-RC2-update-linux-arm64.zip | 552795326 | `bfbed95631af84000f326b79c09e4bdf55022b20646d83b66766a3622509fc94` |
| aide-v0.1.17.0-RC2-windows-arm64.zip | 49648 | `812cddd959fec02bce9a161b82ebaf2153647a4729016e4e47151fa733407ac2` |

## 最终完整门禁

包含导出后校验和的最终19项全部exit0；ok  	aide/internal/server	326.316s, ok  	aide/internal/server/tts	3.759s；vet通过。收据 `.agent-state/verify-20261008T081756081248Z.log`，指纹 `e3c3668e54b229e886a95958a44dce87a5aad0d7632e6ea6bec73fece6b5c32c`。后续只追加docs下发布回执，不变更标签或镜像。
