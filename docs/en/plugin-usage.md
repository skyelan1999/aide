# Where to start with plugins

Open **Plugins → Installed**, find a plugin and select **How to use**. This entry remains available when the plugin is disabled or fails to load.

- **Workbench plugins:** open the workbench and follow its file, connection or example prompts. Help is also available in its header; closing help returns to the same workbench.
- **Video and STEP previews:** select **Open from project files → Go to project files**, choose a supported file, then choose the plugin under Open with. An empty standalone workbench has no file to read.
- **Chat tools:** describe the inputs, goal, destination and acceptance criteria in chat. Tool switches, permissions and approval still apply.
- **Provider/account/routing support:** configure the provider and available models in Model settings first. Installation does not supply a model account.
- **Declaration-only packages:** the guide explicitly states that there is no separate workbench or currently executable tool.

**Enabled** means the plugin may load. Check actual configuration, connections and saved files separately. **Enable plugin / Disable plugin** labels describe the action the button will perform.

Use **Plugin settings** from the guide when configuration is required. **Read included package documentation** shows the installed README as plain text without executing it or changing the package. Missing or truncated documentation is indicated.

For load failures, inspect the card error and documented dependencies, correct settings and refresh. Separate engines, native hosts, accounts and devices remain prerequisites. Help does not install them or bypass authorization.

Unsubmitted goal-review, acceptance-criteria and project-knowledge drafts survive task/history view changes within the current page. Save before reloading the browser. If the stored record changed, review it and explicitly choose whether to keep or discard the draft. Knowledge subject, content and source/version are required.

`GET /api/plugins/{id}/guide` returns installed identity, capability type, executable-tool metadata and at most 128 KiB of the installed README. It excludes settings, secrets, tool schemas and pricing catalogs. Existing authentication applies; a locked host returns 423. Only fixed README filenames inside the plugin directory are read.

This describes source behavior. Source merges, installed versions, runtime upgrades and native/account/device acceptance are separate states.
