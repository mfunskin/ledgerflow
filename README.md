# LedgerFlow

**Transactional event posting and financial statement reconciliation in Go.**

LedgerFlow accepts transfers expressed in integer minor currency units, posts balanced debit and credit movements, and reconciles a source statement against its append-only journal. The v0.1 implementation is a standalone Go service using the standard library. It does not depend on banking APIs, customer data, or cloud services.

## Implemented

- Append-only JSONL journal with monotonic sequence numbers and SHA-256 hash links.
- Idempotency by source and external transaction ID. Identical retries return the original sequence; conflicting retries fail.
- All-or-nothing batch validation, checked balance arithmetic, and fsync before in-memory commit.
- Replay checks for malformed records, broken hash links, or duplicate source keys.
- Multi-currency balances with no floating-point money values.
- Reconciliation reports matched, missing, mismatched, and unexpected transfers.
- Concurrency-safe posting inside a single running process.
- HTTP endpoints, deterministic synthetic data generator, and a command line interface.
- Unit, concurrency, integration and race-detector tests.

## Quick start

Requires **Go 1.23 or newer**. No third-party Go dependencies.

```bash
go test ./...
go vet ./...
go test -race ./...
go build -o bin/ledgerflow ./cmd/ledgerflow
```

Post and reconcile the included synthetic example:

```bash
./bin/ledgerflow post -journal runs/ledger.jsonl -input examples/transactions.jsonl
./bin/ledgerflow balances -journal runs/ledger.jsonl
./bin/ledgerflow audit -journal runs/ledger.jsonl
./bin/ledgerflow reconcile -journal runs/ledger.jsonl -input examples/statement.jsonl
```

`post` is idempotent, so replaying the same input results in duplicate acknowledgments without new postings. The sample statement deliberately includes one incorrect amount, one missing transfer, and one unexpected journal transfer, to exercise reconciliation.

Generate reproducible challenge data:

```bash
./bin/ledgerflow generate -seed 42 -count 5000 -out runs/transactions.jsonl
./bin/ledgerflow post -journal runs/generated-journal.jsonl -input runs/transactions.jsonl
./bin/ledgerflow statement -journal runs/generated-journal.jsonl -out runs/statement.jsonl
./bin/ledgerflow reconcile -journal runs/generated-journal.jsonl -input runs/statement.jsonl
```

The CLI accepts batches of at most 10,000 records. `generate` and `statement` create new files and refuse to overwrite existing output paths.

## HTTP API

```bash
./bin/ledgerflow serve -journal runs/service.jsonl -addr 127.0.0.1:8081
```

```bash
curl -s http://127.0.0.1:8081/v1/health
curl -s http://127.0.0.1:8081/v1/audit
curl -s http://127.0.0.1:8081/v1/balances
curl -s -X POST http://127.0.0.1:8081/v1/transactions \
  -H 'Content-Type: application/json' \
  -d '{"id":"tx-101","source":"demo","external_id":"evt-101","debit":"cash","credit":"sales","currency":"USD","amount_minor":1250}'
```

| Endpoint | Behavior |
| --- | --- |
| `GET /v1/health` | Process health |
| `POST /v1/transactions` | Post one transfer, 201 for new, 200 for duplicate, 409 for conflict |
| `POST /v1/batches` | Atomic batch posting |
| `GET /v1/balances` | Balances keyed by account and currency |
| `GET /v1/audit` | Entry count, journal hash and balance invariant |
| `POST /v1/reconcile` | Compare an array of statement rows against the journal |

### Transaction format

```json
{
  "id": "tx-101",
  "source": "demo",
  "external_id": "evt-101",
  "debit": "cash",
  "credit": "sales",
  "currency": "USD",
  "amount_minor": 1250
}
```

`amount_minor` is a positive integer. Each posted transaction subtracts it from the debit account and adds it to the credit account within the same currency. A journal cannot contain more than one distinct payload for a given `(source, external_id)` pair. Identifiers are intentionally restricted to a defined ASCII character set.

## Architecture and constraints

The journal is durable within a single process using append and fsync. This is **not a distributed transaction database**. Use one writer process for a journal file; multiple processes sharing the same journal path are unsupported. A failed write places the process into a state requiring a restart and journal integrity check. Hash links provide tamper evidence, not tamper prevention against someone who can rewrite the entire journal.

The API binds to localhost by default and does not implement authentication, authorization, TLS termination, or service-to-service isolation. Never expose it directly to the public internet. The data generator and examples are synthetic. See [architecture](docs/architecture.md) and [engineering roadmap](docs/roadmap.md).

## Project development

The initial release is intentionally focused on correctness, not a simulated banking product. Future milestones include PostgreSQL transactions with writer coordination, crash and fault-injection tests, streaming ingestion with backpressure, heterogeneous statement adapters, observability, and a reconciliation review interface. Those features are **not implemented** in v0.1.

## License

MIT. See [LICENSE](LICENSE).
