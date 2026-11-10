# LedgerFlow reviewer walkthrough

## Demonstration

From a clean clone, run `go test -race ./...` and `go build -o bin/ledgerflow ./cmd/ledgerflow`. Then execute the transaction posting and reconciliation commands in the README.

The journal creates three balanced transfers. The sample statement produces one match, one amount mismatch, one missing journal entry and one unexpected journal entry. Reposting the same input should yield zero new entries and three duplicate acknowledgments.

## Engineering decisions

- Integer minor units avoid rounding errors inherent to floating-point money types.
- Source/external IDs enforce idempotency, including conflict detection across restarts.
- Batch writes are checked before a single append and fsync.
- Replay verifies sequential SHA-256 hash links before accepting new transactions.
- Reconciliation runs against a snapshot protected by the journal mutex.

## Current scope

This is a single-process engineering prototype using a local file journal. It is not integrated with a financial network and does not contain personal financial data. Its primary next challenge is moving the transactional storage path to PostgreSQL while retaining replay and idempotency guarantees.
