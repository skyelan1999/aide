# 声音克隆与个性化音色（#36）

> 状态：**可插拔框架已落地；真实克隆模型效果 NOT_RUN**。
> aide 主镜像不内置 GB 级克隆大模型（GPU 才实用）。本模块交付"可插拔克隆音色框架 + 自托管克隆服务接入"，
> 用 `scripts/mock_clone_server.py` 即可完整跑通"录音→加密→创建音色→合成→性格推断"链路。

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
3. **密钥只在内存**：锁屏/锁定小秘后 `personaKey` 清空，样本不可解密。
4. **数据默认不发第三方云**：只有用户显式点「创建音色」时，样本才送用户自己填的自托管地址。
5. **一键删除**：单条删除 `DELETE /api/voice-sample/{id}`；全部清空 `POST /api/voice-samples/wipe`，`.enc` 无残留（测试 `TestVoiceSamplesWipe`）。
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
python3 scripts/mock_clone_server.py --port 9880

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
