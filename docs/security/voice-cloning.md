# 声音克隆与个性化音色（#36）

> 状态：**可插拔框架已落地；真实克隆模型效果 NOT_RUN**。
> aide 主镜像不内置 GB 级克隆大模型（GPU 才实用）。本模块交付"可插拔克隆音色框架 + 自托管克隆服务接入"，
> 用 `scripts/fixtures/mocks/mock_clone_server.py` 即可完整跑通"录音→加密→创建音色→合成→性格推断"链路。

样本上传、删除和清空按 profile 串行更新加密文件与索引，其他 profile 独立进行；请求取消后，排队的操作不会修改样本。此保护是单个 Aide 实例内的并发顺序，不支持多个进程同时写同一份样本目录，也不补回历史上已脱离索引的文件。

试听也参与该 profile 的队列，并要求样本仍在索引中，旧的孤立 `.enc` 文件不能仅凭 ID 直接试听。删除时先确认音频已删除或不存在，再保存索引；文件删除失败返回 `500` 并保留其索引。清空遇到部分失败返回 `500`，附实际 `removed` 数、`failed` 数与 `indexSaved`，成功删除的项移出索引，失败项保留，支持再次清空。目录/索引不可读时停止；索引保存失败也返回 `500`，此时已删除的文件不会恢复，重试可清理残留元数据。清空会尝试删除目录内历史孤立的 `.enc` 文件，但不会重建其索引；这不是跨文件事务或备份/外部克隆服务数据的删除保证。

声音样本的试听、上传、创建音色和性格推断在获取密钥时检查服务端权威锁屏状态，锁定时返回 `403`，即便内存仍有先前的 `personaKey`。上传/试听/本地克隆准备在排队后再次检查，克隆与模型调用前也复核；检查不会重复执行 Argon2。锁屏无法撤回此前已返回的音频或已经发出的远端请求；本实现不承诺取消所有已经开始的音频传输。

创建音色最多选择16个不同样本，重复或无效 ID 会被拒绝。GPT-SoVITS 的本地参考音频总量最多32MiB，单个样本仍最多15MiB：先检查全部密文实际文件大小，再有界读取和解密，并复核实际解码总量，之后才发送载荷。准备样本期间与该 profile 的写操作互斥，调用远端服务时释放锁；已开始发送的克隆请求不能由随后删除本地样本撤回。其他后端只发送 ID 时也受数量和去重限制，不增加本地音频上传。

## 设计目标

让小秘（生活人格）拥有用户本人（或授权者）的音色，而不是固定 edge 神经音。
原则：

- **自托管 / 可离线 / CPU 为主**：克隆服务是独立可选容器，不进 aide 主镜像。
- **可插拔**：新增克隆后端只需在 `internal/server/tts/clone.go` 加一个 case。
- **无真实模型可测**：mock 服务端到端通过；真实效果由用户环境验证。
- **aide 主聊天机械 TTS 不受影响**：`auto`/`edge` 链不含 clone，只有用户显式选 `clone` 才走克隆。

## 架构

```
浏览器 MediaRecorder 录音
  → POST /api/voice-sample/upload（原始音频字节）
  → AES-256-GCM 加密（密钥=账户密码 Argon2id 派生）
  → /data/assistant/voice-samples/xiaomi/<id>.enc
  → index.json 记元数据（不含音频）

POST /api/voice-clone/create
  → 解密样本（内存）→ 送自托管克隆服务 POST /voices
  → 克隆服务返回 voice_id
  → 写入 Settings.CloneVoiceID

小秘朗读时（TTSProvider=clone）
  → tts.ChainSynth：clone → edge（降级）
  → cloneProvider.Synth → POST {CloneTTSBaseURL}/audio/speech
  → 流式 mp3 → 前端 <audio> 播放
```

## 后端选型对比

| 后端 | 协议 | 少样本需求 | 中文 | 硬件 | 备注 |
| --- | --- | --- | --- | --- | --- |
| **IndexTTS2** | OpenAI 兼容 `/audio/speech` | ~10s 参考音频 | 极好 | 4-6GB 显存 | **首选推荐**：开箱即用 OpenAI 兼容层，克隆质量高 |
| **CosyVoice2** | OpenAI 兼容 | 3-10s | 极好 | 8GB 显存 | 阿里开源，指令与情感强；与 IndexTTS2 二选一 |
| **GPT-SoVITS** | 自有 HTTP `/tts` | 1min+ 参考 | 好 | 4GB 显存可 CPU 慢推理 | 中文社区活跃；我们已适配其 `/tts` |
| **OpenVoice v2** | REST（预留） | 几秒 | 中 | CPU 可跑 | 风格克隆强、音色相似度一般；暂列扩展位 |

