# Engineering roadmap

## Milestone 1: PostgreSQL storage engine

Implement a repository interface and durable PostgreSQL writer with a unique constraint on `(source, external_id)`, transactions and a migration runner. Include concurrent writer integration tests using an ephemeral database. Measure equivalence against JSONL replay.

## Milestone 2: Failure injection

Exercise partial writes, fsync failures, process crashes, interrupted replays, and retries across process boundaries. Define recovery behavior and demonstrate no acknowledged partial batch.

## Milestone 3: Heterogeneous reconciliation

Support separately versioned bank, processor and internal settlement schemas, currency rules, mismatch categories, tolerances where applicable, and reconciliation exception workflows.

## Milestone 4: Throughput engineering

Provide bounded streaming ingestion, backpressure, worker coordination, profiling, and reproducible challenge datasets at 100,000 and 1,000,000 rows. Report throughput, memory use, conflict rate and correctness.

## Milestone 5: Operations

Add authenticated APIs, metrics, tracing, deployment automation and an operator console. Design read-only versus write permissions before connecting any live financial source.

All milestones are unimplemented at v0.1. Every change should be built on a feature branch, reviewed, and verified by CI.
