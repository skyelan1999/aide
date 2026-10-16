# Authenticated workbench capability dashboard

Native Safari on the existing localhost9999 workbench, production image ebf9e382. Opened Settings / Accessibility / Interface tests and initiated workspace, skill plugin and pypdf checks through actual UI buttons. Independent authenticated API readback agrees with displayed state and scope: [proof](capability-workbench-proof.json).

Workspace root read passed, while write permission remained untested. Skill entry read passed with SHA-256 and an explicit statement that no plugin/tool was executed. pypdf isolated import passed with an explicit statement that file extraction/rendering remains untested. Time and duration were visible. Unchecked entries remained unchecked; disabled communication plugins had disabled check buttons.

Private screenshots: .agent-state/capability-workbench-20261016/workspace-read.png, plugin-read.png and parser-read.png. Closed Settings and restored original workbench. Settings, sources, authentication and all session bytes match the prior native-cleanup baseline exactly. No new tab, model call, plugin execution or write probe.

This adds authenticated UI success-state and scope evidence only. Failure display, user-host SSH permissions and complete plugin/document processing remain pending. Check results expire after five minutes and clear on restart.
