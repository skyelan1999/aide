# URDF 坐标、姿态与模块名称

新增工具栏“坐标与角度”“名称标注”开关。坐标面板选择连杆或关节原点，支持机器人根坐标系与直接父节点坐标系；显示 XYZ（m）及固定轴 XYZ 的 RPY（rad）。姿态使用相对矩阵、四元数转 ZYX Euler 得到，关节变化与归零同步更新，根模型整体变换及相机视角不改变相对根的数值。单自由度关节另外显示关节值及对应单位。

模块名称取自原 URDF link name，投影到三维连杆原点，点击打开对应坐标。最多 80 个标签，超出时提示；全部 link/joint 仍可通过面板选择。显示使用纯文本、支持长名称截断与完整 title。无写文件／改名行为，无新依赖、后端接口或存储。仅按交互调度绘制；关闭时清除节点、输出、标签引用及原有图形资源。标签不检查几何遮挡；密集节点可能重叠，坐标下拉框保留完整选择路径。

## 实际观察

复用原组件服务和真实产品模块：6 连杆、5 关节、2024 三角面的合成模型。点击名称 tool_link 自动打开并选中坐标面板。shoulder 调为 0.8 rad 后，末端原点根坐标 X=0.348353、Y=0.358678、Z=0.220000、Yaw=0.800000；选中 shoulder 时另外显示当前关节值 0.800000 rad。切换末端的直接父节点坐标系后 XYZ/RPY 为零，符合该 link 在其直接 URDFJoint 下的位姿。父节点不等同于父 link。

1280×800 桌面明色与 390×844 窄屏暗色检查；窄屏 scrollWidth=390，无横向溢出。截图保存在私有 `.agent-state/urdf-coordinates-20261019/`。组件验收仅替代外围状态和合成文件服务，不当作真实工作台文件树、持久化保存或 SSH 验证。

本轮沿用用户对这类界面变化简单测试、合并并及时推送主线的指示。未新建 Release、未替换正在运行的 Docker 实例，未声称 full/Go race 验证；运行实例更新后再刷新页面生效。

Final quick: 131 checks PASS; fingerprint `b85a30d15b61e66556bfac8dd67662eaa79318ab9e0ae02c9a203e4bd37ded73`. Docs check: 343 Markdown files, 1268 local links PASS. Real r128 tests cover all RPY components, transformed robot root invariance and direct-parent pose. Reset returned tool origin to (0.5, 0, 0.22) and yaw 0; source mode removed all canvas and label elements. Owned component tab/server closed and temporary viewport reset. Earlier interrupted quick is not the final receipt.
