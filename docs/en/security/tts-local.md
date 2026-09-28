# Local Offline TTS (sherpa-onnx)

> Version: 0.1.11.0 · Task #44 · For: air-gapped / privacy-sensitive deployments

Local offline TTS lets the assistant speak entirely on-device: text never leaves the network,
no Microsoft edge-tts, no GPU required. The engine is open-source
[sherpa-onnx](https://github.com/k2-fsa/sherpa-onnx) (Apache-2.0, embeddable); the Go backend
shells out to a prebuilt CLI with no cgo.

## 1. Engine priority (auto mode)

```
local sherpa-onnx(offline,default) → edge-tts(online,fallback) → Azure(if key) → browser Web Speech(last resort)
```

- **Local sherpa model installed**: used directly, fully offline, zero outbound traffic.
- **No local model** (no `/data/tts/*/model.onnx`): skipped silently, falls through to edge-tts.
- **edge also unavailable** (no network / server-side close): degrades to browser speech and toasts
  which engine is actually in use (no silent downgrade).
- User explicitly picks "Local offline" but no model installed: does NOT silently fall back to edge;
  shows a red hint in Settings to run `scripts/tts-setup`.

Settings dropdown: `Auto (local-first)` / `Local offline` / `edge-tts online` / `Voice clone (self-hosted)` / `Browser`.

## 2. Architecture & image size

- **Binary baked into image**: `sherpa-onnx-offline-tts` (shared/CPU prebuilt) installed to
  `/usr/local/bin/` along with the onnxruntime runtime libs. aarch64 package ~28MB + libs ~12.6MB,
  **image size delta ≈ +40MB**.
- **Models externalized to `/data/tts/`**: models are NOT in the image (keeps it small, easy air-gap swap).
  One subdirectory per voice.
- **No GPU**: real-time factor well below 1 on modern ARM/x86 CPU; resident memory ~100–300MB.
- **Output WAV**: local engine emits 16/22kHz WAV (no ffmpeg offline, no MP3 transcoding); the
  frontend plays it via a blob URL.

On-disk layout (produced by `scripts/tts-setup`; follow it when placing files manually):

```
/data/tts/
  <voice-name>/
    model.onnx          main model (script symlinks the real *.onnx to model.onnx)
    tokens.txt          phoneme table (required)
    lexicon.txt         lexicon (vits-zh-hf needs it; piper omits)
    number.fst/date.fst/phone.fst   rule FSTs (optional, auto-collected)
    espeak-ng-data/     piper phoneme data (used when present)
    meta.json           {name, gender, nSpeakers, piper}
```

## 3. Install models (online environment)

Inside the container or on a networked machine:

```bash
# list available voices
scripts/tts-setup --list

# install default female voice huayan (piper, ~67MB, includes espeak-ng-data)
scripts/tts-setup --model huayan

# add male voice wnj (vits, ~119MB)
scripts/tts-setup --model wnj

# via a mirror
scripts/tts-setup --model huayan --mirror https://hf-mirror.com/...
```

The script: downloads the tarball → prints & verifies SHA256/size → extracts to `/data/tts/<voice>/` →
symlinks `model.onnx` → writes `meta.json`. Restart aide; Settings then shows "local offline ready".

### Recommended models (≥1 male + 1 female, far more natural than browser voice)

| Voice | Package | Size | Gender | Type | Notes |
| --- | --- | --- | --- | --- | --- |
| huayan | `vits-piper-zh_CN-huayan-medium.tar.bz2` | ~67MB | F | piper | natural female, recommended default |
| chaowen | `vits-piper-zh_CN-chaowen-medium.tar.bz2` | ~60MB | M | piper | piper male |
| wnj | `vits-zh-hf-fanchen-wnj.tar.bz2` | ~119MB | M | vits | clear male |
| theresa | `vits-zh-hf-theresa.tar.bz2` | ~120MB | F-type | vits,804 spk | Genshin-derived; check license |

Base URL: `https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/<package>`.
Piper packages auto-include the shared `espeak-ng-data.tar.bz2` (~7MB).

> SHA256: the script prints the actual hash on first run; pin it back into the `MODELS` table of
> `scripts/tts-setup` for reproducible installs. Each model's own license is in its
> `LICENSE`/`MODEL_CARD`; for Genshin-derived voices (theresa/eula), review rights before commercial use.

## 4. Air-gapped / offline deployment

1. On a networked machine run `scripts/tts-setup --model huayan`, or `wget` the model tarball.
2. Copy the tarball to the air-gapped host.
3. Verify & place:
   ```bash
   scripts/tts-setup --verify-only /path/to/vits-piper-zh_CN-huayan-medium.tar.bz2
   # extract to /data/tts/<voice-name>/ per the hint; ensure model.onnx + tokens.txt exist
   ```
4. Offline image build: drop the two sherpa binary packages (`*-shared-cpu.tar.bz2`,
   `*-shared-cpu-lib.tar.bz2`) into `docker/sherpa/`, then build with
   `--build-arg SHERPA_OFFLINE=1` (same offline convention as `docker/wheels`).

No Microsoft or third-party synthesis service is contacted; models and inference stay on the intranet.

## 5. Relationship to #36 voice cloning

- Local sherpa is **fixed-preset** offline neural voices (huayan/wnj etc.): zero-shot, zero-config.
- #36 voice cloning is a **self-hosted cloning service** (IndexTTS2/CosyVoice2/GPT-SoVITS) for a
  custom cloned voice; needs a GPU and its own deployment.
- They are independent: cloning only activates when the user explicitly picks "Voice clone"; the auto
  chain is `sherpa → edge` and does not include cloning.

## 6. Troubleshooting

- **Red "local offline model not installed" in Settings**: no model present. Run `scripts/tts-setup --model huayan`.
- **"Current engine: browser (mechanical)"**: local and edge both unavailable; check network or install a local model.
- **Preview stuck at "Playing…"**: fixed — the preview has an 8s hard timeout + button reset in `finally`;
  the backend fetch also has a 20s client-side timeout.
- **First synthesis is slow**: cold model load takes a few seconds (~100MB); it then stays resident.
- Override paths via env: `SHERPA_BIN`, `SHERPA_TTS_DIR`.
