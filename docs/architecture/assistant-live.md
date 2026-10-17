# Page-owned work coordinator

The assistant is a work coordinator first: understand goals and dependencies, delegate to the existing Aide executor, follow task progress and blockers, then report results and next steps. Friendly conversation belongs in idle time. User personality settings are preserved and affect tone rather than changing this responsibility.

After identity verification, the page automatically starts the heartbeat; there is no companion enable, pause or end switch. Existing assigned work continues through the executor and its approval controls. This is automatic follow-up of assigned work, not an unbounded loop that invents new jobs. The microphone starts only through its own user gesture, and turning it off does not disable coordination. Switching work sessions keeps the page monitor alive. Hiding the page releases microphone capture and stops audio, while monitoring continues subject to browser timer throttling. Locking or closing the page ends monitoring. Closing the browser does not create a background service. Authentication failures stop checks until reauthentication; transient network failures retry with a slower cadence.

The read-only assistant-watch heartbeat checks followed task states, due reminders in scope and unread-message metadata only when assistant access is granted. The ledger orders blockers and approvals first, then completed results, then active work. Completed previews include only user-facing chat content, never reasoning or tool arguments. Initial blockers are visible immediately; unchanged checks and routine running/queued transitions stay quiet. It neither marks messages read nor accepts invitations/files nor approves commands. The existing agent/tool loop handles actual user-assigned work.

The design borrows heartbeat/quiet-output and queue coordination principles from https://docs.openclaw.ai/gateway/heartbeat and https://docs.openclaw.ai/cron-vs-heartbeat ; it does not embed OpenClaw or add a 24-hour external scheduler. No model requests run on empty heartbeat checks.

Voice uses browser ASR and the configured TTS. New interim speech cancels synthesis/playback; exact echo-like reply fragments are suppressed. Actual acoustic echo and latency depend on browser and hardware. Stop, lock and page close invalidate late results.

## 阿里云百炼千问 TTS

Qwen uses the native DashScope HTTP synthesis API and downloads signed WAV results over HTTPS without forwarding the API key. Only official result OSS hosts are accepted; redirects are disabled. Failure displays a safe stage/status/error code. Qwen keeps text and does not switch voices automatically unless the user opts into browser fallback.

This is a non-realtime text synthesis adapter. Realtime model IDs are not compatible. Qwen playback speed uses media playbackRate with pitch preservation; model expressiveness is hidden. Actual cloud voice quality, latency and microphone echo require target-device verification.


## Catalog discovery and playback reliability (2026-10-17)

GET /api/tts/catalog returns the verified bundled catalog. POST reads the native DashScope /api/v1/models endpoint using the selected region and TTS key, and decodes the official help article JSON/HTML for preset voices. It does not invent an account preset-voice API or enumerate private cloned voices. Failed refreshes preserve bundled choices and display source/warnings. Model discovery does not prove account synthesis access; the selector filters realtime and Qwen-Audio-TTS protocols incompatible with this adapter. Existing saved values absent from the catalog are explicitly labeled rather than overwritten.

Qwen text is grouped into at most 280 Unicode characters per request, avoiding one request per comma and preserving long sentences. Cloud request/download timeouts are 45 seconds and the frontend timeout covers the full audio fetch for 60 seconds. Synthesis POST is not automatically retried because it may have incurred billing. Safe error stages and status codes are shown without raw provider bodies, keys or signed URLs. User interruption/cancellation invalidates playback generations and aborts fetches without triggering fallback. Opt-in fallback resumes from unfinished chunks instead of replaying completed chunks.

Sources: https://help.aliyun.com/zh/model-studio/list-models and https://help.aliyun.com/zh/model-studio/qwen-tts-voice-list . Actual read-only model discovery succeeded with the configured provider; the public article JSON parsed 48 non-realtime preset IDs. These checks do not call paid synthesis.

Configuration: Settings > Voice assistant > Speech engine > Alibaba Cloud Model Studio / Qwen TTS. Choose region, model and voice, enter a dedicated TTS key, save and preview. Keys are not echoed; blank preserves the existing key, and clearing requires an explicit checkbox.

