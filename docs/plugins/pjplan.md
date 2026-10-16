# PjPlan 项目文档插件

PjPlan 按随包十进制分类目录提供分类查询、规范文件名和常用项目管理文档模板。配置位于“设置 → 插件 → PjPlan 项目文档”。

## 文件名

新建文档使用以下格式：

```text
<分类代码> <文档类型> - <项目标识> - <P|R><A|B|C><小版本>.<扩展名>
```

例如：`102 62 Design Specification - 1234567 - PA1.md`。P 是评审版本，R 是发布版本；A/B/C 表示大版本，数字表示小版本。项目标识及版本由 PjPlan 设置提供。Aide 服务端会对新建文档名进行校验；已有附件的原文件允许保持原名修改。

分类目录源自用户提供的 `SYN Decimal Class 12.xlsx` 的 `Decimal Class` 工作表。插件仅打包分类代码、通用英文名称和中文说明，不包含申请人、申请日期等表格字段。1402 分类码保留，显示名称改为“Generic Configuration Management Rules / 通用配置管理规则”。

## 工具

- `pjplan_class_lookup`：按分类代码、英文标题或中文用途查询。
- `pjplan_build_filename`：按插件设置生成文件名。
- `pjplan_document_template`：生成项目计划、进展报告、验证计划和里程碑记录的 Markdown 起始内容；其他类别提供标题骨架。

撰写文档时先确认分类，再生成文件名。若项目标识或版本尚未配置，先在插件设置中补齐；不要猜分类或自行拼接文件名。

## Aide 保存与权限

`write_file` 对 Markdown、文本、CSV/JSON、PDF、Office、SVG 和 draw.io 等文档默认使用 `system-docs`（自动系统文档）来源。该来源独立于工作区目录，默认对 AI 可读写，并继续服从当前工作区沙箱、来源启用状态及 AI 可见性。未绑定用户目录时，使用 Aide 私有的自动系统文档缓存，不会写入 `/context`。其他引用来源只有在当前工作区 AI 可见且来源标记为读写时才能作为目标；已有文件必须先附加到任务。审批提案会固定来源身份，来源配置变化后需重新生成提案。

代码和工程文件继续保存在工作区。`write_file` 生成审批提案而不直接落盘，只有应用回执确认后才表示保存完成。

## 插件仓库

此目录中的源码镜像目标插件仓库 `git@github.com:skyelan1999/aide-pkg-PjPlan.git`。初次访问时远端为空，因此实现按 Aide Plugin Protocol 独立建立；以插件包为分发源，Aide 仓库内的 `plugins/pjplan/` 是运行副本。
