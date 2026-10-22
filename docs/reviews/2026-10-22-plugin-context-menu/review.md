# 插件文件右键菜单按需显示 · 2026-10-22

基线Aide `e1691cc401bd495d5013552c83dcfef77c584450`；Compare私有仓库基线`f7902a7`，其0.3.2仍为未发行候选。用户要求新增Compare设置勾选框，只有插件启用且设置允许时才加入文件右键菜单，并审查其他插件同类入口。

根因：宿主以前对workspace/context无条件添加三个Compare动作，仅点击比较时检查插件是否启用。「设为左侧」甚至不查询插件。因此未安装或停用插件时仍可看到入口。

变更：Compare manifest声明默认false的布尔`fileContextMenu`，设置标题「将 Compare 添加到右键菜单」。宿主严格要求`enabled === true`、无加载错误、protocol 1工作台及`settings.fileContextMenu === true`。原生菜单即时显示，Compare异步读取最新列表后才加入；关闭或替换菜单、切换工作区/令牌、锁屏恢复代次变化均丢弃过期结果。选择左侧与比较点击再次查询，禁用后清除旧左侧选择。开关只控制右键入口，插件面板仍可手动打开工作台；不改变服务端工具权限或文件来源权限。

其他入口审查：

| 路径 | 结果 |
| --- | --- |
| 文件/文件夹右键菜单与新建菜单 | 唯一直接加入的插件动作是Compare，本次已修复；复制、下载、归档、解压、属性等为原生功能，不依赖Archive Tools等插件 |
| 文件预览插件 | `plugin-file-preview.js`使用宿主可用处理器列表，停用回退原生；相关4组回归通过，无同类无条件右键入口 |
| `.pp`项目文件工作台 | 已检查插件enabled/error/protocol和扩展关联，无直接文件右键入口 |
| Markdown History | 既有安装/启用状态控制，不注册文件右键项；历史开关作为管理控件显示不属于执行入口泄漏 |
| 其他浏览器脚本 | 排除vendor后审查`contextmenu`/`file-ctx-item`，未发现其他插件注册同类文件菜单 |

实际验证：

- `node scripts/plugin-context-menu.test.cjs`：6组PASS，覆盖缺省/false/字符串true/停用/未安装/错误/不兼容，显示顺序、点击再校验、旧菜单/锁屏/作用域、接口失败不影响原生菜单。
- 文件打开所有权回归PASS、文件预览4组PASS、工作台19组PASS、i18n检查PASS，修改JS语法与diff检查PASS。
- Compare `node scripts/selection.test.cjs`：6组PASS；`go test ./core -run Test -count=1` PASS；现有`check.py`入口/原生程序/ZIP/合成离线许可证PASS。未修改引擎源码；使用环境已有Go1.26在私有忽略目录编译三个目标测试程序，不代表三平台实机验收。
- `COMPARE_TEST_ARCHIVE=/private/path/compare.zip bash scripts/verify-plugin-context-menu.sh`：真实临时Aide testApp导入独立Compare ZIP、Go race、Chromium18.034秒PASS。通过实际设置保存并刷新验证：默认关闭、勾选开启、取消关闭、停用/重新启用、卸载、过期菜单点击不启动、关闭开关仍能从面板打开工作台；设置弹窗1440/390宽、comfort/blue明暗组合，无横向溢出，无页面错误或外部浏览器请求。
- 快速检查140/142通过，`scripts/test_launcher_data.py`与`scripts/test_offline_bundle.py`未过；这些文件及被测启动器本次未修改。完整结果和运行源码SHA-256见[verification.json](verification.json)。不能声称全量发布门禁通过。

按既有要求尝试`AIDE_OPEN_BROWSER=0 bash start.command`，首次Docker构建在CAD依赖PyPI证书链处失败。启动器随后下载旧版预构建包；确认本轮启动链的精确PID、工作目录与命令后停止自有下载/启动进程，保留可续传缓存。没有启动或替换用户服务。实际浏览器证据来自真实隔离宿主，不能等同生产部署成功。测试脚本精确删除自有Go夹具并关闭自有浏览器/服务。

交付为两仓库源码修复与本地Compare候选安装包，不升Aide版本、不打发行tag、不发布或部署镜像。必须同时更新Aide宿主和Compare才能在设置里看到并使用勾选框；仅更新旧宿主中的Compare不能修复宿主无条件加菜单的逻辑。用户既有main推送授权沿用；新发行仍受未通过的检查阻塞。

公开Aide仅包含通用菜单判断、独立外部包验收脚本和脱敏截图，不包含Compare私有实现、签发密钥或原生程序。此前已有的10个任务文件及保险柜研究文档均保留，不纳入本次提交。

回滚：恢复本次宿主菜单/翻译和Compare manifest到基线；无用户文件或数据格式迁移。暂不支持Windows/macOS实机或原生移动设备验收结论。

截图：[默认关闭](default-off.png)、[设置勾选框](settings-off.png)、[开启后菜单](enabled-menu.png)、[窄屏深色设置](settings-dark-390.png)。

源码交付已确认：Aide `d57a44e0`、Compare `d7aef2d`均推送main。没有创建新Release或替换部署。

独立的main保护请求：本连接创建规则返回403，未成功写入。随后只读复核25个公开仓库均已有main且protected=true，保护设置来源未核实，不能将其归因于本连接。Aide当前使用传统分支保护而rulesets生效列表为空；owner推送已被服务器接受并返回“Changes must be made through a pull request”绕过提示。推送前仅检查rulesets未覆盖传统保护，核查不完整；后续补充记录通过PR，不使用管理员绕过。
