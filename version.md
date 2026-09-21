# aide 版本记录

**当前版本：0.1.1.0 RC1**

## 0.1.1.0 RC1（2026-09-22）

- 多模型管理与上下文统计：模型列表管理（自动获取可用模型名）、侧栏模型选择器、DSH 风格上下文统计卡；参考 DeepSeek DSH 设计


## 0.1.0.0 RC1（2026-09-21）

- 建立版本管理机制：四位版本号（产品级·重大·大版本·日常）+ RC 补丁号，格式 `X.Y.Z.W RCn`
- `version.md` 承载当前版本与 release note；`scripts/version.sh` 托管升级流程（bump / patch / note / tag / check / install-hooks）
- 版本升级以 git tag `vX.Y.Z.W-RCn` 打在对应提交上，**仅 main 分支打 tag**
- git 钩子（prepare-commit-msg）自动为每次提交标注当前版本号
- 界面展示当前版本：侧栏运行卡片与设置面板底部（`/api/config` 返回 version）
