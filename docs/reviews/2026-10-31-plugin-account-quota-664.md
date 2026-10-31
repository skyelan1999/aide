# Plugin account quota - issue664

The account pool previously consumed monetary provider-support balances only. A local Codex Bridge and official API provider with the same display name both appeared unsupported; installed Bridge0.1.2 had no quota support. The fix makes quota endpoint/origin ownership an explicit plugin data contract. It does not identify a provider by display name.

A quota-only catalog declares local scheme/port and a same-origin read-only path with model-bearer auth and quota-windows format, without prices/currency. Existing HTTPS pricing and money formats remain. Credentials stay in the host; redirects are refused. Responses are bounded and allowlisted; in-flight plugin disable/replacement is checked. The pool shows percentages, windows and resets; missing quota is unavailable, and a pricing-only plugin gets its own accurate state.

Evidence: quick passed; focused Go race suite includes selected key/origin, no pricing fallback from quota catalog, disable/inflight, invalid paths/origins, legacy404, malformed payload, redirect refusal, unavailable data and actual0.4.1 ZIP installation/read. Eleven account-pool Node tests and 53 plugin Node tests passed; native manager Go race passed in Linux Docker. Safari isolated18980 fixture showed75%/60% windows and separate pricing-only official account. These percentages are synthetic. Raw evidence under .agent-state/quota-664 is not published.

The local previously installed bridge0.1.2 was safely updated using the published0.4.0 package with verified SHA, unchanged config/key hashes and a program-file rollback copy. Actual account read returned available data with one window; no inference/reset consumption was performed. Values/credentials stay private. Official protocol: https://learn.chatgpt.com/docs/app-server (account/rateLimits/read).

Publication and actual9999 pool acceptance are tracked separately in issue664 and task ledger. No new release or admin exception is claimed here.
