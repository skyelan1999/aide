# Features and operating boundaries

[简体中文](../features.md) · [English documentation](README.md)

This describes public Aide source capabilities. A feature's presence in source, a visible control, an executed test, and a published package are separate facts. Historical screenshots retain their original dates and do not certify this release.

| Area | What to use | Boundaries |
| --- | --- | --- |
| Conversations and workflows | Choose mode, strategy, model and approval policy beside the input. Queue or interrupt work while it runs. | Context budgets, enabled tools and approvals still govern execution. |
| Tasks and proposals | Inspect stage output, tool records, status and trajectory. | A proposed file is not a saved file. Verify the application receipt and destination. |
| Usage and compaction | View cumulative session usage separately from the latest request's context window. | Parallel requests have separate windows. Missing upstream usage is unknown; estimates are labeled. Compaction does not promise smaller session files. |
| Files | Browse workspace/reference sources; edit Markdown and text; use installed viewers for Office and CAD. | Preview, generation, export and successful reopening require separate validation. |
| Knowledge map | Explore files, conversations, source code and document excerpts in a separate page. | Index budgets and source visibility apply. Static relationships do not prove runtime behavior. Keyword retrieval and optional vector retrieval are separate. |
| Models | Configure providers, named credentials, compatible model catalogs and routing profiles. | Provider protocols, capability support, quotas and availability differ. Auto selection alone does not prove an image/audio/video was produced. |
| Plugins | Inspect, install, enable, disable and update trusted extensions. | Node plugins execute trusted code. They are not an OS sandbox. |
| Assistant | Assign tasks, follow progress, configure reminders and use authorized message access. | Microphone capture requires a user action. Locking or leaving the relevant page ends listening. Management actions have independent permissions. |
| Messages | Chats, contacts, groups, calls, file transfers and reminders. | Invitations require recipient consent. Short lookup codes do not replace full identity fingerprints. |

Automatic system documents use the workspace's configured binding. For explicit document proposals, `write_file(source="system-docs", path="relative/path")` targets that binding. Ordinary references must be enabled, visible to AI and writable. Read-only `/context` does not become writable because a tool calls it a document directory. Unconfigured document generation retains its documented cache fallback; project memory remains in the project cache.

AI management settings contain four independent flags for Aide/assistant × plugins/references. They default off and govern management, not ordinary enabled tool calls or file access. Management proposals require human confirmation even when task-review delegation is enabled.

Assistant read-aloud, native realtime calls and downloadable speech files are separate capabilities. Qwen/Bailian synthesis has its own region/model/key/voice settings and browser-fallback policy. Native realtime audio is described in [Voice calls](assistant-voice-call.md). Downloadable MP3/WAV generation belongs to an installed TTS Tools plugin.

Messages reuse the configured account name. Contacts, groups, voice calls and file transfers have separate acceptance flows. Assistant message access defaults off and does not automatically accept invitations. LAN discovery and Internet relay/bootstrap configuration have different reachability constraints.

Appearance includes separate palette and light/dark/system preferences, reduced motion, and the lightweight Studio background. Local media remain in this browser. Hidden pages, locking and reduced-motion settings pause relevant effects.
