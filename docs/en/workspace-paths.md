# Workspaces, references and document destinations

[简体中文](../workspace-paths.md) · [English documentation](README.md)

A workspace is the identity and execution scope of a project. Its working directory, automatic-document binding, cache and optional SSH connection are distinct settings. Configure them in the workspace dialog; do not infer a destination from a historical example path.

The workspace is writable when mounted as `/workspace`. Reference roots have independent enabled, AI-visible and writable flags. `/context` is normally read-only. `/local` follows the configured host mapping; a container path and host path are not interchangeable.

Automatic system documents use the configured document binding. File proposals can select `source="system-docs"` with a relative path. Local bindings use the local source root; an SSH workspace's SFTP binding writes through that workspace connection. An inaccessible binding returns an error rather than silently redirecting output. With no binding, legacy stage documents use the project cache and Office output uses the working directory. This does not redirect ordinary code files or explicitly targeted writes. Existing cache files are not migrated automatically.

The `system-docs` source is built in and marked enabled/read-write. Underlying filesystem permission and server path checks still apply. Ordinary references retain their visibility and write locks. General reference tools do not gain blanket write authority from document generation.

The file-panel New menu captures its current source/root and directory. File saves and directory creation carry that source identity. A source/directory switch invalidates an old menu submission. Relative paths must stay inside the selected root. Read-only sources cannot create files.

SFTP directory browsing uses the current form's host, port, user and authentication mode through a temporary connection. Blank credentials can reuse saved credentials only when the connection identity matches. Changing hosts requires suitable credentials. Browsing lists directories; it does not itself create or rename them.

Markdown images resolve relative to the document's own root/source and directory, including source-internal `../` paths. Side-panel and standalone previews use the same source boundary. Relative attachments open with source-aware file navigation; a preview must not silently substitute the working directory for a reference.

CSV/TSV tables preserve strings, leading zeros, BOM and line-ending style. Supported delimiters include comma, semicolon and Tab, with quoted fields and escaped quotes. Formulas remain text. The bounded table view shows at most 500 rows and 100 columns; undisplayed data remain in the source. Files above 2 MiB use text mode. Saving uses the existing workspace/source/hash conflict checks. Text interfaces require UTF-8 rather than automatic GBK/UTF-16 conversion.

Markdown headings H1–H6 provide an accessible outline outside code blocks. The outline collapses on selection, outside click or Escape and adapts to narrow preview widths. Editing, draft recovery and version restoration update the preview without changing document content merely to navigate.

Uploads, pasted images and dropped attachments use the selected writable source and explicit save flow. History and recovery drafts are separate from server-saved files. Verify actual save receipts and reopen the intended source before calling a change persistent.
