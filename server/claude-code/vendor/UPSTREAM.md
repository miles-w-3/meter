Vendored from https://github.com/dmmulroy/pi-claude-cli at commit 31e02ea.
Copyright and license: see LICENSE. Meter makes the pool limits a host setting
read on each use (`poolLimits` in src/provider-config.ts, with its own
defaults) and adds `ClaudeProcessPool.reconfigure` and
`ProviderRuntime.reconfigurePool`, which apply a change to parked processes. Integration is in ../provider.ts and ../bridge.ts.
src/event-bridge.ts parses streamed tool arguments only on the delta that
completes the JSON object (`ToolInputScan`), not after every
`input_json_delta`, so a long tool input costs linear rather than quadratic
time; covered by scripts/claude-tool-input-test.ts.
The test fixture scripts/fixtures/claude-stub.cjs is from the same revision,
with an added opt-in cwd log.
