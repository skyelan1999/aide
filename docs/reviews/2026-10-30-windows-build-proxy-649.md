# Windows 构建代理修复验收（#649）

问题：[issue #649](https://github.com/skyelan1999/aide/issues/649)。实现：[PR #650](https://github.com/skyelan1999/aide/pull/650)。基线为 main `7a1e4b6766fe5f32cdaf5c14204738679218d9fc`。

## 实现与边界

`build/clean` 的 CMD 入口在首个 EXE 编译前导入当前用户手动代理，设置仅留在入口的 `SETLOCAL` 进程范围内。首次 Docker bootstrap、临时 EXE 的 host-stop 和后续原生构建继承该环境。直接调用原生 build/clean 时通过 WinHTTP 读取当前用户代理，使用同一组策略。

已有 HTTP_PROXY、HTTPS_PROXY 或 ALL_PROXY 时保留整组显式代理；NO_PROXY 优先。支持单一地址、分协议地址、方括号 IPv6 和地址两端空格。PAC-only、SOCKS-only、凭据等复杂手动格式给出显式环境配置指引。绕过规则只转换可等价表示的部分；`<local>` 映射回环地址并提示短主机名语义差异。诊断不输出代理值。

本修复处理 Windows Buildx 客户端的认证请求。BuildKit 容器和 Dockerfile RUN 的联网仍使用已有 Docker Desktop 容器代理/构建配置；不向容器注入宿主回环地址，不修改系统网络设置或现有 builder 环境、归属和缓存。运行入口、Release 模式、用户数据及无 PowerShell/宿主 Python 的运行基线保持原有协议。完整行为见[启动器规范](../architecture/launcher-contract.md#windows-源码构建代理649)。

## 已执行验证

| 验证 | 结果与证据 |
| --- | --- |
| 生产 C++ 可移植代码 | `python3 scripts/test_windows_native_portable.py`：66 项通过，其中代理策略 28 项；涵盖直连、禁用残留、单一/分协议/IPv6/空格、绕过规则、显式环境优先、PAC、无效格式和敏感值隐藏。 |
| 真实 Windows SDK / CMD / 控制台 | [Windows CI job](https://github.com/skyelan1999/aide/actions/runs/41038456698/job/124005018716) 通过：生产 EXE 编译/链接/self-test，28 项实际 CMD 策略用例，实际根 BAT 两段环境传递，PAC 拒绝，模拟网络失败提示，旧 EXE/用户数据/退出码保留，以及独立控制台等待确认。 |
| 原生宿主核心 | `python3 scripts/test_windows_native_host.py`：35 项通过，包括事务恢复、停止确认、失败日志脱敏。 |
| 启动器及包组装 | `python3 scripts/test_launcher_contract.py`：20 项通过，包含实际 ZIP 组装和源码/Release 边界。 |
| 快速 CI | [quick job](https://github.com/skyelan1999/aide/actions/runs/41038456698/job/124005019102) 通过，内容指纹 `1f1524ea03ebd114a2790a49f5cada3041f850bfb26e08698db27867c4bc3eb9`。 |
| 原生 AMD64 完整交叉构建 | 10,024,960 字节；SHA256 `6c87c00b13b2599306fd42ed712b8cedf50f67f7cc2d48ecb6b9de2ac3f3d1d1`。完整嵌入当前 Go 宿主组件及许可证；PE 架构和系统 DLL 依赖检查通过。 |
| 原生 ARM64 完整交叉构建 | 9,226,752 字节；SHA256 `c2035a539d38913251b46ef0eca613b82910b194283a5976ea28ab272a0874c2`。同样通过组件、许可证、PE 架构及系统 DLL 检查。 |

上述 Windows job 对应产品提交 `53c17bec5c9c4ed51b6d458bf128d3db5911f63a`；之后仅补充本验收记录/任务账本。最终 PR 的全部 required checks 和正常审阅状态以 PR 页面为准，文档记录不替代远端门禁。

CMD 测试使用原生 reg.exe 查询夹具提供合成系统设置，并以原生 Docker/启动器夹具检查实际 BAT 进程传递；不改 Windows 当前用户的真实代理配置。它验证脚本/策略/进程边界，不能冒充真实 Docker Desktop 完整构建验收。直接原生读取 WinHTTP 的代码已由 Windows SDK 和完整双架构构建覆盖，实际受影响用户配置仍待现场验收。

## 仍需验收与合并状态

- 受影响 Windows Docker Desktop：完整 `build.bat` 为 **NOT_RUN**。需在该电脑验证仅系统代理时完成 bootstrap、运行镜像/原生宿主构建及健康启动；不能以最小元数据构建通过代替。
- PAC/WPAD 和需要认证的代理按提示显式设置 HTTP_PROXY/HTTPS_PROXY；若失败发生在 Dockerfile RUN，按独立容器代理作用域排查。
- Issue #649 保持打开，直到受影响环境完成验收。PR #650 的完整 Go/gate 和 main 必需审阅分别受远端门禁管理；本次修复未授权管理员例外、tag、发行或生产部署。

原始日志与本地 PE 保留在忽略的 `.agent-state/issue-649-*`。构建容器均以 `--rm` 结束；未操作生产服务或原始脏 checkout。