## Safari playback lifecycle (2026-10-17)

The same media element is activated synchronously on microphone/companion/preview gestures, then reused after asynchronous cloud synthesis. Safari autoplay grants are per element (https://webkit.org/blog/7734/auto-play-policy-changes-for-macos/). A NotAllowedError keeps the current audio URL and shows a Play voice reply button; that user gesture retries playback without another synthesis request. Normal playback state stays internal. There is no permanent playback/status toolbar or duplicate speech-reply toggle; the saved Voice reply setting is authoritative. Browser-blocked playback alone shows a temporary action notification, and failures use the existing error notification/settings status. Completion means a media ended event, not proof that a speaker produced audible sound. Stop/interruption resolves pending playback, clears retries and releases object URLs immediately. Failed starts time out with a visible error. No browser autoplay/security setting is changed.

The historical 200 WAV synthesis test did not validate audible Safari playback. Actual microphone barge-in, acoustic echo rejection and output device routing still require target hardware acceptance.

Text messages submitted from the assistant composer must refresh the session before speech begins: selectSession cancels earlier playback. Companion mode routes these replies into the same interruption-aware queue rather than suppressing them. Paused/off sessions do not enqueue new speech; silent/ignore replies remain silent. The media lifecycle test executes this response-routing block with a cancelling refresh in both modes.

## Delegated execution and approval (2026-10-17)

Both dispatch routes enter the validated task-start pipeline. `push_to_session` starts a real run or queues its prompt behind a running task, and returns a failure if starting is rejected; appending a chat message alone is not success. A generated prompt cannot answer a pending human clarification or approval channel.

Settings > Voice assistant > Assistant approval contains “允许助理代审交办任务”. It defaults off and is independent of the workbench global approval mode. Only server-marked assistant-delegated runs and their child tasks receive this optional independent reviewer. Public run payloads cannot claim that origin. Disabling the option invalidates in-flight review generations and returns to the existing workbench policy.

The reviewer checks the original human instruction, never treating an assistant-generated task summary as additional authorization. Existing operation fingerprints, sandbox constraints, audit reasons, protected-operation checks and manual-review requirements remain. Existing-file overwrites, uncertain authorization, business clarifications, fingerprint verification and peer invitations remain manual. Settings changes alone do not enable the option on the user's behalf.

Delegated prompt, follow-up and queue bubbles show “助理代发”; approval audit entries identify “助理代审”. Origin metadata is saved with the run/message and survives reload. Historical messages without metadata are not retroactively guessed to be assistant-created.

## Document and explicitly selected reference proposals (2026-10-17)

New text document deliverables use `write_file(source="system-docs", path="relative/name.md", content=...)`. The destination follows the automatic-document directory configured in Workspace; no Downloads or other absolute path is hardcoded. Office/workflow document generation already follows the same binding. Existing code edits and explicitly requested workspace files keep the legacy `write_file` workspace destination when source is omitted.

When the user explicitly chooses a reference destination, `source` is its ID from `list_sources`. The source must be enabled, visible to AI in the current workspace, and RW. Read-only/context references and non-file link/MCP/FTP/SMB sources cannot become write destinations. Local/skill, workspace SFTP and independent SFTP file sources use their registered roots. The assistant never toggles the locks or falls back to another destination on denial.

The server records source ID, binding fingerprint and actual target path on the proposal. Approval fingerprints and the file UI include that destination; identical relative names in workspace and references remain separate. Existing-file modifications require the attached original and version match. Changing a binding, hiding/locking/removing the reference, or switching workspaces rejects pending writes. Local directory handles and remote connection generations are pinned during application. Partial success is recorded per file. Reads of pending new documents remain pending-proposal notices; local approval receipts read back the bound source, not the workspace. Remote writes keep their normal transfer result boundary and are not described as locally verified bytes.

New files in explicitly configured local bindings can use the independent reviewer; original replacements and remote proposals remain manual. An empty automatic-document binding fails clearly instead of silently using the read-only `/context` fallback. Historical workspace proposals without source metadata keep their original target.
