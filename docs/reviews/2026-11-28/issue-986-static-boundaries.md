# #986：Compose 选择与音频值域静态复核

日期：2026-11-28（Asia/Shanghai）。操作账号：skyelan1999。

基线：`821ca6902492a94906fc5f58e7004f0122dccd8d`，从当次 fetch 的 origin/main 建立独立分支 `review/986-static-boundaries-20261128`。基线已包含 #1004：`6614f65f399f7475bc16ec3e45e546118e1e0b36`。本次不重复处理依赖和缓存哈希，不涉及 #1010、原生 JPEG 或 Windows 验收分支。

## 结论

本轮指定范围内未确认需要修改生产逻辑的安全缺陷。提交仅补充准确的信任边界/值域注释和正常输入回归；没有新增扫描告警抑制。Compose 路径的本地配置风险不能仅凭文件名校验宣布已消除，也不能仅凭可配置路径判定存在远程命令执行。G.711、PCM、WAV 和帧长度转换均有对应值域或协议语义。

## Compose：配置可信性与命令形状必须区分

- `native/windows-launcher/main.cpp:227`：默认使用已选 Docker 路径加 `compose` 参数；有独立 Compose 路径时直接运行该路径。
- `main.cpp:262,293-295`：先发现 Docker，再探测内置 Compose；失败时按固定 Docker 插件目录和系统查找结果选择独立 Compose，要求普通文件及正常 `version` 返回。`version` 探测是兼容性检查，不是来源认证。
- `main.cpp:476-484`：准备目录服务前覆盖 `AIDE_LOCAL_MOUNTS_COMPOSE` 为 `docker` 或实际 `composeExe` 路径；`-docker` 独立传入 `docker.wstring()`。Go helper 的配置、应用、回滚都经同一 `compose()`，见 `cmd/aide-local-mounts/main.go:45,257,327,350`。
- `main.cpp:127,145`：CreateProcessW 明确指定可执行文件；环境参数为 nullptr，子进程继承 launcher 设置；此处没有切换身份/提权操作。helper 继续通过 Go `exec.CommandContext` 传递独立参数（`main.go:38-54`），没有 shell 求值。
- 所以 G204/G702 中“可执行路径可变”的事实成立；但现有证据只表明它属于宿主进程环境/安装目录/PATH 的本地信任边界，没有发现请求或挂载配置字段可改变该选择的路径。同一边界也涵盖 `-docker`。文件名检查不认证二进制，因此更正了原有容易误解的注释。
- 不将固定绝对路径白名单作为修复：现行 launcher 支持多个安装位置及用户插件目录；改为 PATH 查找会丢失已探测的路径，且 PATH 本身仍属于本地配置。实际 Windows ACL、搜索顺序和提升权限运行时的部署假设未由本轮验证。如需要抵御本地可写工具目录，应另行定义安装信任策略，同时覆盖 Docker、Compose、所有 launcher 工具发现，而非只修改 helper 的一个入口。

## 整数转换复核

| 位置 | 值域/语义 | 判定 |
| --- | --- | --- |
| `internal/voip/g711.go:5-14` | μ-law：输入先升为 int；绝对值含 32768，随后截到 32635，加 132 后最多 32767；segment 为 0..7，拼装值最多 127 | 编码 byte 转换在范围内 |
| `g711.go:17-24` | 解码中间值 132..32256；最终 -32124..32124 | int16 不截断 |
| `g711.go:27-35` | A-law：负数变换 -n-1 得到 0..32767；segment 为 0..7，拼装值最多 127 | 编码 byte 转换在范围内 |
| `g711.go:38-50` | A-law 解码最大幅值 `(240+264)<<6 = 32256` | int16 不截断 |
| `internal/voip/phone.go:153,263` | uint16↔int16 表达同一组 PCM16 二进制补码位模式；负样本写回 uint16 属于预期语义 | 不应加钳位，否则会改变音频 |
| `internal/server/tts/openai.go:207-219,267-282` | 唯一生产调用先限制 PCM 累计不超过 16 MiB，且要求非空偶数字节；WAV 的 data 长度及 RIFF 长度均小于 uint32 上限 | 当前调用范围内安全；函数本身并非任意大小输入 API |
| `internal/server/tts/edge.go:459-475` | 小帧长度 <126；中帧长度 <=65535，两个 byte 明确编码其高低八位；大帧使用 uint64 | 低字节截取为协议要求 |
| `internal/hostbridge/identity_unix.go:21` | 设备号/ inode 转为 uint64 作十六进制目录标识；不是音频窄化，也不是 uint64 转小类型 | 本轮未确认截断缺陷 |

## 实际验证

在已有 `golang:1.26.11` Linux/ARM64 SDK 容器中执行，网络禁用、GOPROXY=off、GOTOOLCHAIN=local、-mod=vendor；源码来自隔离工作区。

1. 基线 `go test -mod=vendor ./cmd/aide-local-mounts ./internal/voip ./internal/server/tts ./internal/hostbridge`：四包 PASS。
2. 修改后 `go test -mod=vendor -race ./cmd/aide-local-mounts ./internal/voip ./internal/server/tts ./internal/hostbridge`：四包 PASS（2.577s / 1.017s / 2.638s / 1.026s）。默认 go test 包含 vet 检查。
3. `GOOS=windows GOARCH=amd64 go test -mod=vendor -c -o /tmp/local-mounts-windows.test.exe ./cmd/aide-local-mounts`：PASS，仅交叉编译，不运行 PE。
4. 新增正常输入：每个 G.711 编码器遍历全部 65536 个 PCM 值，检查解码单调性及量化误差上限；每个解码器遍历全部 256 码字检查再编码（μ-law 双零归一化除外）；检查三组端点码字。
5. Compose 正常选择回归：空值、docker 哨兵、包含空格/中文的独立 Compose 路径；验证传递参数不变。POSIX 夹具不代替 Windows 进程验收。
6. WebSocket 正常长度 0/125/126/255/256/65535/65536，逐字节解码验证长度及掩码后的内容；WAV 正常 PCM 长度 2/48000/16777216，验证 RIFF/data 长度及内容。
7. `python3 scripts/check_docs.py`：PASS，最终 586 Markdown、2585 本地链接。`git diff --check`：PASS。

首轮容器 /tmp 默认 noexec 导致测试程序无法执行，四包 FAIL（permission denied），这是测试环境错误；显式使用可执行临时 tmpfs 后重跑通过，未放宽源码或测试断言。

## 交接与限制

没有构造漏洞复现、利用链或攻击脚本，没有搜索或记录服务标识/凭据。没有修改 Windows launcher 生产逻辑，没有调用外部音频服务。Go 包中的条件性集成测试不等于实际 SIP/话机或在线 TTS 验收。

Windows 实机上的 Compose fallback 路径、CreateProcess 环境继承/ACL/Docker Desktop 行为为 NOT_RUN；完整 quick/full、正式 Delivery 为 NOT_RUN。本轮未启动或重启服务，未推送、发 PR、合并 main、发版或关闭 Issue。#986 是否可关闭由统一审查决定，本结论只覆盖本轮指定静态范围。

回滚只需不集成本分支，或在集成后撤回该提交；没有运行时状态迁移。精确提交 SHA 由交接消息给出。
