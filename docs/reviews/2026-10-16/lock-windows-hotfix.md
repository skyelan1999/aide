# 锁屏与 Windows 启动紧急修复

用户报告密码正确仍提示“锁屏状态已变化，请重新解锁”，并要求尽快发布。Windows 照片显示 Dockerfile CAD 阶段 `sh /cad-install.sh` 返回 2；用户负责 Windows 实机测试。

## 锁屏

已通过运行实际 lock-cluster.js 的多标签回归复现：从页准备验证后，主页面关闭，从页在密码验证期间重新当选主页面。旧代码以角色不再为 slave 为由拒绝服务端验证结果，尽管锁屏代次未变。

另已复现主页面升锁写入失败后仍广播锁态，从页验证成功后保留遮罩的路径；修复统一以身份验证回执撤下该代次的本地遮罩，旧心跳不能重新遮住，新持久锁定仍会撤销本地凭据。

修复让本地验证凭据绑定服务端代次，跨主从选举保留。主页面的全局锁态与本地遮罩分开判断；本地验证不解除其他页面锁屏，新的手动锁定或服务端代次变化仍撤销凭据。未修改密码、密码校验和服务端权限。

Safari 隔离完整工作台（临时数据、密码 123456）实际验证：错误密码仍锁定，正确密码进入工作台，刷新后保持服务端未锁状态。真实用户账户配置没有修改。截图见 evidence/lock-hotfix-unlocked.png。

## Windows

照片直接证明 CAD 安装脚本失败，未包含安装阶段更早的输出，因此无法仅凭照片断言唯一原因。仓库缺少 LF 属性，Windows core.autocrlf 可将 shell 脚本检出为 CRLF；隔离回归已复现该脚本以 CRLF 运行失败，规范换行后执行完成（安装器命令使用桩，无网络下载）。

增加 `.gitattributes` LF 约束；Dockerfile 和独立 CAD 构建同时在执行前规范脚本及 requirements 换行，兼容旧工作目录，无需重新克隆。保留依赖版本和哈希校验。Windows 实机验收由用户负责，记录为 NOT_RUN；若仍失败需提供 CAD 阶段首个错误，而非仅结尾。

## 发布状态

本次继承 b7c4eb23 的在线插件升级功能，保留旧插件、注册表和数据的恢复机制。最终运行源码 `bfb120b6`，镜像 `sha256:52e943e1e8c3441b2e58d915d288e563e9d84035b075b640c96c3a24b037d661`，源指纹 `d1ee8fc8a334b82e7e7a87450d0301ec0af4440e60ee0672995a1385765b3100`。通过指定 start.command 更新本机9999，保护配置、认证、会话、挂载与45项插件输入摘要保持一致。

最终包 `docker-images/aide-0.1.17.0-RC2-linux-arm64-52e943e1e8c3-offline.tar.gz`，770931075字节，SHA-256 `5eb5671d8117fa3315b602511f6a2a4423fd24e60fab3087578a4c30ada6a48f`。内外哈希、实际 docker load、空数据目录启动（无 build/pull）、CAD原生120三角形样例通过；接收设备导入未运行。ARM64包不作为其他架构包分发。

全量检查首轮发现额外从页遮罩问题后主动停止，修复完成并重启全量门禁；仅最终门禁可作为发布依据。[脱敏收据](evidence/lock-windows-hotfix-receipt.json)记录最终结果。保留原镜像和离线包；需要回滚可用先前 `aide:offline-5c5876dd6049`，保持现有数据挂载，禁止删卷。无版本升档、tag或GitHub Release，交付为main、本机运行与离线包。

Final full verification: 105 checks passed, including Go race/vet, runtime assets and native CAD. Fingerprint `19a268eceda6c20ec9a7e6d02d33350933d9b29f76b88ff32a3d4e84fa92f68b`; verification tree `935e08286d0c6835507eab141501a87bf65af3ce`. Code delivered as `512f4271`. The initial documentation finalizer failed with a stdin encoding error; release-check rejected stale stage metadata, but the shell continued and pushed. The final test receipt was already PASS. Corrected metadata and rerun gates are required before the final receipt push.