默认 `CloneTTSBackend=openai`（IndexTTS2/CosyVoice2 的 OpenAI 兼容层都走这个）。
GPT-SoVITS 在设置里选 `gpt-sovits`，客户端自动切到 GET `/tts?text=...&text_id=<voiceID>`。

## 自托管离线部署（用户侧）

> 以下由用户在自己的 GPU 机上部署，**不进 aide 主镜像**。

### 硬件

- 推荐：NVIDIA GPU ≥ 6GB 显存（IndexTTS2/CosyVoice2 实时合成）。
- 仅 CPU：GPT-SoVITS 可跑但首包数秒~数十秒；aide 已给 30s 合成超时。
- 离线：需提前下载模型权重并校验 SHA256。

### 快速起一个 OpenAI 兼容克隆服务（示例：IndexTTS2）

```bash
git clone https://github.com/index-tts/index-tts.git
cd index-tts
pip install -r requirements.txt
# 下载模型权重（见其 README），校验哈希后启动：
python -m indextts.server --port 9880
# 验证：
curl -s http://127.0.0.1:9880/healthz
```

然后在 aide 设置里：
- 语音引擎选「克隆音色（自托管）」
- 服务地址填 `http://127.0.0.1:9880`
- 后端选 `openai`
- 录音并「创建音色」→ 自动绑定 voiceID

### docker compose 模板

见 `docker/voice-clone/docker-compose.yml`（可选服务，默认不启动）。
该模板以 IndexTTS2 OpenAI 兼容层为例；替换 image/command 即可切到 CosyVoice2/GPT-SoVITS。

## 样本加密与 GDPR Art.9

声音是生物特征（GDPR Art.9）。本模块的默认姿态：

1. **录制前显著告知 + 勾选**「本人或已获被录制者同意」——未勾选后端拒绝上传（400）。
2. **AES-256-GCM 加密落盘**：音频字节用账户密码经 Argon2id 派生的密钥加密；`.enc` 文件不含明文片段（测试 `TestVoiceSampleEncryptedStoreAndDelete` 验证）。
3. **密钥只在内存**：样本入口强制检查工作台锁屏与小秘解锁状态；旧的内存密钥不能绕过当前锁屏。
4. **数据默认不发第三方云**：只有用户显式点「创建音色」时，样本才送用户自己填的自托管地址。
5. **一键删除**：单条删除 `DELETE /api/voice-sample/{id}`；全部清空 `POST /api/voice-samples/wipe`。成功后移除 `.enc` 与索引；失败按上述规则报告，不能将错误响应当作清空完成。
6. **导出默认不含样本**：配置导出走 `/api/config/export`，不含 `/data/assistant/voice-samples/`。

## 性格推断流程

1. 用户选若干样本 → `POST /api/voice-personality/infer`。
2. 后端解密样本，轻量声学特征：WAV 头解析时长、转写字数/秒（语速）。
3. 特征 + 浏览器 STT 转写交 LLM，输出大五倾向 + 正式度/热情/果断/幽默/情绪稳定 + 提示词草稿。
4. 前端展示画像，用户可编辑草稿，点「采纳为小秘性格」→ `POST /api/voice-personality/adopt`。
5. 采纳复用 #34 演化体系：旧提示词入回滚栈、校验「小秘」身份锚点、写 `settings.Personalities[xiaomi]`。
6. 全程标注「AI 推断仅供参考」；可在性格设置一键恢复默认。

> LLM 不可用时走 fallback：返回保守画像 + 当前提示词草稿，不阻断流程。

## mock 端到端测试

```bash
# 1. 起 mock 克隆服务（:9880）
python3 scripts/fixtures/mocks/mock_clone_server.py --port 9880

# 2. 起 aide（任意测试配置），设置里：
#    语音引擎 = clone，服务地址 = http://127.0.0.1:9880
# 3. UI 上录音 → 上传 → 创建音色 → 试听；从声音推断性格 → 采纳
```

Go 单测（不需要真实克隆服务）：

```bash
go test ./internal/server/ -run 'TestVoiceClone|TestVoiceSample|TestVoicePersonality' -v
go test ./internal/server/tts/ -run TestClone -v
```

## NOT_RUN 项

- **真实克隆模型合成效果**：未在本机跑 IndexTTS2/CosyVoice2/GPT-SoVITS 权重；声音相似度、自然度、首包时延需用户 GPU 环境验证。
- **浏览器 MediaRecorder 真机录音**：单测用构造字节；真机录音/试听需人工在浏览器确认。
- **GPU 硬件要求实测**：上表硬件数字来自各项目 README，未在本仓库实测。
