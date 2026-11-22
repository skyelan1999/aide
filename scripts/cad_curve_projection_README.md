# CAD 参数曲线投影入口

通过项目现有 `cad_python.py` 使用安装在同一 SDK 目录的 `cad_curve_projection.py`。

```python
from cad_curve_projection import project_curve_on_surface, ProjectionError
pcurve, diagnostic = project_curve_on_surface(
    curve, surface, first, last,
    tolerance=1e-7, samples=65, max_nodes=257, use_native=True,
)
```

`curve` 为 OCP 三维几何曲线，`surface` 为同一坐标系的 OCP 支撑曲面，`first/last` 为原三维曲线参数区间。距离沿用输入几何的长度单位，不自动推定毫米。位置变换须由调用者正确处理，不可混用未定位曲面与已定位曲线。

返回新二维参数曲线和诊断。安装原生桥接模块时优先返回 `ShapeConstruct` 实际输出句柄；其后采用明确返回结果的 `GeomProjLib.Curve2d_s`，再按原三维参数取最近 UV 并自适应插值。每条路径都须通过同一实际偏差门槛，失败原因保留在 `priorProjectionFailures`。`use_native=False` 可单独验证 Python 路径；模块不存在时仍可使用该路径，其他加载或 ABI 错误不会被当作模块不存在。

Python 插值节点从17逐次增至 `max_nodes`，该值必须为17–16385间的2的幂加1，默认仍为257。更高预算必须由调用者明确指定；预算不足必须失败，不自动扩大，也不放宽原误差门槛。更密的插值可能仍不收敛，且不保证符合工程要求。`samples` 为17–257间整数，包含端点及偏离插值节点的内部采样，避免把同一组插值节点当作独立检查。结果仍保持原三维参数对应。原生算法有自己的内部离散过程，不以该节点预算承诺其硬运行时间。

返回前检查 `GeomLib_CheckCurveOnSurface` 数值最大偏差及实际采样，原 `tolerance` 预算保持不变。`kernelApproximationRequest` 是较小的内部近似请求，不能解释为实际误差。数值结果不是严格数学误差上界。曲面外距离超差、内核检查未完成、非有限结果或节点耗尽会失败，不能将失败转成零偏差或通过。

工具不读取或导出模型，不自动关联任何边，不改源曲线、容差、SameParameter 标志、读取设置、拓扑或安装位姿。调用者仍须验证相邻面、缝边两条参数曲线和分支、裁剪线、实体闭合、正确初始化的默认/Exact 检查、导出再读回，以及原始尺寸、材料、体积、位姿和干涉。该工具的回执始终为 `modelAccepted=false`。

原生投影或数值极值调用没有独立硬超时；保持 Aide 正常审批和进程时间/RSS预算。仅在内存副本上试验表示修复，保留原始模型及失败版本。需要更高节点预算时明确提供新预算，仍检查原物理误差预算。

原生模块当前目标为 CPython3.12/Linux aarch64，与固定 OCCT8.0.1、pybind11 v5/GCC ABI1019一致，链接 SDK 已有动态库。桥接源码为 `cad_projection_native.cpp`，安装清单记录源码、二进制、编译器镜像和官方头文件输入指纹；第三方许可随模块保存。不替换内核。返回句柄正确不代表原生算法达到请求误差：合成B样条支撑面曾测得原生返回曲线仍超预算，工具记录该失败并采用后续经过测量的路径。

固定 SDK 的 `ShapeConstruct_ProjectCurveOnSurface.Perform` 返回布尔成功时，替换的 C++ 输出句柄不一定传回 Python。合成测试已复现调用方仍持有原旧直线；不能仅凭返回 true 宣称获得新投影曲线。

`test_cad_curve_projection.py` 使用平面、圆柱和B样条支撑面的合成几何，另测曲面外拒绝、预算不足及参数保护；未读取或生成工程零件。正常工程代码的应用、执行和独立验收分别完成。
