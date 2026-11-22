# Claude Code provider prototype

Opt in on the backend host with `PI_CLAUDE_CODE=1`, install Claude Code, and
run `claude auth login` on that host. Restart the backend to enable the
provider, then select a Claude model from Meter's inventory or save it as the
default in Models settings. `PI_PROVIDER` and `PI_MODEL` do not select normal
providers or models.
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

The vendored runtime is pinned and attributed in `vendor/UPSTREAM.md`. It uses
up to two parked processes and a 15-minute idle timeout. Its default loopback
relay rewrites upstream requests to preserve caching. Its metadata traces are
enabled by default; set `PI_CLAUDE_CLI_TRACE=off` to disable them. Do not enable
raw wire tracing or relay dumps unless storing conversation content is intended.

The deterministic tests `node scripts/claude-provider-test.mjs` and
`node scripts/claude-inventory-test.mjs` use a fake CLI and do not verify real
Claude Code compatibility or Anthropic cache hits. The provider test turns the
relay off; its default rewrite mode still needs a live compatibility test.
