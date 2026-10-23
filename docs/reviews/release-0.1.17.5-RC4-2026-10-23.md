# Aide 0.1.17.5 RC4 release record — 2026-10-23

## Scope and authorization

The user requested “push release”, selected the main application plus BailianSupporter and AutoAiModelRouter, requested a marketplace update, then supplied the public TTS Tools repository. PR #39 consolidates the Qwen adapters, plugin-first pricing/manual fallback, account balance pool, compact usage chart, native media output routing, TTS Tools host bridge and core-only CI delivery pipeline. It merged into main at 4f6d7c77f71f893397fa972c17587d93bc7a3f94. Existing branch protections were not changed. Earlier dependent reviews #34/#36/#37 were closed without deleting branches.

## Verified application behavior

See [Qwen adapter acceptance](20261023/qwen-adapters.md) and [native media/TTS acceptance](20261023/auto-media-routing.md). Qwen text/thinking, image input, audio input, realtime text and image generation were tested separately; DeepSeek V4.1 Flash image identification/OCR/chart reading also passed the recorded fixtures. This is not a full-model benchmark or a guarantee of account entitlement.

MP3 generation and the AI TTS tool produced decodable audio. Auto video selected Wan2.7, completed one persisted async job and produced a 90-frame 720P H.264 clip. Safari playback passed. No video-to-Python planning fallback was used after the fix. Default assistant read-aloud settings were compared and retained.

## Plugin and marketplace delivery

| Component | Version | ZIP bytes | SHA-256 |
| --- | --- | --- | --- |
| ALiBaiLianSupporter | 0.1.2 | 6140 | b88098def971e2afcb3db042d71e29c3d771f6c4fb742d9307ecb0e66f28ed47 |
| AutoAiModelRouter | 0.1.2 | 7866 | 8418518ccc48fd154d3b579293513bb439ec2d4150ec282ed8fee8ae93ab2060 |
| TTS Tools | 0.1.0 | 2964 | a42b4cbab8aaf9929d307133e202c8eea14c8f4fa460d4259fd04876e350b515 |

All three GitHub prereleases are public, immutable ZIPs and SHA256SUMS were independently checked. Marketplace PR #4 merged at c53328e8932b525613b49766ec043bc1d2e837af; Pages run 39903759480 passed. Live catalog, downloaded package hash and visible TTS Tools details were checked. Each listing states its corresponding RC4 host requirement. Private plugin binaries and credentials were excluded.

## Regression and release gates

Current-source remote Aide CI run 39901597014 passed quick, full Go race/vet, Windows launcher fixtures and gate on b0d3a5c0098f76952c54af27d1f449670abc3823. Merged main has identical implementation inputs. Local full receipt, exact RC4 tag/commit and native release artifacts are recorded below only after completion.

## Recovery and limitations

Existing local preview images and settings backup are retained; session/workspace/settings mounts are preserved. Rollback uses the previous image through the existing launcher slot workflow. Published assets are not replaced; marketplace rollback uses a reviewed listing revert. No full native Windows Docker Desktop launch is claimed; Windows checks are launcher fixtures. Voice cloning, realtime speech-file synthesis, batch synthesis and exhaustive Qwen model validation are outside this acceptance. Media routing weights are heuristic preferences, not measured quality rankings. A locally cancelled remote job may still incur provider cost.

Local full verification: PASS; fingerprint `1f5ff9483021e38c6fdf11fccac570618eabcd85b758565a3a407eeb936f65de`; receipt log `.agent-state/verify-20261023T044451828073Z.log`. All checks exited zero. The release artifact pipeline remains pending at this preflight record.

## RC4 delivery stopped

Run 39905317273 failed before publication: ARM64 exposed an outdated six-file runtime helper inventory gate (the build ships twelve), and AMD64 exceeded the implicit ten-minute complete-package Go test deadline. No RC4 public Release or install assets were published. Issue #42 tracks the gate repair; RC4 tag remains immutable and the corrected release will use the next version.sh patch.
