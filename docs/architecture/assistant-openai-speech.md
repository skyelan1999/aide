# 助理 OpenAI 朗读

本功能让助理回复、导览与已有朗读队列选择 OpenAI 语音，保留千问和自动／本地／Edge／浏览器引擎。实现分支为 `feat/assistant-openai-speech`，基线为 `0e1b02fa`；本页不代表已发布或已部署。

## 设置与使用

在「设置 → 助理与语音 → 语音引擎」选择 **OpenAI 语音**，填写独立的语音 API Key、API Base URL、接口、模型与音色，保存后试听。默认基础地址为 `https://api.openai.com/v1`，不会从聊天模型或千问中读取密钥。密钥输入留空保留，勾选清除仅删除 OpenAI 语音密钥；切换引擎保留千问的模型、地域、音色和密钥。

| 接口 | 内置模型示例 | 传输／输出 |
| --- | --- | --- |
| Realtime 朗读（默认） | `gpt-realtime-2.1-mini`、`gpt-realtime-2.1` | 服务端 WebSocket `/v1/realtime`，收集完整 24 kHz、单声道 PCM16 后封装 WAV |
| Speech API（旧版） | `gpt-4o-mini-tts`、`tts-1`、`tts-1-hd` | 服务端 HTTP `/v1/audio/speech`，返回 MP3 |

模型可手填，必须与所选接口匹配，目录不是账户权限证明。旧 `tts-1`／`tts-1-hd` 显示其六个传统音色；其他 Speech 模型显示较完整的音色目录。Realtime 使用单次无历史朗读请求，不向远端发送工作区、聊天记录、工具或麦克风音频；提示模型按原文朗读。生成式语音仍可能改变读法，需实际听音验收，不能视为确定性逐字 TTS。

按 280 个 Unicode 字符分段，复用原播放队列、暂停、停止、浏览器播放授权及取消清理逻辑。语速在播放端调整一次并保持音调。OpenAI 显式选择失败时保留文字并显示错误，只有勾选「失败时使用浏览器语音」才切换浏览器声音。此回退偏好沿用现有全局 `ttsBrowserFallback`，并非每个提供商独立保存。

## 接口与数据

- `PUT /api/settings`：`ttsProvider: "openai"`，新增 `openaiTTSMode`、`openaiTTSModel`、`openaiTTSVoice`、`openaiTTSBaseURL`、`openaiTTSAPIKey`、`clearOpenAITTSKey`。未传字段保留；接口模式、URL 中凭据／查询／片段在写入前校验。
- `GET /api/config`：仅回显非敏感配置与 `hasOpenAITTSKey`。非敏感备份剔除密钥；普通导入保留本机密钥，仅显式导入含密钥备份才恢复备份密钥。持久化沿用现有 TTS 设置机制及配置文件权限，不宣称迁入聊天模型的加密 vault。
- `POST /api/tts/synthesize`：沿用 `text/voice/provider` 请求；浏览器不携带第三方密钥，服务端按 OpenAI 专属配置发起调用。WAV／MP3 的 MIME 与实际接口相符。
- HTTP 不跟随重定向，不回传上游错误正文或带密钥的请求信息。Realtime 在取消／超时后关闭连接；不完整、错误、空或无效音频整体失败，不播放成功前的部分结果。单请求 60 秒，Realtime 音频累计最多 16 MiB，单事件最多 2 MiB。建连时的临时 EOF、连接重置／拒绝或超时最多重试一次（间隔 200 ms）；HTTP 拒绝、证书错误、取消及已建立会话后的失败不重试，不重发可能已计费的生成。错误仅显示固定分类，不暴露代理地址或原始连接错误。

打开 OpenAI 设置不请求账户或产生合成费用；保存、切换也不调用语音 API。只有实际朗读／试听发送文本并可能计费。可配置兼容端点，凭据与朗读文本会发送到用户填写的地址。

## 验证边界

本轮完成本地 HTTP/WebSocket 模拟服务的协议和取消测试、设置／导入导出／落盘测试、Go race 与 vet、既有千问／播放队列回归；浏览器验证使用真实产品渲染函数和 CSS，API 与播放为模拟，覆盖中英文和桌面／窄屏四格。证据见[任务记录](../tasks/assistant-openai-speech-20261102.json)。

2026-11-02 后续实测中，用户在独立测试实例自行填写了语音密钥。真实 Realtime 请求已返回有效 24 kHz WAV（129,644 字节、2.7 秒音频，单次请求耗时 3,212 ms）；Chromium 设置页另有 2.9 秒音频的 playing／ended 记录。Docker 测试网络同时发生连续 TLS 握手 EOF，不能将一次成功当作稳定性验收。早期浏览器 response.body 采集记录的零字节与实际播放矛盾，未作为音频有效性证据。后续范围和状态见[真实语音测试记录](../tasks/assistant-openai-speech-live-20261102.json)。主观音质、逐字准确性和实际 Safari 播放仍待验收。

本轮不改实时双向语音通话链路、语音识别或独立 `tts-tools` 文件生成插件。生产服务保持原版本。

## 官方接口依据

核对日期：2026-11-02。[文字转语音](https://developers.openai.com/api/docs/guides/text-to-speech)说明旧 TTS 模型将于 2027-01-06 停用，新的 Realtime 模型不能发往旧 Speech API。[Realtime 会话](https://developers.openai.com/api/docs/guides/realtime-conversations)描述 GA 事件、音频格式和无历史独立响应；[停用公告](https://developers.openai.com/api/docs/deprecations)为迁移时间依据。此处不是模型永久可用性承诺。
