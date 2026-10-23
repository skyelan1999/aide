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

Final local full verification passed: quick checks, all 1189 registered server tests/fuzz seeds/examples across 24 disjoint race shards, other Go packages, vet, runtime assets and CAD. Receipt fingerprint: 1f5ff9483021e38c6fdf11fccac570618eabcd85b758565a3a407eeb936f65de. Raw log: .agent-state/verify-20261023T044451828073Z.log. Current-source remote CI 39901597014 also passed all four jobs. Plugin contract and generated-player regressions passed separately.

## Final live media acceptance

Source: b0d3a5c0098f76952c54af27d1f449670abc3823, local RC3 preview image aide:media-tts-acceptance-20261023. This preview is not a published RC4 artifact.

- Direct Qwen-Audio-TTS 3.0 flash: completed one native speech generation, no parallel analysis. MP3 decoded as 24 kHz mono, 160 kbit/s, 1.656 seconds.
- AI calling TTS Tools: the coordinator invoked tts_generate once with an explicit short test script. Auto speech scoring selected Qwen-Audio-TTS 3.1 flash (405 versus 305 for configured 3.0 flash). Valid MP3, 24 kHz mono, 160 kbit/s, 1.008 seconds. Coordinator text usage and provider media billing remain separate.
- Auto video: the request phrase included "only for API testing". It selected wan2.7-t2v (503), created one remote job and polled the persisted ID to completion, with no parallel text analysis. Full decode: H.264 High, 1280x720, yuv420p, 30 fps, 90 frames; visual duration 3.00 seconds, MP4 duration 3.02 seconds, AAC stereo. No repeated paid acceptance calls after success.
- Safari: MP3 player showed the actual duration without an error; video played the three-second generated clip. Native controls and download links are available; no autoplay. Local media serving verified HTTP 206, audio/mpeg, matching Content-Range and MP3 ID3 signature. Compact route details no longer overflow horizontally.
- Assistant TTS settings, clone fields and voice-reply settings were compared before and after installation and remain unchanged.
- The first API-test wording incorrectly entered text planning; that owned test was stopped and archived, and exact wording is now a regression case. The user's earlier stopped video task was not resumed or modified.
- User subsequently supplied skyelan1999/aide-pkg-TTSTools as the target repository. Its empty public state was verified, the MIT source was pushed and v0.1.0 published with independently downloaded checksum verification. Marketplace PR #4 adds its immutable public package; it requires RC4 or an equivalent host bridge.

Private raw records and screenshots are retained under .agent-state; provider workspace IDs, access tokens, signed storage URLs and private session content are excluded from this public review.

Official protocol sources:
- https://help.aliyun.com/zh/model-studio/qwen-audio-tts-http-api
- https://help.aliyun.com/en/model-studio/text-to-video-api-reference
- https://help.aliyun.com/en/model-studio/qwen-image-generation-and-editing-api-reference
