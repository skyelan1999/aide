# 2026-10-08 发布联调：SSH 与星图多来源

任务：`release-20261008`。用户明确授权“运行完整验收并发布”。本记录只描述本轮实际执行的协议与 API 验收；完整发布门禁、浏览器回调和发行附件由发布总账另行记录。

## 环境与身份

- 隔离候选由原 `start.command` 启动，端口 18201；测试配置和文件只位于 `.agent-state/release-20261008/candidate/`。没有修改既有用户实例、联系用户 SSH 主机或调用付费模型。
- 首轮候选二进制 SHA-256：`5f06c39b7060330fec0267e664bb149112160de68cac3c12c33cd0b7e7c5df98`。逐项重核原编译输入后，仅取消测试的 `server_test.go` 有变化，该文件不进入运行二进制；产品代码与嵌入资产匹配编译收据。重启发现缺陷后，修复候选 SHA-256 为 `b0ee07874a88ed119071d100b893c3735ff1af4a0538c9568b2a3bb488d59a1d`，已从容器挂载内再次核实。两者均尚无发行版本和提交注入，不能代替升版后的最终镜像验收。
- Go 客户端镜像：`sha256:97abb2c15d948a24e40c2f3f48031fa6417fd77abb1d0925f844ddbd2af1e4d4`；协议服务器镜像：`sha256:338a92fe0e3667734930a480ce9c7a557d4ae129d9160dbcb80ac96a2bde0211`。
- 协议服务器运行在临时 Docker 内部网络，不发布协议端口。账号和资料均为新建夹具。FTPS 证书只加入隔离候选的信任库；候选 API 验证固定本地证书、主机名并使用鉴权，不关闭证书验证。

## SSH 实连回归

命令：`DOCKER_BIN=/Users/skyelan/.docker/bin/docker AIDE_SSH_TEST_EVIDENCE_DIR=.agent-state/release-20261008/integration/ssh bash scripts/test-ssh-workspace.sh`。

结果：**PASS，exit 0**。`TestSSHLiveWorkspaceAndIndependentSource` 开启 race，两个独立服务器，12 个子测试各执行 2 次，共 24 次子测试通过，Go 报告耗时 15.136 秒。测试输入哈希前后相同，脚本自动删除自建容器和网络。

实际覆盖：空目录采用账户 home、中文与空格目录读写、并发首次连接、关闭 ControlMaster 后恢复、加密私钥粘贴与引用、调用方截止时间、缺失与权限错误、独立来源服务器 B、同服务器 A 的相对／绝对文档路径、绝对缓存目录推拉与本地镜像、远端退出状态，以及取消后 shell 和子进程均停止。

原始证据：`.agent-state/release-20261008/integration/ssh/aide-ssh-test-20261008134050-15648.log`，同目录保留输入哈希与日志哈希。

## 星图多来源 API

执行带固定证书信任的 Python 客户端，向上述候选配置本地、Skill、独立 SFTP、HTTP、FTP、FTPS、SMB 和 MCP 目录夹具。首轮脚本传入未填写路径的内置本地来源，API 返回 400；第二轮误用了不存在的 `/api/source/raw`，8 个查看检查返回 404。修正验收输入及实际入口 `/api/file/raw` 后重新执行全部步骤，**50 个检查全部通过**；保留失败尝试，未将它们算为产品通过证据。

| 实际动作 | 结果与范围 |
| --- | --- |
| 来源目录和状态 | 停用来源显示 disabled、不可连接 SFTP 显示 unavailable、空目录 ready、MCP 说明目录 catalog |
| 原文与 RAG | 本地 A/B、Skill、SFTP、HTTP、FTP、FTPS、SMB、MCP 共 9 类夹具原文检索和本地 RAG 命中；每个命中保留指定来源编号 |
| 编号引用 | 9 类夹具按实际文件 ID、定位、偏移、hash 和 origin 重新读取，响应正文与检索片段一致 |
| 原文件查看 | 本地 A/B、Skill、SFTP、HTTP、FTP、FTPS、SMB 经 `/api/file/raw` 读取到相应 marker；MCP 不执行原始工具 |
| 同名隔离 | A/B 使用相同来源显示名和相同 `shared.md` 路径，文件编号、origin 和正文仍分别对应正确来源 |
| 拒绝过期引用 | 错 hash、跨来源 origin、文件正文变化、来源停用和路径改配均拒绝旧引用；停用和改配后的原查看地址也被拒绝 |
| 路径范围 | 来源 `../` 跨目录读取返回拒绝 |
| 自动差量 | 无变化时 reset=false、0 节点／0 删除；新增单文件只返回 1 新节点；删除该文件只返回 1 删除 ID，不重发全图 |
| MCP 边界 | 配置的 `touch` 夹具命令未执行，没有生成标记文件；只检索预存说明 |

原始证据：`.agent-state/release-20261008/integration/api-attempt3.log`、`api-receipt.json`、`summary.json`、初始／最终图谱快照。原始日志不包含 API token；受控文档不记录夹具密码。

