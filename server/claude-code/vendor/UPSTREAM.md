Vendored from https://github.com/dmmulroy/pi-claude-cli at commit 31e02ea.
Copyright and license: see LICENSE. Meter makes the pool limits a host setting
read on each use (`poolLimits` in src/provider-config.ts, with its own
defaults) and adds `ClaudeProcessPool.reconfigure` and
`ProviderRuntime.reconfigurePool`, which apply a change to parked processes. Integration is in ../provider.ts and ../bridge.ts.
The test fixture scripts/fixtures/claude-stub.cjs is from the same revision,
with an added opt-in cwd log.
