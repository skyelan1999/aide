# SQLite database plugin

SQLite is independently distributed under MIT in [aide-pkg-sqlite](https://github.com/skyelan1999/aide-pkg-sqlite). New Aide images do not bundle it. Install the marketplace ZIP or an exact Release package; updating Aide does not replace an installed plugin.

Version 1.0.3 supports Node 24 DatabaseSync queries, transactions, scripts, schema inspection, CSV and database export. Queries default to read-only; transactions/scripts are writable by default and export writes a file. Review SQL and paths and back up databases. Parameter binding, per-statement dangerous-write confirmation and native ATTACH authorization do not sandbox trusted plugin code. Result truncation and busy_timeout are not general SQL memory/execution budgets.

Local relative paths follow the host-selected workspace; legacy absolute paths retain their allowed-root checks. SSH workspaces and enabled AI-visible references use per-call offline checkout and guarded host writeback. Reference writes require RW; exports may select a separate outputSource. Requires Aide 0.1.19.0 RC1; old hosts explicitly refuse the remote path.

Stop external writers and complete WAL checkpoint until the operation ends. Nonempty WAL/journal is refused; four slots, 64 MiB per file and aggregate per call. The checks are optimistic, not distributed CAS/database locks or an atomic transaction across database/export slots. Cancellation, relocking, source change or conflict can refuse saving; reread after uncertain output.

See the [canonical Chinese guide](../../plugins/sqlite.md), [plugin source/release documentation](https://github.com/skyelan1999/aide-pkg-sqlite/blob/main/README.md) and [0.1.19 migration](../release-guide-0.1.19.md).
