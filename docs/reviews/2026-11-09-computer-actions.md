# 电脑控制动作补齐与当前实例验收

任务：`computer-actions-20261108`；集中跟踪：[Aide #835](https://github.com/skyelan1999/aide/issues/835)。基线为 `3edafef75ee6f17d8ddc680721c67d2c0428ce8a`，继承本地快捷键、截屏、目标路由、辅助审批和虚拟光标改动。以下是本地候选实现与验收，不是新发行版或 Codex 对比测试。

## 最终接口与执行行为

- `computer_snapshot` 返回实际图像尺寸、目标窗口 ID/桌面矩形、`source.screenshotId`，并通过可选 OCR 返回 `textLocations`（文字、置信度、桌面中心与矩形）。可直接使用文字定位结果，也可向 click/drag/scroll 传 `coordinateSpace:"image"` 和同图 `screenshotId`，无需模型自行缩放坐标。默认仍兼容桌面坐标。
- 图像引用仅在当前 Task 内保存元数据，最多保留 8 个应用，2 分钟有效。绑定确切进程、应用规则、窗口和几何；跨任务、过期、进程重启、非法图像坐标均拒绝。执行时再次校验窗口身份与位置。截图引用摘要和图像字节摘要用途不同，不要求二者相等。
- 新增 `computer_drag`：起点 `x/y`、终点 `toX/toY`，`durationMs` 默认 900、范围 100–2500。整段平滑拖拽持有应用与全局实体输入队列；取消、锁屏、焦点丢失或窗口移动时中止，并在 defer 中释放鼠标。起终点均限定在同一目标窗口。
- 新增 `computer_scroll`：目标点 `x/y`，`deltaX/deltaY` 为整数像素，每轴范围 -2000–2000，至少一轴非零。正值分别向右、向下；可指定内部滚动区，支持双轴和反向。
- `computer_key` 支持 CMD/CTRL/ALT/SHIFT 的多修饰组合及常用别名、字母、数字、标点、方向、Home/End/PageUp/PageDown、F1–F20。最多 4 个不重复修饰键加 1 个主键；非法格式先拒绝。macOS 使用实际键码与修饰标志；不是所有键盘布局的通用文本输入。
- 辅助审批接入新动作：审核准确动作、原图与带标记的局部放大图，拖拽同时提供起终点，审核后复核两端区域。图像坐标由服务端转换的事实通过可信元数据交给审核模型，避免把正常坐标变换当成篡改。图片仅驻留本轮内存；手动审批及敏感操作边界保持生效。

## 实测中修正的问题

Safari 曾把 66×20 的提示窗当成目标窗口，截图为 132×40，而 inspect 是浏览器主窗口，导致证据与坐标错位。原生截图选择现在优先匹配 AX 焦点/主窗口的实际 frame；无法匹配时选择前排有效普通窗口并过滤微小提示窗，不简单按最大面积选窗。

OCR 使用后台单飞工作器，调用等待上限 4 秒。超时仍可返回截图，旧工作未结束时不新增工作；迟到结果不会二次恢复 continuation。保留锁屏/取消复核。早期截图超时还涉及窗口选择，不能全部归因于 OCR。

## 当前本地 Aide 实测

所有实际动作由当前 Aide 会话和 `gpt-6-luna` 调用产品工具完成。独立 CUA 仅准备一次性夹具、观察结果与清理，没有替 Aide 执行验收动作。复用 `aide-aide-1` 和现有 BuildKit；未启动第二个工作台实例。

| 项目 | 实际证据与结果 | 结论 |
| --- | --- | --- |
| 图像坐标拖拽 | 会话 #92；2477×1614 截图映射到 Safari 窗口。夹具蓝块由 x20/y20 到 x393/y20；HUD moves=73、releases=1。独立 CUA 读回一致 | PASS |
| 双轴与反向滚动 | #92 的内部 Scroll sandbox：0/0 → 0/350 → 450/350 → 250/200；实际工具分别执行纵轴 +350、横轴 +450、双轴 -200/-150。最终 CUA 读回 250/200 | PASS |
| 复杂组合键 | #93 TextEdit：Alpha Beta Gamma；CMD+RIGHT、ALT+SHIFT+LEFT 后仅输入 Delta，得到 Alpha Beta Delta；CMD+Z 恢复 Gamma；CMD+SHIFT+Z 恢复 Delta | PASS |
| 恢复原测试文稿 | #93 恢复原有两行测试文字；独立 CUA 读回一致；未保存或关闭文稿 | PASS |
| EDA 视觉定位 | #94 在原生 EDA 开始页用 OCR 视图（V）坐标 x227/y51 点击，截图/OCR 与独立 CUA 均显示正确 View 菜单（左侧面板、右侧面板、底部面板等） | PASS，限定位与打开 |
| EDA 菜单收尾 | 同一标题再次点击不收起，模型反复尝试并触发重复调用保护。另开本会话内的收尾轮次，Aide 发送 ESCAPE，截图确认菜单项消失；独立 CUA 确认开始页 | PASS，经过任务纠正，非一次自主往返 |

早期诊断 #91 已取消；#92 中误拦、过期图像引用未执行的尝试也保留记录。滚动验收曾需要澄清目标区域，恢复文稿曾被误判后经明确任务说明重试。没有手动批准电脑动作卡，但有测试指令纠正与普通澄清回答；不能据此声称所有普通操作均能零干预自动完成。EDA 没有打开工程、点击菜单项或修改工程文件。

测试页已关闭，仅关闭本次创建的 fixture 标签；保留其他 Safari 页面。TextEdit 原内容已恢复，EDA 下拉菜单已收起。#91–#94 均无待审批或运行中的测试轮次。

## 自动检查与部署身份

- quick：PASS，`.agent-state/verify-20261108T162315807243Z.log`。
- 当前容器内 Go 聚焦回归：`Test(UIReview|Assisted|Surface|Approval|Computer|PluginImages)`，PASS（14.304s），`/tmp/aide-computer-actions-go-verified.log`。覆盖图像引用归属/PID/TTL/坐标、手势边界、按键语法、两端区域变化及既有审批/图像路径。
- 原生 14 项回归：PASS（4.322s），`/tmp/aide-computer-actions-native-deadline-tests.log`。含组合键、FIFO、取消竞态、UTF-16 选区、几何及可选工作器超时/单飞恢复。
- 原生完整编译与安装：PASS，`/tmp/aide-computer-actions-native-window.log`、`/tmp/aide-computer-actions-native-window-install.log`。
- 服务源码重建：`/tmp/aide-computer-actions-local-build-resolution.log`；当前服务 healthy、`status=ok`，原有 `integrity=degraded` 未变成 clean。
- 本地动作/API脱敏证据：`.agent-state/computer-actions-live-evidence.json`；会话 #92 拖拽轮次 `8e23f0be4ba652ab24a15afc1ff84fb2`、滚动轮次 `822f298b62d2834d5c0f94f9959fc70e`；#93 `409b99381400d702a9acf1a928ace316`；#94 收尾 `fd6341b0315f1cc9e00713955492275d`。

| 身份 | 值 |
| --- | --- |
| agent-route 内容指纹 | `09fd40fae6dfd84641f99920038baf393025f90501cf57d24c4b09dcf276204d` |
| 当前镜像 ID | `sha256:a11a457f2aa78d245e737abd1b3edf54c4aeec7e3381c194ba2ffdcad1b744ca` |
| 镜像 aide.srcsha（不同算法） | `83a0fa449f3646a8f9c3890082ecfc0729425d0793850ddb766946d847a9d8e7` |
| 已安装 AideDesktopBuildDigest | `a4d22874367820a6aa19d577ad52fa36573e9864e22e7e41c89c5f6c0b6d0f51` |
| 已安装 AideDesktop 二进制 SHA-256 | `4784b1c87e975bf6841ec6adefb8fcbd5fa08fe54a6500d862fa3f44fe61c604` |

Go 使用当前容器和已校验官方摘要的本地 Linux SDK，低内存参数 `GOMEMLIMIT=128MiB GOGC=10 GOMAXPROCS=1 -p=1`。此前较高内存参数的测试编译接近容器内存上限，已只终止自有测试进程，随后低内存回归通过；没有新建测试容器或调整全局 Docker 资源。

## 限制、回滚与交接

未执行完整 Go race、Windows/Linux 真机、多显示器硬件、实际拖拽中取消释放、全部键盘布局/功能键硬件和单轮 10X 多软件压力验收。取消释放有实现与队列回归，不能替代真实中断手势验收。右键/双击、用户指定多窗口选择与完整 EDA 设计仍不在本次完成范围。

源码仍在 main 的本地未提交候选中；没有提交、合并、推送、升版、发布或关闭 #835。回滚应按任务初始状态逐项撤销本次动作增量并重建同一实例，保留继承改动、会话数据和文稿；不要使用整树 reset、git clean 或清空 Docker。当前原生/服务均已本地重建，本报告记录的源码指纹不等于 clean 发布身份。


## 源码集成后续

用户随后授权“和到main然后推送”。功能提交 `90b06494093da4568e860ebc76915a69cd77d40c` 已在 main 并推送 origin/main，远端 SHA 已核对一致。上面的未提交状态是实测时的历史状态。当前只完成源码集成，没有升版或发布安装包；仓库接受直接推送时提示 PR 和必需 gate 规则被绕过，因此不声称远端 CI 门禁已通过。详见 `docs/tasks/computer-actions-main-20261109.json`。
