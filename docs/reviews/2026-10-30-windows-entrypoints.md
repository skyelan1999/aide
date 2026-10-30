# Windows Git BAT 入口修复验收（#646）

用户确认故障发生在 Git 源码目录。基线 `820c18d8`，实现 PR [#647](https://github.com/skyelan1999/aide/pull/647)，产品提交 `0509137c`。修复不代表已替换用户 Windows 机器上的文件。

## 根因与修复

1. 根 BAT 立即退出，EXE 的错误等待只覆盖单进程控制台，遗漏 `cmd + EXE`。现在统一显示操作、退出码，`cmd /c` 保留窗口等待确认，自动化可设置 `AIDE_LAUNCHER_NO_PAUSE=1`。
2. 已有旧 EXE 时 build 不 bootstrap，导致新命令被交给旧实现。显式 build/clean 现在编译当前原生 host；旧编译收据不能反向委派 build/clean。停止本安装控制器后再替换启动器；start 不编译。
3. Windows 实际执行暴露了原 bootstrap 的 64 字符类 FINDSTR 正则超长，导致 build 失败。改用长度有界的 CID 字符与长度校验，拒绝短串、非十六进制和追加命令，失败显示具体阶段。编译临时 EXE 放在本次拥有的 `.agent-state` 路径。
4. 根目录和活动 scripts 中的 12 个旧 PS1 原字节迁入 `scripts/fixtures/legacy-windows/`；保留历史兼容回归，退出产品打包、Docker 上下文和源码构建身份。

## 已执行的验证

- `python3 scripts/agent-route.py verify quick`：313 项 PASS，指纹 `401596127d4d2bdaf73fb6e93b1f01d4ba29d29ac21b1bc2448fd504efde9db2`。此前代码/文件集合变化期间的收据标为 fail，未用于本结论。
- 20 项启动器合同及实际 ZIP 组装、40 项数据迁移、38 项可移植原生检查通过；包内包含统一 CMD 入口，不含旧 PS1。首次迁移测试的临时 socket 被沙箱拒绝，允许后完整重跑通过。
- 完整内嵌资源 EXE AMD64 / ARM64 离线交叉编译通过，PE 许可证和系统 DLL 导入检查通过。交叉编译不等于目标 Windows 执行。
- Windows SDK 编译生产 C++ 代码、自检及实际根 BAT 调用通过，测试源码包与 Release 包支持边界、缺失/旧 EXE、中文和空格路径、bootstrap 成功/失败、旧 EXE/数据保留、退出码 23 原样传播、异常 CID 不执行 cp 或 native build。
- 双击链路使用独立 `CREATE_NEW_CONSOLE`，验证 CMD 在输出结果后仍存活，并仅在向其专属 `CONIN$` 注入确认键后退出。没有把 headless PIPE 的 PAUSE 结果冒充真实控制台等待。
- Windows CI 的原生进程/锁、文件中继、MIXSoul、PS 5.1 和 PS 7 历史 fixture 回归全部通过。不可将历史 PS 测试误认为产品仍依赖 PowerShell。

上述初次通过的 [Windows job](https://github.com/skyelan1999/aide/actions/runs/41027014024/job/123967676884) 对应产品提交 `0509137c`。原始失败、最终 PASS 和本地编译日志保留在忽略的 `.agent-state/windows-action-entrypoints/`。当前 PR 全量 Go/总 gate 仍需以实时 CI 状态为准。

补充核对后，`275c458f` 修正了跨 EXE 的控制器退出确认：先校验安装目录和 PID 创建时间，再等待原进程句柄退出，不再把“程序路径不同”误当作“进程已停止”，也不终止不熟悉的进程。新增真实旧版本进程延迟响应停止标记的 Windows 用例，确认 host-stop 返回时原进程确实退出。[补充 Windows job](https://github.com/skyelan1999/aide/actions/runs/41028992918/job/123974453263) 全部通过；AMD64/ARM64 完整 EXE 重新编译通过。后续变更仅更新验收与任务记录。

## 边界与操作

没有触碰生产 9999、原 checkout 的用户改动、真实 Windows Docker Desktop 数据或已有私有插件。CI 的 Docker 边界为原生 fixture；未声称用户受影响机器已经完成 build、stop 或 docker_clean。Issue 保留至受影响主机复验。

合并并更新 Git 源码后先运行 `build.bat`，它会刷新旧启动器，再构建启动运行镜像；不要只更新 BAT 后继续使用旧 EXE，也不需要先手动删除 EXE。其他命令失败时查看保留的控制台输出及 `.aide-data/native/launcher.log`。未升版、发布、替换生产镜像或修改分支保护。
