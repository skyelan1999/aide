# 本地离线 TTS（sherpa-onnx）

> 版本：0.1.11.0 · 任务 #44 · 适用：保密 / 空气隙 / 隐私敏感部署

本地方案让「小秘」朗读完全在本机完成：文本不出内网、不依赖微软 edge-tts、不需要 GPU。
引擎为开源的 [sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx)（Apache-2.0，可嵌入），
Go 后端经子进程调用预编译 CLI，无 cgo。

## 一、引擎优先级（auto 模式）

```
本地 sherpa-onnx(离线,默认)  →  edge-tts(联网,备选)  →  Azure(配 key 时)  →  浏览器 Web Speech(兜底)
```

- **本地 sherpa 已装模型**：直接用，全程离线、零外发。
- **本地未装模型**（无 `/data/tts/*/model.onnx`）：自动跳过，不报错，落到 edge-tts。
- **edge 也不可用**（联网受限 / 被服务端断连）：再降级浏览器合成，并弹 toast 明确告知当前用的是哪个引擎。
- 用户在设置里显式选「本地离线」但未装模型：不静默降级 edge，而是在设置页红字提示运行 `scripts/tts-setup`。

设置页下拉：`自动（本地离线优先）` / `本地离线` / `edge-tts 联网` / `克隆音色（自托管）` / `浏览器合成`。

## 二、架构与体积

- **二进制打进镜像**：`sherpa-onnx-offline-tts`（shared/CPU 预编译）随 Dockerfile 安装到
  `/usr/local/bin/`，连同 onnxruntime 运行库。aarch64 包约 28MB + 运行库 12.6MB，**镜像体积增量约 +40MB**。
- **模型外置 `/data/tts/`**：模型不进镜像（控体积、便于空气隙换源）。每个音色一个子目录。
- **无 GPU 依赖**：CPU 实时率（RTF）远小于 1，现代 ARM/x86 CPU 可实时合成；常驻内存约 100–300MB。
- **输出 WAV**：本地引擎出 16/22kHz WAV（离线无 ffmpeg，不转 MP3），前端经 blob 直接播放。

磁盘约定（`scripts/tts-setup` 自动生成；手动放入时照此）：

```
/data/tts/
  <voice-name>/
    model.onnx          主模型（脚本会把真实 *.onnx 软链成 model.onnx）
    tokens.txt          音素表（必备）
    lexicon.txt         词典（vits-zh-hf 类需要；piper 类缺省）
    number.fst/date.fst/phone.fst   规则 FST（可选，自动收集）
    espeak-ng-data/     piper 类音素数据（存在则启用）
    meta.json           {name, gender, nSpeakers, piper}
```

## 三、安装模型（联网环境）

进入容器或在可联网机执行：

```bash
# 列出可用音色
scripts/tts-setup --list

# 安装默认女声 华严（piper，约 67MB，含 espeak-ng-data）
scripts/tts-setup --model huayan

# 加装男声 文nj（vits，约 119MB）
scripts/tts-setup --model wnj

# 国内网络走镜像
scripts/tts-setup --model huayan --mirror https://hf-mirror.com/...
```

脚本会：下载 tar.bz2 → 打印并校验 SHA256/体积 → 解压到 `/data/tts/<voice>/` →
软链 `model.onnx` → 写 `meta.json`。重启 aide 后设置页即显示「本地离线已就绪」。

### 推荐模型（≥1 男 1 女，自然度远胜浏览器机械音）

| 音色 | 包 | 大小 | 性别 | 类型 | 说明 |
| --- | --- | --- | --- | --- | --- |
| huayan 华严 | `vits-piper-zh_CN-huayan-medium.tar.bz2` | ~67MB | 女 | piper | 自然女声，推荐默认 |
| chaowen 超闻 | `vits-piper-zh_CN-chaowen-medium.tar.bz2` | ~60MB | 男 | piper | piper 男声 |
| wnj 文nj | `vits-zh-hf-fanchen-wnj.tar.bz2` | ~119MB | 男 | vits | 清晰男声 |
| theresa | `vits-zh-hf-theresa.tar.bz2` | ~120MB | 女系 | vits,804 speaker | Genshin 衍生，注意许可 |

模型下载地址基准：`https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/<包名>`。
piper 类自动附带共享 `espeak-ng-data.tar.bz2`（~7MB）。

> SHA256：首跑后脚本会打印实际哈希；建议回填 `scripts/tts-setup` 的 `MODELS` 表以实现可复现安装。
> 模型自身许可见各包内 `LICENSE`/`MODEL_CARD`；Genshin 衍生音色（theresa/eula 等）商用前请自查。

## 四、保密 / 空气隙部署

1. 在联网机上 `scripts/tts-setup --model huayan` 下载，或直接 `wget` 模型 tar.bz2。
2. 把 tar.bz2 拷到内网机器。
3. 校验并就位：
   ```bash
   scripts/tts-setup --verify-only /path/to/vits-piper-zh_CN-huayan-medium.tar.bz2
   # 按提示解压到 /data/tts/<voice-name>/，确保含 model.onnx + tokens.txt
   ```
4. 离线镜像构建：先把两个 sherpa 二进制包（`*-shared-cpu.tar.bz2`、`*-shared-cpu-lib.tar.bz2`）
   放进 `docker/sherpa/`，再 `--build-arg SHERPA_OFFLINE=1`（与 `docker/wheels` 同一套离线约定）。

整个过程不访问微软、不访问第三方合成服务；音频模型与推理都在内网完成。

## 五、与 #36 音色克隆的关系

- 本地 sherpa 是**固定音色**的离线神经音（华严/文nj 等预置音色），零样本、零配置即可用。
- #36 音色克隆是**自托管克隆服务**（IndexTTS2/CosyVoice2/GPT-SoVITS），用于克隆专属声音，需 GPU 与单独部署。
- 二者独立：克隆仅在用户显式选「克隆音色」时启用；auto 链里默认是 `sherpa → edge`，不掺克隆。

## 六、故障排查

- **设置页红字「本地离线模型未安装」**：未放模型。运行 `scripts/tts-setup --model huayan`。
- **`当前引擎: 浏览器合成（机械音）`**：本地与 edge 都不可用，已降级。检查联网或装本地模型。
- **试听按钮一直「播放中…」**：已修——试听有 8s 硬超时 + finally 复位按钮；后端 fetch 也有 20s 客户端超时。
- **首次合成慢**：模型冷加载需几秒（~100MB），属正常；之后同进程内常驻。
- 二进制路径/模型目录可用环境变量覆盖：`SHERPA_BIN`、`SHERPA_TTS_DIR`。
