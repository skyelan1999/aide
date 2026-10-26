# 数据目录分层与完整性自愈

> 版本：0.1.11.0-RC3 · 分支：feature/permission-panel · 关联：#29 安全 / #30 编号会话 / #38 保险库 / #43 API Key vault / #63 侧车批注
> 权威方案：`proposals/data-integrity/data-layering-and-self-healing.html`

aide 把数据分为三类边界：**程序（镜像只读）/ 项目（绑定挂载，用户所有）/ 状态（启动目录私有绑定挂载）**。
本文描述状态卷 `/data` 从“30+ 平铺文件”重构为语义化分层目录的目标布局、一次性迁移流程，以及启动校验 + 运行巡检 + 自动恢复的闭环。

## 一、目标目录结构

```
/data                        状态卷根（aide 私有，0700）
├─ auth/                     认证凭据（机密，0600）
│   ├─ access-token         本机访问令牌
│   ├─ kdf-salt.bin          Argon2id 固定派生盐
│   ├─ password.phc         账户密码 Argon2id PHC（#29）
│   └─ webauthn-credentials.json  Touch ID / WebAuthn 凭证
├─ sessions/                 会话数据
│   ├─ active/session-*.json    活动会话
│   ├─ archived/session-*.json  归档会话
│   └─ assistant/session-*.json  小蜜系统会话（#30，含 runs/messages 持久化，#62）
├─ assistant/                小蜜私有区（全局、加密）
│   ├─ voice-history.json   小蜜对话历史信封
│   └─ voice-memory.json     小蜜长期记忆
├─ memory/                   全局记忆与反馈缓存
│   ├─ core/                 插件调用成败统计；旧版 aide memory.md 的迁移源
│   ├─ cache/                嵌入/向量缓存（可再生）
│   └─ feedback/             好/坏回答反馈
├─ config/                   配置
│   ├─ settings.json         全局设置（原子写）
│   ├─ profiles.json          模型自定义配置
│   └─ backups/               配置导入导出与历史副本
├─ stats/                    统计（可再生）
│   ├─ token-stats.json
│   └─ token-pricing.json
├─ audit/                    审计日志（追加写，只增不删）
│   ├─ debug-audit.jsonl     外部接入审计
│   └─ security-audit.jsonl  安全/恢复审计
├─ secrets/                  第三方来源/工作区/模型密钥（AES-256-GCM 加密）
│   ├─ vault.enc             统一保险库信封（SSH 凭据 + model:api-key，#43）
│   ├─ master-key.bin        无密码兜底的机器绑定随机主密钥（32B，0600，#43）
│   ├─ sources-secrets.json
│   └─ workspace-secrets.json
├─ comments/                 #63 侧车批注（跨格式 docx/xlsx/pptx/pdf 通用，目录 0700 / 文件 0600）
│   ├─ index.json            id → docPath 索引（原子写）
│   └─ <sha256(docPath)>/<commentID>.json   单条批注（含 replies）
├─ certs/                    TLS 证书与私钥（#29，0600）

`stats/token-stats.json` 按调用保存用量和不可变费用快照，包括模型、提供商、输入缓存命中/未命中数量、费率档位和费用。DeepSeek 官方模型按该次调用对应的费率及时段保存；缺少缓存拆分时会标记为估算。`stats/token-pricing.json` 继续为其他兼容提供商保存可编辑的备用费率。修改费率不会改写历史快照。
│   ├─ cert.pem
│   └─ key.pem
├─ .integrity/               完整性清单 / 基线 / 恢复日志
│   ├─ baseline.json
│   ├─ migration-state.json
│   └─ recovery.log
└─ .quarantine/              损坏用户数据隔离区（不自动删）
```

代码权威来源：`internal/server/paths.go`。所有路径由纯函数返回（如 `SettingsPath(data)`、`SessionPath(data,id,"active")`），`EnsureDirs(data)` 以 0700 创建全部分层目录。

## 二、一次性迁移流程

