# 辅助资料真实协议与 AI 读取验收

> 历史快照，当前以 docs/verification.md 为准。

2026-09-23。执行 `bash scripts/test-source-protocols.sh`，七个子测试全部 PASS（Go race，2.52 秒测试逻辑）。不是只检查配置或使用 curl 替身。

| 来源 | 实际服务/介质 | 验证 |
| --- | --- | --- |
| local / skill | 临时目录、真实文件 | 注册、列目录、嵌套/空格路径、读写回读 |
| link | Python HTTP server | 从 HTTP 响应读取真实文件正文 |
| ftp | vsftpd | 用户名密码登录、根目录与子目录枚举、空格文件名读取 |
| ftps | vsftpd 显式 TLS，ftp URL + ssl-reqd | 同上；使用测试 CA 校验证书和主机名，无 insecure 跳过校验 |
| sftp | OpenSSH server，密码认证 | 真实 SSH/SFTP 登录、目录枚举、读文件、写入后读回 |
| smb | Samba NT1 服务 | curl SMB1 认证、单文件读取；不支持目录枚举或 SMB2/3 |

所有来源还经过 aide `read_file` 工具读取同一文件。测试 Chat Completions 服务主动发出工具调用，断言下一轮模型请求中存在 role=tool 的原始正文 `real protocol reference`。因此验证了“资料内容能送给模型”，而不是靠固定最终回复宣称读到。模型提供商为可控测试服务，非真实付费 LLM；外部供应商网络环境及所有认证组合不在本次验收范围。

非写入类型通过 API 拒绝写入；AI 工具统一禁止来源写操作。MCP 是登记功能，不能列为资源访问 PASS。普通完整检查也通过；真实服务测试单独启用，普通无环境运行会 SKIP，不能把该 SKIP 冒充实测。

## 修复点

- 本地/Skill 添加路径浏览按钮，复用挂载根目录选择器；真实浏览器已检查路径回填。
- 修正本地/Skill/链接类型下拉选项的本地化空白问题。
- FTP/FTPS/SMB 提供账号密码字段；读写选项按能力限制并在服务端验证。
- 网页按单资源读取，保留完整 URL 查询参数，HTTP 错误不再当正常正文。
- FTP 解析常见 Unix 列表，保留嵌套目录前缀并编码空格路径。
- SFTP 修复 batch 参数顺序、嵌套路径、符号链接列表与来源凭据读取锁。
- 新增 list_sources，以及 list_files/read_file 的 source 参数；轨迹显示来源 ID。

## 复现与清理

运行脚本构建 `aide-source-fixtures:test`，依赖签名 Debian 软件包。镜像用清华镜像站获取 Debian 包；保留官方签名验证。服务位于 Docker internal 网络，没有主机端口映射，无生产数据卷。测试用户名密码是固定的 disposable fixture，证书和数据每次重新生成。SMB1 仅在该隔离网络启用，不修改宿主安全设置。

脚本退出时删除测试容器、网络和临时证书目录；保留可复用测试镜像。原始日志在忽略目录 `.agent-state/source-live.log`。18121 预览使用新候选；未发布 Release、未替换生产服务。
