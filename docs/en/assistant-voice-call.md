# Native realtime voice calls

[简体中文](../assistant-voice-call.md) · [English documentation](README.md)

The assistant's Voice call entry uses a Qwen native realtime audio session. AudioWorklet converts microphone input to 16 kHz mono PCM, sent over Aide's same-origin WebSocket to the server and then to Qwen. Returned 24 kHz PCM plays incrementally. This entry does not use browser speech recognition or a separate TTS synthesis request.

1. Use HTTPS or localhost, unlock the assistant and select Voice call.
2. Grant microphone access. The native model is `qwen3.8-omni-flash-realtime` with the `Tina` voice, independent of ordinary read-aloud and TTS Tools.
3. You can interrupt while listening. Server semantic VAD determines turns; speech onset clears unplayed audio and cancels the older response. Truncation uses the played position, and late audio from an old response is discarded.
4. Muting releases input capture while retaining playback. Hanging up, leaving the assistant, hiding the page, locking or changing workspace ends the call and closes its upstream connection. Background tasks already started are not cancelled by hanging up.

Configure the Qwen business-space HTTPS endpoint and its region-matching key. Aide resolves the credential in the vault; it is not sent to the browser. The browser connects only to Aide and supplies the existing local access token in a WebSocket subprotocol header, not a URL. Provider failures are explicit; there is no silent fallback to the old turn-based implementation.

The realtime model can request `assistant_work` through existing assistant authorization. Only the current user transcript received by the server can become that work input. Browsers cannot inject system instructions, arbitrary tool results or conversation history. Tool call IDs are deduplicated. The model gains no independent file, terminal or message permissions.

The assistant's live text mirror is an in-memory cache: up to 100 KiB per session and 256 sessions (25 MiB of text, plus allocation overhead). Least recently used terminal entries are evicted before active entries. If all slots are active, a new run has no live mirror until capacity becomes available; primary session SSE and saved messages continue normally. Recent interrupted/failed output remains readable, while older entries may be evicted. Read persisted messages through the session API. Single deletion, archived-session bulk deletion and session reset remove both the mirror and its task bindings; delayed delta, finish or start callbacks cannot recreate a deleted session's cache. Cache contents do not survive a process restart.

Transcripts are saved in the unlocked assistant session; raw microphone PCM is not written to disk. Each assistant session permits one native call, up to 30 minutes. There is no automatic paid reconnection. Severe network/playback backlog ends the call rather than accumulating audio indefinitely. Normal task notifications do not interrupt native playback.

Provider audio billing is separate from the text-token pricing chart. Use the provider's audio billing console for costs. Downloadable MP3/WAV files remain the responsibility of TTS Tools.

Historical tests include a simulated upstream and synthetic audio sent to the real provider. They do not establish subjective microphone/speaker quality or echo behavior on every device. This English-documentation update itself does not repeat paid audio tests. See the dated [implementation record](../tasks/assistant-native-duplex-20261023.json) for its original scope.
