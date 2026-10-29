# MIXSoul assistant memory

MIXSoul is an optional built-in assistant engine. Set `AIDE_ASSISTANT_ENGINE=mixsoul` in the server environment before starting Aide; restart to change engines. The default remains `builtin`. Node.js 18 or newer must be available in the server runtime. Plugins cannot select or replace this engine. This is a source integration, not a claim of human cognition or a benchmark of model quality.

## Data and inference

The host binds each call to the unlocked assistant, workspace identity, data root and workspace revision. Memory uses a per-workspace key from the unlocked vault and encrypted files under the server data directory's `assistant/mixsoul` tree. Locking or changing workspaces cancels in-flight work. Preserve the vault and encrypted files together when backing up. Do not upload either to a repository.

User statements, assistant replies, proactive utterances and emotion assessments retain distinct provenance. Assistant statements provide conversational evidence; they are not proof of user facts. Inferred emotions remain tentative and time-bound. Retrieval filters by topic and provenance before using weighted evidence. Confidence, importance, reinforcement, decay, expiry, suppression and corrections affect eligibility. Idle maintenance uses timestamps without invoking a language model; this does not establish subjective time awareness.

Native calls associate segmented utterances and reviewed assistant speech with the current turn. Unreviewed audio and speculative assessments must not become confirmed facts. Memory can fail to retrieve evidence; the assistant should acknowledge uncertainty rather than invent a recollection.

## Controls

Unlock the assistant and use the memory icon in the composer to inspect, correct, suppress, delete or undo consolidation of memory. Deletion removes the selected memory and linked source/derived records from the active store. It does not erase host conversation history, backups or provider retention. A request not to save a turn suppresses long-term storage; it is not a promise to erase the host timeline.

Proactive conversation requires the existing user opt-in. It never automatically opens the microphone. Task result notifications are persistent, deduplicated conversation messages driven by executor status changes.

## Optional local semantic embeddings

Without a configured endpoint, the engine uses lexical/concept retrieval. `AIDE_MIXSOUL_LOCAL_EMBEDDING` accepts a JSON object with `endpoint`, `model`, `revision`, `dimensions`, and optional `token`. Only literal loopback HTTP URLs are accepted; redirects and remote endpoints are rejected. The service must return OpenAI-style indexed embedding rows. Model identity and revision isolate vector spaces. Unavailable embeddings explicitly fall back to lexical/concept retrieval.

For an offline CPU service, `scripts/mixsoul-embedding-service.py` loads a pre-provisioned model at `/models`. It listens on loopback port 32141 and requires a 64-character hexadecimal `MIXSOUL_EMBEDDING_TOKEN`. Run it in the same network namespace as Aide. Protect credentials with deployment secrets; never place real tokens in committed configuration. The optional Dockerfile is separate from the normal Aide runtime and does not download model weights. Provision and verify dependencies and weights before enabling it; this integration does not automatically install or supervise that service.

Set `AIDE_ASSISTANT_TIMEZONE` to the operator's IANA timezone when needed; the default is UTC. Existing encrypted stores retain their format and scope identifier.

## Verification and scope

Run `node --test internal/server/mixsoul/*.test.cjs` for synthetic memory policies, decay, weight, emotion, provenance, topic isolation, deletion, encryption and persistence. Run the assistant Go regressions for host authorization, scope cancellation and native turn linkage. `scripts/mixsoul-eval.cjs` is a deterministic policy fixture, not a naturalness evaluation; the optional local-model evaluator requires an explicit loopback model configuration.

The module retains its existing separate license in `internal/server/mixsoul/LICENSE`. Public source availability does not remove that notice. Historical private development commits, research drafts, real conversations, acceptance dumps and local deployment launchers are excluded from this integration.
