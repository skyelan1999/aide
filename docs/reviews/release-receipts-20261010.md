# 任务发布记录：操作与验收边界

## 用途

记录某轮任务关联的发布观察，放在轨迹的“输入与输出 → 此轮详情 → 缺口与发布”。操作者选择 JSON，检查预览后确认导入。服务器持久化任务记录、时间和摘要；不访问导入 URL，不下载远端资产，不自动发布、不替换生产。

本批仅编译和语法检查，运行/API/Safari验收 NOT_RUN，交给 Luna。资产摘要是本地文件摘要，远端字节数只是元数据；两者不能证明远端资产内容相同。commit 为操作者提供，工具不核验 tag 指向。

## JSON 契约

```json
{
  "schema": 1,
  "tag": "v0.1.17.0-RC2",
  "commit": "0000000000000000000000000000000000000000",
  "url": "https://github.com/skyelan1999/aide/releases/tag/v0.1.17.0-RC2",
  "observedAt": "2026-10-10T00:00:00Z",
  "deployment": "unknown",
  "artifacts": [{
    "name": "example.zip",
    "sha256": "0000000000000000000000000000000000000000000000000000000000000000",
    "bytes": 123
  }]
}
```

以上全零值仅演示格式，不能作发布证据。deployment 可取 `unknown`、`not_deployed`、`reported_deployed`，最后一种仍是操作者声明。1–64个资产、名称唯一，40位提交摘要、64位SHA256；请求64 KiB，每任务最多50条。服务器补 `source=operator_import`、`recordedAt` 和 `digest`，导入不能指定这些字段。SHA256表示完整导入内容摘要，服务器不能确认外部事实。

## 从正式发布观察生成

先完成现有发布门禁及发布流程，再捕获 GitHub Release 元数据；本工具不能替代发布门禁。

```bash
gh release view "$TAG" --json tagName,url,assets > /tmp/aide-release-observation.json
python3 scripts/release-receipt.py \
  --release-json /tmp/aide-release-observation.json \
  --assets "$ASSET_DIR" \
  --commit "$VERIFIED_TAG_COMMIT" \
  --output /tmp/aide-task-release-receipt.json
```

commit 必须先由操作者核对远端tag及源码；变量来自当前发布记录，不可用示例值。工具检查每项资产对应本地普通文件、拒绝符号链接、名称与本地字节数不一致，并计算本地 SHA256；以独占创建写输出，拒绝覆盖旧观察。部署默认 unknown。导入生成的JSON后，记录仍显示“操作者记录”。该工具未接入发布脚本自动运行，防止发布观察失败被误认作未发布并重复执行发布。

## Luna 必验

- 有鉴权/无鉴权；正确/错误/删除的会话及任务；超过大小、非法字段、非法URL、重复资产、超50条。
- 新记录持久保存和重启恢复；相同内容幂等；保存失败不留内存假记录。
- 运行中任务改变快照时409，刷新后可导入；分页快照包括发布记录。
- Safari预览、取消、确认、锁定、切换会话、过期异步响应；导出包含记录；专业/经典、明暗、窄屏及英文。
- 捕获真实发布观察时独立核对远端tag、资产内容和部署状态；不得以页面显示记录认定发布验收通过。
