# PjPlan 独立核心候选验证 · 2026-10-18

本轮完成技术实现与本地提交，不是正式商业发行。生产 Aide 未替换；原有 plugins/registry.json 修改保留。私有核心在同级独立本地仓库，无远端，不把源码或密钥复制到公开项目。

## 实现指纹

- 私有核心：2160766e80beeeae2f60600f88edb23fa893f21a。
- 公开连接层：PjPlan 526b81dcbe9dea5e6e24ab4497ab048177dbf46f。
- 市场：5cc0884c2fc01e2430ae64a1d23886b94abbcb9e；Aide 内置快照与此本地提交一致，远端尚无该提交。
- 加密构建使用既有 PjPlan 3.1.0 引擎 index.js/classes.json/manifest.json，来源 48b75bea4ac4321e2f3fcf64e592a2afe57d6d28；本轮不改变这三份公开内容。
- 连接层候选 ZIP：58184630a9f8e8caf68dd2537998a972d51235031391a064750620700fd0b7c5，仅含 manifest.json、index.js、README.md。无分类表和规划实现；不是可独立运行的商业产品。

## 已执行

1. 独立核心：离线一次性 aide:local 容器，Go 1.26，`PJPLAN_SOURCE_ROOT=/pjplan AIDE_SOURCE_ROOT=/aide go test -mod=vendor -race -count=1 -timeout=5m -v ./...` 和 `go vet -mod=vendor ./...` 成功退出。密钥/设备存储为合成测试夹具，不调用系统凭据库。
2. 核心覆盖：签名/签发者/版本/密文/时效篡改拒绝、另一设备拒绝、设备密钥缺失拒绝、逐调用检查、未知工具与请求大小/深度限制、HTTP 令牌/Origin/方法拒绝、私密输出禁止写 Git 和覆盖。
3. 既有 49 项 PjPlan 回归经授权核心 HTTP 执行通过。新增真实 Aide plugin_host.js → 连接层 → 核心测试，分类 10262 正常；移除模拟设备密钥后，即使 activated=true 也拒绝，不回退明文算法。首次测试夹具无同步替换存储触发 race，修正为同一互斥锁后全套重新通过。
4. `node --test test.js scripts/license.test.cjs bridge/bridge.test.cjs`：53 通过、0 跳过、0 失败。连接层回环请求和无核心拒绝、独立 ZIP 内容验证通过。
5. 私有 `scripts/build-private.py`：使用容器内临时测试发行者，离线构建 darwin/windows/linux × amd64/arm64 六目标成功。仅测试容器配置受信挂载 /pjplan 的 safe.directory。测试密钥和本机程序随一次性容器删除，没有生成正式发行密钥或保留可误发测试程序。
6. 市场 `verify_catalog.py` 和 `test_private_catalog.py` 通过：23 免费包保持不变；私有条目仍 coming_soon、公钥为空、无价格或购买入口。Aide 快照逐项对比市场目录与提交一致。范围内 diff --check 通过。

原始忽略日志：私有项目 .agent-state/core-tests-final.log、cross-build.log；公开 Node 测试日志为本次临时输出。受控结论在本文保留。

## 未执行或未实现

真实 Keychain/Credential Manager/Secret Service、Docker Desktop 宿主回环转发与只读挂载、三平台实机激活和客户安装、操作系统签名/公证均 NOT_RUN。未做 UI 改动或浏览器验收；未将自动测试替代这些实机结果。Aide 本轮仅目录与文档变更，检查其快照一致性；未重复全量 Aide Go 测试。

服务端专业功能、订单/付款回调、席位、退款吊销、短效下载与签名更新均未实现。系统凭据库不能防同用户代码或管理员提取，离线授权不能防时钟回拨或保证实时吊销。已公开的算法不能重新变成秘密。未推送、未发布、未重启生产。

## 恢复与下一步

需要撤销候选时回退范围内提交，保留用户注册表与数据。私有核心尚未启动，没有需要停止的新持久服务。正式发行前先完成真实平台与 Docker 连通验收，再由发行者提供真实密钥、私有交付路径和操作系统签名；不能将本次合成测试密钥投入使用。