另外对工作区、引用目录和内置 `source:context` 分别执行原文／RAG及编号引用，**12/12 PASS**。HTTP 原文夹具扩大为 HTTPS 文本和 HTTP PDF／DOCX／XLSX，以及 SMB XLSX，**35/35 PASS**：实际提取 marker、按定位重新引用、保留原始二进制字节；HTTP／SMB 的虚拟 `.xlsx` 还经只读表格 API 返回正确内容与 `readOnly=true`。HTTPS 和 FTPS 都验证夹具证书，没有使用 `-k`。至此本轮 API 分项共 **97/97 PASS**。

补充原始证据：`.agent-state/release-20261008/integration/baseline-api.log`、`baseline-summary.json`、`formats-api.log`、`formats-summary.json`。

## 重启发现与 SSH 修复回归

停止隔离候选并用原 `start.command` 重启后，首轮重启检查 **13/14 PASS、1 FAIL**：19 个来源配置与凭据状态保持不变，旧引用与查看地址被拒绝，Skill／FTPS／SMB／HTTPS 可读，但 SFTP 无命中。进一步实际读取 `/api/files?source=qa-sftp&path=.` 返回 400、`SFTP 会话未建立: exit status 255`；`vaultUnlocked=true` 且该来源 `hasSecret=true`。直接 `ssh -O check` 报告控制 socket `Connection refused`，旧 socket 仍在；这不是锁定凭据或图谱检索预算造成的失败。

根因：容器重启保留 `/tmp` 中旧 Unix socket，原 `ensureSSHSession`／`ensureSourceSession` 在检查失败后直接建立 ControlMaster，没有清除无监听进程的残留路径；OpenSSH 因旧路径已存在无法创建可用的复用连接。最小修复在已有生命周期锁内执行：检查取消或超时直接返回；有界 1 秒 Unix Dial 只确认 `ECONNREFUSED` 的旧 socket，并再次核对文件身份后清理残留；活跃、被替换、无权限或非 socket 路径不作该清理。共用原关闭连接的文件清理逻辑，没有增加自动写操作重放。

修复后重新运行受控 SSH 脚本，增加工作区与独立来源的真实残留 Unix socket 恢复场景：**14 项各执行 2 次，28/28 PASS，race，15.848 秒**。新加的活跃／超时检查保持 socket 不变回归在工作区和来源两种连接上各重复 2 次，**8/8 PASS，race，6.927 秒**。测试输入前后哈希相同。

同一个 18201 隔离候选再由原 `start.command` 启用修复二进制，实际 SFTP 目录读取恢复 200；来源配置 19 项（2 个内置、17 个夹具）保持一致，节点编号稳定、process origin 更新、旧引用返回 409、旧查看地址失效；Skill／SFTP／FTPS／SMB／HTTPS 的新引用均可读取，MCP 命令仍未执行。此后再做一次真实停止／启动复核：**两轮各 15/15 PASS，共 30/30**。

修复二进制还完整重跑了前述三组 API 验收，**50+12+35=97/97 PASS**，并复核二进制 SHA-256。复核脚本改用新的证据子目录时，首轮最后的制证哈希路径层级错误，未计入成功执行；修正脚本路径后重新运行全部步骤并 exit 0，保留该尝试日志，没有因此改动产品源码。

证据：`.agent-state/release-20261008/integration/restart-after.log` 与 `sftp-files-test.log` 保留修复前失败；`ssh-fixed/aide-ssh-test-20261008140723-17885.log`、`ssh-fix-build-attempt2.log`、`fixed/round1-restart-after.log`、`fixed/restart-after.log`、`fixed/sftp-files-round2.log` 保留修复后回归；`fixed/api.log`、`fixed/baseline-api.log`、`fixed/formats-api.log` 与各 summary／receipt 对应 97 项重跑。第一次新增候选编译使用了与缓存目录不一致的容器用户而报权限错误，随后以与既有脚本一致的隔离容器用户重新执行并成功，未修改共享缓存权限。

修复后 SSH 实连日志 SHA-256：`22adc32a655bf179b8f0fae215bd97ab5cf37f616b7442e36e6fb5e4c9fdb1ce`。忽略目录中的 `fixed/evidence-sha256.json` 记录其余实测日志与收据的内容指纹；日志和本记录均不包含 API token 或真实用户凭据。

全部协议验收完成后，已断开候选与临时内部网络的连接，删除本次协议夹具容器和该网络；双服务器 SSH 脚本亦完成自身清理。只保留候选及其隔离数据，供发布负责人用最终包替换验收。清理 exit 0，收据为 `.agent-state/release-20261008/integration/fixture-cleanup.json`。

## 限制与待接续

- 这里证明新建夹具上的协议连通性、来源身份和 API 行为，不能宣称用户实际服务器的“链接失败”已经消失，也不保证所有 FTP 列表格式、SMB 版本和复杂文档兼容。
- RAG 只验收本地 TF-IDF 检索，没有执行模型回答质量评估、OCR、向量检索或远端 MCP 资源获取。
- 后端差量通过不等于浏览器镜头保持与滚动性能通过；浏览器来源筛选、文件／会话回填和相机动作需结合发布总账的实际浏览器观察。
- 自动系统文档和工作区缓存可在本地或工作区服务器 A；辅助 SFTP 文档可在独立服务器 B。没有增加独立缓存服务器 C 的配置能力。

发布升级后的最终镜像、包、标签、GitHub 附件和生产替换状态均以[发布任务](../tasks/release-20261008.json)为准。
