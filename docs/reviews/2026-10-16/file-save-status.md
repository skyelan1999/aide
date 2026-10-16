# 编辑后的保存状态一致性

任务：[file-save-status-20261016](../../tasks/file-save-status-20261016.json)。实际Safari前轮发现自动保存关闭时，保存后继续输入仍显示服务器已保存，独立服务器正文未改变，见[原始运行证明](workbench-file-position-runtime-proof.json)。

本批输入依据已确认的正文及版本更新状态；撤回至已确认正文无需额外写入。尚未保存的新空文件不会误标为已保存。进行中的保存、冲突重读失败、历史归档失败及锁屏后的保存结果待核对保留原有语义。明确采用冲突内容后同步最新服务器正文及版本；保留本地内容仍为未保存并可恢复，采用服务器正文可直接确认为已保存。

两组行为回归均先失败后通过，私有日志为.agent-state/file-save-status/red.log、conflict-red.log及green.log。quick收据20261016T012544475591Z通过，指纹5e27ff418a1b323734bd92e0db7c75f5f893158a7dfc1a3365e78741e9d4b9ed。单测不替代真实Safari验收；完整门禁、候选镜像、运行替换、页面与服务器正文对照仍待完成。本批未升版、打tag或创建Release，原12项目标保持进行。

## 候选与离线包

候选sha256:7cb76e39c35cdb66db5dbb3026ec79917918d50253b1a825fd3b3c5dd1111637，构建f6f5dd703305ba5a4d14f79de81b5234fc1a4b8b，源码摘要ffc98f18ada9ffb2dd3cd4e5e101bf99668d6937825a3b3fc2a509c0f63f63a5。完整包706070441字节，SHA-256 854d392c1481a603243a3f673566d6b057978364edbc3420e16c767f6522084d。内外摘要、应用与依赖标签、启动脚本、许可证以及实际本机docker load通过，原运行镜像保持不变，见[initial bundle evidence](file-save-status-initial-bundle-proof.json)。接收设备导入未验。完整门禁和实际Safari仍待验。

## Post-acknowledgement input race

An additional behavioral regression directly reproduced stale saved status while the server had acknowledged V1 and local saved-draft cleanup was still awaiting persistence. New V2 input must immediately invalidate the saved label during this interval. The fix distinguishes acknowledged cleanup from an unacknowledged in-flight write; the existing in-flight, failure, conflict, lock and partial-history checks remain green. Focused logs: cleanup-race-red.log / cleanup-race-green.log under the private task directory.

The earlier 7cb76e39 candidate and its full snapshot do not cover this additional fix. That isolated verification container was intentionally stopped as superseded to avoid overlapping heavy runs, rather than restarted after an observation timeout. Production was unchanged. A new candidate and full receipt are required.

## Replacement candidate

Image sha256:8d1957c9b0c92bc376e5a20933f3b81abd10400cd0688625f9f97366d06512f9, source SHA e31ef4425069dfeaf86df42bc97fae1f92b0e10495d40885d61e111d7e57b1db, build commit22a15eafd57e7664563123cf6783a38d63af1948. Complete bundle docker-images/aide-0.1.17.0-RC2-linux-arm64-8d1957c9b0c9-offline.tar.gz, 706070195 bytes, SHA-256 af0398cd1e350ff516ae6f5a46fee3f945ac015c6bb4c989b87a4a16e33d74bb. Outer/inner checksums, both image tags, launcher/license consistency and actual local Docker import passed; production remained unchanged. See [replacement bundle evidence](file-save-status-bundle-proof.json) and [source regressions](file-save-status-source-proof.json). Fresh full and native Safari acceptance remain pending.

## Final full and native candidate acceptance

Final full88 passed, all exit0; server race588.399s and all-package vet passed. Receipt20261016T013627048713Z, fingerprinted6649e0c7dd582f91be849b4a6d3991c5cd7ddcf321db06957f2286f27de59b. Runtime asset checks cover19 readable resources and DOCX roundtrip under nonroot/network-none conditions.

After a fresh idle preflight (14 sessions, running/nonterminal/queued all0), designated start.command updated local9999 to8d1957c9. Health, source labels,12 HTTP assets and strict original protected bytes/mounts/workspace configuration passed. This was an authorized local candidate acceptance before main publication.

Native Safari reused the existing tab and kept auto-save off. V1 input was unsaved, manual save confirmed exact API/disk V1; V2 input immediately showed unsaved while independent API/disk stayed V1. Native Command-Z returned V1 and saved status with unchanged server hash. An external owned V3 write caused the expected conflict dialog for localV2; explicit adoption of serverV3 displayed saved status, with identical server hash before/after. SubsequentV4 input was unsaved and native manual save matched the whole API/disk text. See [runtime/native proof](file-save-status-runtime-proof.json).

Closed the owned editor and restored the original root/session. Deleted only the unique one-file fixture after exact final-content checks. Original root listing, settings, sources, authentication and all session bytes are identical to the pre-test snapshot. No new browser tab or model call. Three asynchronous/source regressions are not represented as physical production fault tests. Main gate/push remains to be recorded.
