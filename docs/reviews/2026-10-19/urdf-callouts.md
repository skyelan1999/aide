# URDF 图上引线标注与模块高亮

用户明确要求坐标和弧度显示在图上，用引线连接对应模块，标注不互相覆盖，点击高亮模块。替代上一轮侧栏读数和靠近原点的浮动名称：侧栏现在只保留节点、参考系等选择；数值在模型画布的卡片中显示。

## 实现与边界

SVG 折线与原点圆点连接卡片和三维节点。每次绘制投影节点原点、按侧边和锚点顺序排布；卡片宽 204px／精简名称 168px，高 100px／38px，间距 8px，顶部预留导航。宽度至少 620px 时双列，否则单列。容量由画布高度决定，超出时分页；不足以放一张卡片时提示放大，不叠加、溢出卡片。图中只标注当前相机裁剪和视口范围内的节点；不模拟几何遮挡，引线可能交叉。显示 3 位小数，过大数字使用科学计数法，原始姿态计算未舍入。

正常显示每个 link 的卡片，附带父关节名称／当前关节值；选择 joint 后替换其对应子 link 的卡片，避免同时重复标注同一位置。关节／名称均来自原文且以纯文本显示。坐标沿用机器人根／直接父节点参考系，RPY 是固定轴 XYZ 的等价姿态主值。坐标开关提供名称加数值；单独名称开关提供精简卡片。

高亮只遍历选中 link 自己的几何，遇到子 link 停止；临时复制材质，保留原材质引用，选择变化时恢复并释放复制品。浅蓝色与独立包围框配合显示。关节选择映射子 link。模型点选使用射线，仅接受可见 mesh；超过 5px 的旋转拖动和非主指针不触发选择。异步资源到达重新建立选中模块高亮。销毁回收临时材质、包围框、SVG、卡片及 pointer 监听，原 XML 不写回。

## 定向证据

`scripts/urdf-preview.test.cjs` 的真实 Three.js／URDFLoader 测试继续通过，新增重合锚点 81 项跨页布局：桌面、窄屏、极小画布、名称与完整卡片均检查卡片两两无交集、边界不溢出、跨页不遗漏／重复、页码夹紧。实际产品高亮函数在真实模型中验证：只改变所选模块的一个 mesh，不改变子 link，关节正确映射模块，清除后恢复原材质，临时材质 dispose 一次。

本轮使用原组件夹具服务、真实产品查看器与实际 WebGL；外围工作台状态／文件 API 为合成夹具。没有新增真实工作台文件树、SSH、Windows 或原文件保存验证。生产运行镜像不变，源码更新需原 start.command 更新实例后生效。不新增公开 Release；按用户既有简单验证、主线及时推送指示完成源码交付，不把 quick 当 full/Go race 或公开 Release 门禁。

## 浏览器结果

1280×800 明色桌面：6 个模块卡片全部可见，逐对 DOM 矩形检查无重叠。点击 tool_link 卡片后，末端变为浅蓝色且有独立包围框；直接点击机座，选中值切换为 base_link。选择 arm_link 并把 shoulder 调到 0.8 rad，arm 卡片 Yaw=0.800，mimic 卡片 Yaw=-0.800，末端 XYZ=(0.348,0.359,0.220)。只有 arm 模块变色，wrist 等子模块保持原颜色。相机 fit 为标注边缘预留空间。

390×844 暗色窄屏：显示单列两张卡片、共 3 页；实点下一组进入 2/3，卡片边界都位于画布内、两两无交集，scrollWidth=390。直接点击缩小后的模型仍能选中 base_link；拖动旋转后选中值未变，引线锚点随视角变化。关节选择 shoulder 后用该关节卡片替代 arm_link，切换父节点参考系后图上单位和读数更新。最终窄屏卡片的引线连接内侧边缘（画布 x=144），不绕到外侧。

切回 XML，canvas、卡片及 SVG 引线计数都为 0。完成后关闭唯一自建组件标签和 59198 夹具服务，恢复临时视口；没有操作生产服务。截图与窄屏测量记录位于私有 `.agent-state/urdf-callouts-20261019/`。一次检查 SVG 时选择器匹配多条线，改为明确第一条再比较；该工具选择器错误不是产品异常。

Final quick: 131 checks PASS; fingerprint `b3feb955c0592def8a2ba8deacca50e94bfbd4e0ef925c33f2594ef6517e35df`. Documentation links and diff whitespace checks PASS. Full/race and production runtime acceptance were not run.

Source delivery: feature `c0356bcc`, main merge `0596ad50`, pushed to origin/main. Revert with `git revert -m 1 0596ad50`; no data migration. Runtime image remains unchanged.
