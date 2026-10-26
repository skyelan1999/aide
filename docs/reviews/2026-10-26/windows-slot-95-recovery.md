# Windows RC12 slot recovery at 95% / 槽切换回滚停在 95%

Issue: https://github.com/skyelan1999/aide/issues/369

## Confirmed / 已确认

The RC12 agent exits without a terminal receipt when both target activation and the recovery launcher fail. A regression run against the released script reproduces this uncaught exception. The UI retains its persisted rollback/95% operation. The hidden agent did not retain durable diagnostics, so the reporting host's first activation failure is still unknown.

RC12 在目标启动失败、回滚启动也失败时，会在发送最终结果前退出；已用原脚本复现。页面因此保留回滚中/95% 的状态。原代理没有持久日志，报告设备第一次启动失败的原因仍未确认。

## Repair / 修复

- Pin the actual launcher process handle, read its exit code before disposal, and wait only for that process. Capture bounded, redacted launcher diagnostics in `.aide-data/update-agent.log` (rotating at 2 MiB).
- Guard recovery independently and preserve both activation and recovery errors. Save terminal results in `.aide-data/update-agent-result.json` before HTTP delivery; retry after temporary API failure or agent restart. A superseded-operation response discards the stale receipt without changing the newer operation.
- An identical image ID, verified running image and successful health check permit logical slot activation without restarting a healthy service. Version labels alone never authorize this shortcut.
- Progress reporting and post-success canonical metadata sync must not cause a healthy activation to roll back.

读取真实子进程句柄和退出码；回滚异常单独收敛；记录脱敏日志和可重试的最终结果。只有镜像 ID、运行容器身份和健康检查全部一致，才省略同镜像切换时的重启，不按版本名猜测。

## Verification / 验证边界

Local PowerShell 7: 16 activation/recovery fixtures, restart receipt retry, same-image activation, transient progress outage, and real child-process exit 0/7 with a background descendant, paths containing spaces, redaction and temporary-output cleanup passed. The released RC12 script fails the double-launcher-failure regression. Repository quick checks are required; Windows PowerShell 5.1/7 CI results are recorded on the PR.

Docker/HTTP adapters do not establish native Windows Docker Desktop acceptance. The affected Windows computer still needs the repair and an actual retry. No changes to private implementation, live 9999, Docker settings or user data are part of this repair.

## RC12 host-only repair / RC12 宿主脚本补丁

1. Save work, stop Aide normally and back up the existing `scripts/update-agent.ps1`.
2. Replace only that file with the repair. Keep `.env`, `.aide-image` and `.aide-data` intact. Restart Windows once to stop the previous hidden agent, then run the installation's `start.bat`.
3. Retry activation once. If it still fails, provide `.aide-data/update-agent.log`; do not delete slot state or user data to clear the progress bar.

先保存工作、正常停止 Aide 并备份原 `scripts/update-agent.ps1`；只替换该脚本，保留配置和数据。重启 Windows 一次后从原目录运行 `start.bat`，再尝试切换一次。失败时使用新日志排查，不删除槽状态和数据来消除进度。

The host-only patch needs no image rebuild. It does not replace or republish the RC12 release archives.
