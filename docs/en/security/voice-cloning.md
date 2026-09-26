# Voice Cloning & Personalized Voice (#36)

> Status: **pluggable framework landed; real clone-model quality NOT_RUN**.
> aide's main image does not bundle GB-scale clone models (GPU-only in practice). This module
> delivers a *pluggable cloning framework + self-hosted clone-service integration*.
> `scripts/mock_clone_server.py` lets you exercise the full upload→encrypt→create→synthesize→personality loop
> without any real model.

## Goals

Let 小秘 (the life persona) speak in the user's (or an authorized person's) timbre instead of a fixed edge voice.
Principles:

- **Self-hosted / offline-capable / CPU-first**: the clone service is an optional container, not part of the main image.
- **Pluggable**: adding a new backend is one `case` in `internal/server/tts/clone.go`.
- **Testable without a real model**: mock server covers the end-to-end path; real fidelity is verified by the user.
- **aide main-chat mechanical TTS is unaffected**: the `auto`/`edge` chain never includes clone; clone only activates when the user explicitly selects it.

## Architecture

```
Browser MediaRecorder
  → POST /api/voice-sample/upload (raw audio bytes)
  → AES-256-GCM encrypt (key derived from account password via Argon2id)
  → /data/assistant/voice-samples/xiaomi/<id>.enc
  → index.json holds metadata only (no audio)

POST /api/voice-clone/create
  → decrypt samples in memory → POST /voices on the self-hosted clone service
  → service returns voice_id
  → persisted to Settings.CloneVoiceID

小秘 speaks (TTSProvider=clone)
  → tts.ChainSynth: clone → edge (fallback)
  → cloneProvider.Synth → POST {CloneTTSBaseURL}/audio/speech
  → streaming mp3 → browser <audio>
```

## Backend Selection

| Backend | Protocol | Few-shot data | Chinese | Hardware | Notes |
| --- | --- | --- | --- | --- | --- |
| **IndexTTS2** | OpenAI-compatible `/audio/speech` | ~10s ref | excellent | 4–6GB VRAM | **Recommended**: OpenAI-compatible out of the box, high fidelity |
| **CosyVoice2** | OpenAI-compatible | 3–10s | excellent | 8GB VRAM | Alibaba open-source; strong instruction/emotion |
| **GPT-SoVITS** | native HTTP `/tts` | 1min+ ref | good | 4GB VRAM; slow on CPU | Active CN community; we adapt its `/tts` |
| **OpenVoice v2** | REST (reserved) | seconds | medium | CPU-runnable | Style cloning ok, timbre similarity meh; placeholder |

Default `CloneTTSBackend=openai` (both IndexTTS2 and CosyVoice2 expose an OpenAI-compatible layer).
Pick `gpt-sovits` in settings to switch to `GET /tts?text=...&text_id=<voiceID>`.

## Self-Hosted Offline Deployment (user side)

> Deploy on your own GPU box. **Not bundled in the aide image.**

### Hardware

- Recommended: NVIDIA GPU ≥ 6GB VRAM for real-time IndexTTS2/CosyVoice2 synthesis.
- CPU-only: GPT-SoVITS runs but first packet takes seconds to tens of seconds; aide allows 30s.
- Offline: download weights ahead of time and verify SHA256.

### Quick start (OpenAI-compatible, IndexTTS2 example)

```bash
git clone https://github.com/index-tts/index-tts.git
cd index-tts && pip install -r requirements.txt
# download weights per README, verify hashes, then:
python -m indextts.server --port 9880
curl -s http://127.0.0.1:9880/healthz
```

In aide settings:
- TTS engine → "克隆音色（自托管）" / "Cloned voice (self-hosted)"
- Base URL → `http://127.0.0.1:9880`
- Backend → `openai`
- Record samples → "Create voice" → voiceID auto-bound

### docker compose

See `docker/voice-clone/docker-compose.yml` (optional service, **not started by default**).
The template wires an IndexTTS2 OpenAI-compatible container; swap image/command for CosyVoice2/GPT-SoVITS.

## Sample Encryption & GDPR Art.9

Voice is biometric data (GDPR Art.9). Default posture:

1. **Prominent notice + explicit consent checkbox before recording** — backend rejects upload (400) if consent is not given.
2. **AES-256-GCM at rest**: audio bytes encrypted with a key derived from the account password via Argon2id; `.enc` files contain no plaintext fragments (verified by `TestVoiceSampleEncryptedStoreAndDelete`).
3. **Key in memory only**: after lock, `personaKey` is cleared and samples cannot be decrypted.
4. **No third-party cloud by default**: samples leave the machine only when the user explicitly clicks "Create voice" against their own self-hosted URL.
5. **One-click deletion**: per-sample `DELETE /api/voice-sample/{id}`; wipe all `POST /api/voice-samples/wipe`; no `.enc` residue (`TestVoiceSamplesWipe`).
6. **Exclusions from export**: config export at `/api/config/export` does not include `/data/assistant/voice-samples/`.

## Voice→Personality Flow

1. User picks samples → `POST /api/voice-personality/infer`.
2. Backend decrypts samples, computes lightweight acoustic features: WAV-header duration, chars/sec from transcript.
3. Features + browser STT transcript go to the LLM, returning Big-Five leans + formality/warmth/decisiveness/humor/stability + a prompt draft.
4. UI shows the profile; the user edits the draft and clicks "Adopt as 小秘 personality" → `POST /api/voice-personality/adopt`.
5. Adoption reuses the #34 evolution system: old prompt is pushed to the rollback stack, the "小秘" identity anchor is checked, and `settings.Personalities[xiaomi]` is updated.
6. Everything is labeled "AI inference is advisory"; the personality settings can reset to defaults.

> If the LLM is unavailable, a conservative fallback profile + the current prompt draft is returned without blocking.

## Mock End-to-End

```bash
# 1. Start the mock clone service (:9880)
python3 scripts/mock_clone_server.py --port 9880

# 2. In aide settings: TTS engine=clone, Base URL=http://127.0.0.1:9880
# 3. Record → upload → create voice → preview; infer personality → adopt
```

Go unit tests (no real clone service needed):

```bash
go test ./internal/server/ -run 'TestVoiceClone|TestVoiceSample|TestVoicePersonality' -v
go test ./internal/server/tts/ -run TestClone -v
```

## NOT_RUN

- **Real clone-model fidelity**: IndexTTS2/CosyVoice2/GPT-SoVITS weights were not run here; similarity, naturalness, and first-packet latency need the user's GPU.
- **Real-browser MediaRecorder**: unit tests use synthetic bytes; real recording/playback needs manual browser verification.
- **GPU hardware claims**: the table numbers come from each project's README and were not measured in this repo.
