# 当前完整离线包公开交付 · 2026-10-17

任务 `continuity-offline-delivery-20261017`，基于本轮[运行候选验收](continuity-runtime-candidate.md)。两个平台使用相同源码哈希，ARM 已通过原始启动脚本及进程重启、实际 MCP 与离线解压启动，AMD64 构建及实际导入通过；AMD64 原生运行 / Windows 由用户验收。

## 发布前盘点

实际公开 GitHub API 确认现有 `v0.1.17.0-RC2` 是公开预发布，Release ID `406597437`，原有 23 个附件；两个新归档名均不存在。归档已校验无宿主数据路径，摘要和身份见[机器可读交付证据](continuity-offline-delivery-proof.json)。

本机没有 `gh` CLI；使用已有登录的 Safari Release 编辑页，不新建标签、不修改或提交已有未保存说明。通过原生文件选择框选中当前 ARM 包后，页面显示 “Uploading your file now...”。这只证明上传已经开始；公开 API 确认 uploaded、大小和 SHA256 之前，不记为上传完成。

## 当前状态

- ARM64 附件公开校验通过；AMD64 同一页面串行开始上传，仍待公开完成。
- 默认清单、仓库校验文件尚未修改；只准备本地候选清单。
- main 未合入或推送；未升版、打标签或提交 Release 编辑表单。
- 生产实例保持；完整原生工作台验收暂缓，Windows 用户验收，真实向量暂缓。

## 后续门禁

两个公开附件精确校验通过后才更新默认清单及摘要；交付输入变化后重新执行 full，随后按干净 main 和 release-check 门禁推进。保留已有公开附件、镜像与数据作为回滚依据。

## 上传期间的门禁与配套资料

对本轮真实候选清单运行 `scripts/verify_prebuilt_assets.py`，返回 exit1：`linux/arm64: no unique uploaded public asset`，mutations=false。公开附件尚未就绪，门禁按预期拒绝，未激活默认地址。

已准备独立 `AIDE-runtime-07cdef2d4719-README.txt` 和 `AIDE-runtime-07cdef2d4719-SHA256SUMS.txt`，写明两架构归档身份、启动步骤、资料保留、原生 AMD64 / Windows 和完整工作台验收边界；未修改现有 Release 说明，也未覆盖旧摘要附件。配套资料待主归档上传后追加。

## ARM 公开附件已完成

公开 API 返回 ARM 附件 ID `649882716`、state `uploaded`、771016230 字节，SHA256 与本地 `d1392c59b789f8d8d7b8150d00a604ac557a7a5d2e6b353e8f7959485ef16283` 一致。原 23 个附件全部保留，公开 Release 正文哈希不变。

实际访问[ARM 完整离线包](https://github.com/skyelan1999/aide/releases/download/v0.1.17.0-RC2/aide-0.1.17.0-RC2-linux-arm64-4b784ce0724f-offline.tar.gz)，Range 0–1023 返回 HTTP206，Content-Range 总大小正确，读取 1024 字节且 gzip 头正确。这是公开下载入口冒烟，不是网络全包下载或另一设备导入验收。

本轮候选公开门禁已推进到 `linux/amd64: no unique uploaded public asset`，exit1/mutations=false；ARM 对应项已通过，仍不激活双架构默认清单。随后在同一现有 Safari 页面选择精确 AMD64 `382c90e72fca` 包开始上传，未重复上传 ARM，未提交说明表单。
