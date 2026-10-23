# Qwen adapters — local acceptance, 2026-10-23

Fixes #38. Local deployment only; this record does not claim a merged release or marketplace publication.

The affected account uses a workspace endpoint. A legacy public DashScope URL returned 404 from Images; changing only the provider URL to the existing account's Beijing workspace endpoint retained its vault keys and resolved the inference connection. Account funds were not the demonstrated cause.

## Implemented behavior

- Qwen hybrid models use `enable_thinking`; Qwen 3.8 Omni uses `reasoning_effort` and text output. Router adaptation cannot restore another vendor's reasoning fields. Qwen 3.5/3 Omni streaming requirements also apply to internal text-completion calls.
- Modern documented native-vision families accept images. Local WAV/MP3 audio requires non-Realtime Omni. Local MP4 video requires a compatible modern vision family. The existing local-file and 5 MiB bounds apply. Model capability overrides remain authoritative.
- Omni Realtime uses an authenticated WebSocket text/tool bridge. It does not implement microphone/camera capture, duplex voice UI, ASR-only, TTS-only or live-translation protocols.
- Qwen Image 3.0/pro and 2.1 variants use a synchronous single-shot Images or native DashScope request. No tools, title generation, automatic compaction, reviewer calls or automatic retry may produce duplicate images. Image counts are not estimated as text tokens. Reference-image editing is not included.
- Returned provider-owned Alibaba OSS image URLs are fetched without credentials or redirects, bounded to 20 MiB and validated as PNG/JPEG/WebP. Successful downloads are saved under `generated/images/` in the current local workspace and rendered through the existing authenticated raw-file route. Download failure retains the original link and explicitly advises against regenerating for download.
- BailianSupporter 0.1.2 matches Beijing workspace hosts through a constrained declarative region matcher; Singapore and spoofed suffixes are excluded. Unknown models retain manual pricing fallback. AutoAiModelRouter 0.1.1 adds eight Qwen text/vision profiles; weights remain maintainer heuristics. Existing licenses are preserved.

## Evidence

- Targeted Go provider/router/attachment/vision/supporter regressions: PASS (9.512 s), including WebSocket cancellation.
- Quick repository checks: PASS. Application image build including `go vet ./...`: PASS.
- Full server package run: 449.972 s, six failures caused by missing `AIDE_OFFICE_SCRIPTS` in the test container; all six passed on a corrected-environment recheck (5.819 s). This is not reported as a clean single-run full-suite pass.
- Both plugin repositories: 7 Node tests PASS; allowlisted ZIPs built and locally upgraded through Aide's normal upgrade API.
- Actual-account text calls: Qwen 3.8 Flash, 3.7 Plus with thinking enabled, 3.8 Omni Flash, and 3.8 Omni Flash Realtime completed. Each recorded exactly one main request and actual token usage. These representative calls do not establish inference availability for every catalog model.
- Real vision input: 64×64 red PNG answered “红色”. An earlier 8×8 synthetic fixture was rejected by provider dimension constraints; that test was corrected, not treated as an adapter pass.
- Real audio transport: WAV accepted. A pure tone received “无”, so that call alone does not establish audio interpretation accuracy. A locally synthesized spoken test word was then correctly returned as “orange”; the word was absent from all textual request messages. The reply also included an incorrect self-disclaimer about media access; the attachment marker was clarified to state that native media is present. Final live verification on that change returned only “orange”, with one main request and 12,492 actual tokens.
- Real image: Qwen Image 3.0 generated an image, saved it locally and displayed the actual image in Safari. The first successful remote URL output was blocked by the page's same-origin image policy; local persistence fixed display without weakening CSP.
- Raw request logs/screenshots remain local under `.agent-state/`; no account workspace identifier, credentials or signed image URL is included here.

## Configuration and limits

Use the actual workspace's matching-region service URL, for example `https://<workspace-id>.cn-beijing.maas.aliyuncs.com/compatible-mode/v1`. Preserve the corresponding API Key. The current provider catalog lists 178 IDs, including non-chat protocols; listing is not evidence of inference entitlement. Sixteen mainstream models were added to the local selection, reaching the existing 20-model configuration limit together with the four retained entries.

Video, reference-image editing, full Realtime voice UI, every model's tool execution and every catalog entry were not live-validated. No broad automatic fallback or repeated paid verification was used.

Official references: [model list](https://help.aliyun.com/zh/model-studio/model-list-text-generation), [thinking](https://help.aliyun.com/zh/model-studio/deep-thinking), [Omni](https://help.aliyun.com/zh/model-studio/qwen-omni), [Realtime](https://help.aliyun.com/zh/model-studio/realtime), [image API](https://help.aliyun.com/en/model-studio/qwen-image-generation-and-editing-api-reference).
