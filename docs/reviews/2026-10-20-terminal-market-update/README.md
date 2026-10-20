# Terminal 市场1.1.1登记

用户授权“更新一下，然后列一下阻塞原因”。市场public listing更新到1.1.1预发布，ZIP沿用公开Release的104320字节与SHA256 f70d53d819444dd5b16f5a87f99f70e500241e464dfdd1d05976a1feecd319cf。旧1.0.0 ZIP保留，其他24项元数据和包未改变。

条目使用既有features机制要求terminal-workbench-v1，旧宿主拒绝未知能力。PR #7中的配套宿主新增该标记识别，仍要求原最低版本，未知后续能力继续拒绝。市场网页可下载预发布ZIP，但说明要求配套宿主；原生平台仅列已验证的Linux，Windows容器路径及原生限制写入依赖。

## 验收

- 市场public/private各2项回归通过，verify_catalog验证25条目/24公开包/1私有条目。只更新Terminal条目与包；其他条目完全相同。
- 真Chromium：搜索terminal唯一条目、1.1.1、预发布和依赖提示、桌面/390px、真实浏览器下载ZIP并核验SHA256通过，无页面异常。
- Go race：go test -race ./internal/server -run '^TestPluginMarket' -count=1，通过38.078秒，包括带能力标记的实际安装端点、最低版本和未知能力拒绝；见market-go-race.txt。

## 宿主正式发行阻塞（最近一次full）

最近完整收据执行138个检查项，133通过、5个检查项失败，不等于5个Terminal测试失败；具体收据见上轮terminal-ui-release/full-gate.json。

1. test_launcher_data.py：Windows所有权fixture期待auth/cad-auth执行chown，但当前临时目录UID/GID已为1000:1000，产品正确跳过重复chown，与断言不符。需修正fixture模拟不同所有者。
2. test_offline_bundle.py：IPv4/IPv6端口和断点续传两个case的启动器返回3。fixture只模拟open，Linux实际走xdg-open且无可用桌面；还缺message-lan-control.sh辅助fixture。需补齐跨平台启动模拟和fixture脚本。
3. test_runtime_assets.py：无aide:local，Docker尝试拉取该本地镜像名被拒绝，无法执行镜像内资源验证。
4. test_go_snapshot.py：无aide:local，完整容器Go race/vet未执行。需构建/提供镜像或设置已验证AIDE_TEST_IMAGE。
5. test_cad_geometry.py：无本地测试镜像，CAD容器几何验证未执行。需准备该脚本所用aide:local及对应CAD运行依赖/镜像。

另：远端main已到49566480（0.1.17.3 RC1），包含另一套独立页支持，但仍缺bridge.renew/daemon.detach。PR #7尚未合并，配套正式镜像尚未发布；需要整合上游、通过当前源码full，再走干净main的发行门禁。本轮针对市场兼容的Go修改尚未重跑full，不能沿用旧收据宣称正式发行通过。

## 公开发布结果

[市场站点](https://skyelan1999.github.io/aide-pkg-shop/) 已部署成功，市场提交582ce59a7fe4310b45557ee9f2d231c6927ea4ed，Pages运行39351324782成功。线上Chromium搜索、预发布/依赖说明、390px和实际ZIP下载SHA256全部通过。匿名读取线上catalog及ZIP再次验证1.1.1、terminal-workbench-v1、104320字节与固定摘要；见published.json。

宿主能力标记fd1b136e已推送PR #7。插件README与既有v1.1.1 Release说明同步市场已收录状态，Release仍为prerelease，原tag/ZIP不变。未合并宿主main、未发布宿主正式镜像。旧客户端的目录缓存可能暂时显示上一版，按正常刷新周期更新；该情况不改变Pages公开目录验证结果。
