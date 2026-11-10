# Contributing to LedgerFlow

Use a focused issue and a feature branch for each change. Explain the invariant being protected and include a test that would fail without the change. Do not commit generated journal files, private statements, credentials or customer data.

Run before opening a pull request:

```bash
gofmt -w ./cmd ./internal
go vet ./...
go test -race ./...
```

Changes to journal serialization, idempotency or recovery behavior require compatibility tests using previously persisted records. Report failed experiments as failures instead of modifying historical measurements.
