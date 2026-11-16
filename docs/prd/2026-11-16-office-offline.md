# Built-in offline Office

The user requires document viewing, editing and saving directly inside Aide, without a separate Collabora deployment or any address/settings entry. This supersedes the external-service setup shipped in Office 1.2.0.

The Linux Aide runtime (including the Docker image used by macOS/Windows launchers) bundles the unmodified, hash-pinned CODE AppImage for its own architecture. Build-time staging may use the network; the delivered runtime never downloads or installs the engine. Existing image export/import packages carry this engine automatically.

On the first authenticated Office request Aide starts the engine as its existing unprivileged user, in an owned private temporary directory. Readiness is bounded and cancellable; subsequent opens reuse it, failed children can restart, and Aide shutdown terminates the process group and private callback listener. Aide publishes no additional host ports. The engine can reach only the loopback WOPI endpoint by configuration; documents, remote fonts and update checks use no cloud services.

Editor resources and WebSockets pass through /office-engine/ on the existing Aide origin. A short-lived HttpOnly, SameSite capability cookie is issued by the authenticated open API; Aide bearer credentials and cookies are stripped upstream. Proxy endpoints are limited to browser/editor traffic, with administration and conversion endpoints refused. WOPI retains document-scoped tokens and source/workspace/version/lock checks.

No Office plugin configuration fields remain. The existing viewer opens in preview; editing, saving, AI attachment and idle-save protection remain. Document viewing is a built-in host feature; an explicitly disabled installed Office plugin revokes its sessions. AI tools continue to follow plugin enablement and existing approvals. Cloud AI models still require their provider connection; fully offline AI requires an already available local model.

Acceptance uses a fresh isolated runtime on a Docker internal network (no external route), no Collabora settings, and a browser restricted to the local Aide origin: DOCX/XLSX/PPTX open/edit/save/readback, native AI read/edit, preview/active/idle/stale/readonly refusal, child restart/shutdown and proxy rejection. Linux ARM64 is executable locally; AMD64 staging and build contracts are checked separately and must not be reported as native execution.

The user further requires Aide-only use: no independently exposed plugin API. The public Aide listener rejects /wopi/ entirely. Document callbacks exist only on the private loopback server; unauthenticated editor-resource/WebSocket access and administration/conversion endpoints are rejected. No external MCP/HTTP service is registered.

The former general /api/office/document/read and /edit routes are removed. Structured document reads and edits are invoked through the internal Aide native tool dispatcher, with task/workspace/sandbox checks; no replacement standalone HTTP or MCP service is provided. The browser retains only the authenticated session endpoints needed for its embedded editor.
