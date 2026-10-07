# 桨叶建模与气动插件 v1.0.0

两个可执行工具：`propeller_analyze`（只读计算）与 `propeller_reconstruct`（生成文件提案）。
通过 Aide 插件面板上传 ZIP 后启用。运行时仅需 Node 与 Python 3，Python 只使用标准库，无下载依赖或联网动作。

默认官方基线：DJI Avata 360 3340S，直径约 83.1 mm、名义螺距 101.6 mm、照片四叶。材料及重量为参考资料，不参与强度或刚度计算。
弦长、翼型、后掠、7 mm 气动切除半径都是假设；桨毂与安装接口不建模。气动方法为轴向 BEMT，不是 CFD；轴功率不是电池功率，不输出绝对噪声或续航改善。

示例输入：
```json
{"diameter_mm":83.1,"pitch_mm":101.6,"blades":4,"mass_g":455,"rotors":4,"altitude_m":0,"axial_speed_ms":0}
```
建模再加 `output_dir`，例如 `avatar360_prop_concept/new_3340s`。结果是待应用文件提案，不能宣称已落盘。

本地直接重现：`python3 -I worker.py --output NEW_DIRECTORY`。目录必须不存在。输入单位写在工具 JSON schema 与 report.md 中。
输出包含镜像 STL、几何/气动/敏感性 CSV、SVG 图表、离线旋转查看器、图标与网格闭合记录。

修复旧概念脚本的三叶/127 mm 错误基线、弦长未正确旋转、端点重复积分与缺少诱导速度收敛记录。旧脚本保留作历史资料；新工具执行同一计算引擎。