旧版本把 `settings.json`、`access-token`、`session-*.json`、`voice-history.json` 等 30+ 文件平铺在 `/data` 根。新版启动时运行幂等迁移器：

```mermaid
flowchart TD
    A[启动 New] --> B{DetectLayout}
    B -->|empty| Z[跳过迁移]
    B -->|layered| Z
    B -->|flat| C[EnsureDirs 建立分层骨架]
    C --> D[完整备份到 .integrity/migration-backup-ts]
    D --> E[逐文件复制到分层位置 不删原件]
    E --> F{校验: SHA-256 一致? 会话可解析?}
    F -->|全部通过| G[原平铺文件移入 .quarantine/migrated-ts]
    F -->|任一失败| H[中止 + 清理本次分层副本 原件纹丝不动]
    G --> I[写 .integrity/migration-state.json]
    H --> J[从备份恢复 / 下次启动重入]
```

要点：
- **复制 → 校验 → 再动原件**：任何一步不一致都不删原文件，已复制的分层副本被清理，下次启动从断点续作。
- **原件不直接删**：校验通过后移入 `.quarantine/migrated-<ts>/`，保留一个版本周期。
- **幂等可重入**：已分层直接跳过；部分迁移时，目标已存在且哈希一致视为已完成。
- **损坏会话不强行归位**：JSON 不可解析的会话留在平铺根，由启动校验跳过/隔离，不丢数据。

## 三、完整性校验机制

```mermaid
flowchart LR
    subgraph 构建期
      B1[Docker 构建 对二进制算 SHA-256]
    end
    subgraph 启动
      S1[比对程序哈希与基线] --> S2[校验 /data 目录结构] --> S3[关键文件可解析性]
    end
    subgraph 运行时
      P1[每 5 分钟轻量巡检] --> P2[敏感操作前校验]
    end
    subgraph 上报
      R1[/healthz integrity 状态] --> R2[/api/debug/overview integrity]
    end
    B1 -->|baseline.json| S1
    S3 --> P1
    P1 --> R1
```

- **基线**：`BuildBaseline` 首次启动对运行二进制算 SHA-256，写入 `.integrity/baseline.json`（含 version/commit）。
- **启动校验**：`VerifyIntegrity` 输出 `IntegrityReport{status, checks[], quarantined[], healed[], errors[]}`。
- **运行巡检**：`RunPeriodicIntegrity` 每 5 分钟复查，结果回写 healthz。巡检与其他由 `App` 启动的后台任务统一登记；关闭服务时先拒绝新后台任务、取消可取消工作并等待已登记任务退出，再关闭目录句柄。关闭操作可重复调用。
- 所有写盘沿用 `atomicJSON`（temp+rename），杜绝写一半损坏。

## 四、自愈策略

| 异常分支 | 自动恢复动作 | 安全边界 |
| --- | --- | --- |
| 缺失目录 | `EnsureDirs` 重建 | 无数据损失 |
| 缺失默认配置 | 用默认值打底 | 不丢已有设置 |
| 可再生数据（cache/stats） | 清空重建 | 不影响核心数据 |
| 损坏关键文件（settings/sessions） | 移入 `.quarantine`，settings 回退默认 | 用户数据只隔离、不自动删 |
| 程序层被篡改/换版 | 不自动修复，healthz 标 `degraded`，提示重新部署镜像 | 程序在卷外，只读 |
| 密码/密钥丢失 | 密码不可恢复→重置；access-token 丢失→重新生成 | 符合既有设计 |
| 小蜜历史 re-wrap 失败 | 保留旧/新密文双份，标 `degraded`，不覆盖 | 防止历史丢失 |

每次校验与恢复都追加 `.integrity/recovery.log`，恢复后再校验形成闭环。

## 五、healthz / /api/debug 输出

```json
// GET /healthz
{"status":"ok","service":"aide","integrity":"ok"}
```

