# Auto media routing and TTS Tools — 2026-10-23

Refs #40. Previous Auto catalog contained only text task-role scores. The reported repeated text planning was reproduced by inspecting the task routes; new media output ratings and native dispatch address that gap.

## Behavior and boundaries

- `output` profiles independently rate image/video/speech generation. Exact configured model, supported host adapter, provider endpoint and vault key are required. Input vision is not output generation. Scores and candidate reasons are shown, with the explicit warning that weights are maintainer heuristics, not a measured cross-vendor ranking or an entitlement guarantee.
- Explicit generation requests bypass parallel text analysis. A missing eligible generator produces an actionable error. Coding/analysis/tutorial requests retain text routing.
- Qwen images reuse the existing single-shot adapter. Wan2.7 text-to-video uses one async create, persistent task ID and 15-second status polling, 720P, 2–15 seconds (default 3). No automatic recreation after ambiguous submit, cancellation or failed lookup. Local cancellation stops polling; a previously accepted remote task may still run and incur provider charges. Resume queries its original ID, provided model/provider/key identity is unchanged.
- Independent **TTS Tools** exposes `tts_voices` and `tts_generate`. It does not read or mutate assistant default TTS provider/voice/key. The host uses configured Qwen-Audio-TTS 3.0/3.1 flash/plus model-provider vault credentials. Explicit model selection works without the router; automatic selection requires the updated speech catalog. MP3/WAV requests preserve actual encoding, default MP3, 600-character bound. Quoted/colon-delimited text is required for direct artifact requests; tool input contains the exact script. No cloning/realtime microphone/batch generation.
- Audio/video downloads use trusted provider object storage, no forwarded API credentials or redirects, codec checks and bounded sizes (20/100 MiB). Writes pin the original local workspace under writer locks. Generated files have controlled local players and download links. read-only forbids file generation. One script/voice/format per run; duplicate or changed repeat calls do not create another paid request.
- Provider-returned speech token/character counts are retained in the media run record; character/video billing is not invented as text-token estimates. Existing assistant TTS behavior is unchanged.
- Model configuration limit expanded from 20 to 64 so text and media models can coexist.

## Verification status

Initial protocol/routing/cancellation tests: PASS (server focused run 4.221 s). This preceded the later TTS Tools bridge and codec/player integration; the final full regression is running and must replace this partial evidence. Plugin Node tests and generated player path/download controls regression pass. Native image generation had previous actual-account/Safari acceptance. New MP3/video provider entitlement and final browser checks remain pending, not claimed passed here.

Local source-only TTS Tools package is separate from the two already-authorized public plugin releases. No new public TTS repository has been created by this task.

Official protocol sources:
- https://help.aliyun.com/zh/model-studio/qwen-audio-tts-http-api
- https://help.aliyun.com/en/model-studio/text-to-video-api-reference
- https://help.aliyun.com/en/model-studio/qwen-image-generation-and-editing-api-reference
