# Claude Code provider prototype

Enable **Claude Code subscription** in Meter's Models settings, install Claude
Code, and run `claude auth login` on the backend host. Enable and disable take
effect immediately without a backend restart. Select a Claude model from
Meter's inventory or save it as the default in Models settings. Disabling the
provider makes its models unavailable but preserves existing selections.
In Models settings, use **Refresh login & models** after login or logout. This
explicitly checks Claude Code auth and probes the account's model catalog. The
probe can make Claude Code's normal feature-flag network requests; it does not
send a model prompt. The displayed catalog source is `account`, `cached`, or
`builtin`: the builtin fallback is not guaranteed to match the account.

Claude Code owns its login. The provider does not import credentials. This is
experimental; Claude Code subscription terms, availability and billing can
change. Usage-credit guardrails are inherited from upstream; they cannot undo
an already charged request.

A generation hook maps the durable provider session to the conversation cwd.
The mapping is consumed once and a missing mapping fails closed. Meter forbids
cwd changes during a run. A cwd change between runs retires the parked process.
Compaction requests are intentionally rejected because compaction does not run
the generation hook; Meter currently disables automatic compaction.

The vendored runtime is pinned and attributed in `vendor/UPSTREAM.md`. Each
conversation gets its own Claude Code process, parked between calls. Models
settings choose how long an idle process is kept (default 1 hour), how long one
waits inside a tool call (default 1 hour, the CLI's own tool timeout), and how
many are parked at once (default 3; each may use substantial memory). Changes
are blocked while conversations or queued messages are active. Once idle, they
are saved in the workspace database and applied to parked processes. Its default loopback
relay rewrites upstream requests to preserve caching. Its metadata traces are
enabled by default; set `PI_CLAUDE_CLI_TRACE=off` to disable them. Do not enable
raw wire tracing or relay dumps unless storing conversation content is intended.

The deterministic tests `node scripts/claude-provider-test.mjs` and
`node scripts/claude-inventory-test.mjs` use a fake CLI and do not verify real
Claude Code compatibility or Anthropic cache hits. The provider test turns the
relay off; its default rewrite mode still needs a live compatibility test.