`integrity` 取值：`ok`（正常）/ `degraded`（可降级运行，如程序疑似换版、re-wrap 失败）/ `corrupted`（严重损坏需人工介入）。`GET /api/debug/overview` 顶层同样暴露 `integrity` 字段，无浏览器亦可诊断。

## 六、部署注意事项

- **项目记忆**：本地工作区的 aide 长期记忆保存在 `<project>/.cache/aide/memory.md`，不通过全局 `/data` 共享；SSH 工作区目前按 workspace identity 隔离并保存在本机 `.cache/projects/<id>/aide/`，不会随远端仓库同步；详见 `memory-access.md`。
- **Docker 数据映射（2026-10-16）**：启动器将 `/data` 绑定到启动目录 `.aide-data/storage/data`，`/home/aide` 绑定到 `.aide-data/storage/home`，与所选项目分开。旧命名卷校验迁入后保留；删除 Docker 卷不删除这些宿主数据。见[启动目录持久化](launcher-storage.md)。
- **首次启动迁移**：旧平铺卷在首次启动时自动迁移，全程备份可回滚；无需人工干预。
- **权限**：分层目录 0700，敏感文件 0600。
- **TLS 路径对齐**：当前运行期 TLS 证书仍写 `data/tls/`（#29/B 任务在改）；`paths.go` 已定义 `certs/` 目标位置，待 B 完成后统一对齐。

## 文档绑定与缓存分开（源码 0fef6411）

2026-10-17：正式文档目标来自工作空间自动系统文档配置；generated_documents.go 为四阶段编号/索引准备临时副本，再发布到绑定。没有绑定时四阶段保留缓存 system-docs；write_file 的 system-docs 正式提案不因此获得只读 /context 写权限。项目记忆和应用 /data 的职责保持独立。

![文档绑定与缓存分开](../images/current-20261017/02-workspace.jpg)

[完整原图、环境与边界](../ui-evidence.md)。

插件经验 `memory/core/plugin-experience.md` 只保存工具与插件标识、主机观察到的最近调用成败、累计次数和日期，不保存插件返回正文或错误详情。后续系统提示仅复用这些统计，并明确历史结果不代表当前可用性或授权。旧记录中的自由文本在生成提示时忽略；下次正常更新经验文件时改写为结构化成败描述，不做启动时删除或另存敏感副本。此限制针对经验重用链路，不代替实时工具输出的信任边界和工具权限检查。

旧平铺布局迁移和会话桶归位在改动数据文件前，先把涉及的原文件、已有目标及旧状态文件保存为受限原件副本，并写入 `.integrity/flat-migration-undo.json` 或 `.integrity/session-bucket-migration-undo.json` 恢复日志。原件按流复制，记录 SHA-256 和文件权限；日志及状态文件通过同目录临时文件、同步、重命名发布。准备日志失败时不改动数据文件；最终状态或提交日志保存失败时返回错误并尝试恢复。恢复失败会保留日志和原件，阻止启动继续；修复存储问题后，下次迁移入口先重试恢复，即使当前布局看似无需迁移。已提交日志仅清理，不回滚完成的结果。

正常完成或恢复后只清理这次创建的 undo 副本，原有 migration-backup、session-bucket-backup 与隔离文件继续保留。会话桶手动回滚若无法保存完成状态，也返回明确错误。undo 需要额外临时磁盘空间；迁移与恢复要求应用尚未开放数据卷，不能与其他进程并发编辑该卷。该机制覆盖失败和进程中断后的重试，不宣称文件系统损坏、断电持久性或跨卷事务保证。

小秘历史采用原子替换保存，加密密文成功写入后才更新内存缓存，保证同实例锁定、解锁使用最新的已保存内容。加密历史处于锁定状态时清空返回403，须先解锁；清空写入失败返回500并保留原历史和缓存。启用加密、修改密码、关闭加密或重新包装密钥若落盘失败，也不发布部分内存状态。成功清空仍保留加密和密码校验，锁定或重启不会恢复旧条目；此操作只清理小秘历史，不删除独立长期记忆或会话记录。
