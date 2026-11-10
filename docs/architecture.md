# Architecture and invariants

## Write path

1. Parse and validate every transaction in the incoming batch.
2. Resolve its `(source, external_id)` idempotency key against committed and staged records.
3. Reject a conflicting retry. Identical retries return the original sequence.
4. Apply debit and credit changes to a private balance copy; reject overflow.
5. Allocate contiguous sequence numbers and compute SHA-256 links over canonical JSON.
6. Append the entire batch to the journal and call fsync.
7. Only after fsync succeeds, publish the new in-memory state under the posting lock.

If persistence fails, the service attempts to truncate the partial append and stops accepting new writes in the current process. It cannot guarantee recovery from arbitrary storage corruption or power loss. On process startup, replay checks all recorded lines before accepting writes.

## Ledger invariants

- Transfer amounts are positive int64 minor units, never floating point.
- Debit and credit accounts are distinct and use the same currency.
- The sum of account deltas for each currency is zero.
- Every committed source/external key is unique.
- Entries have strictly increasing sequence numbers and matching previous hashes.
- A failed batch creates no acknowledged in-memory transfers.

## Reconciliation

An external statement row has the source key plus the transfer fields. The engine compares each row to its journal counterpart. Missing, mismatched and unexpected rows are reported with deterministic sort order. Statement transaction identifiers are not required to match internal IDs.

## Initial tradeoffs

JSONL is inspectable and makes replay easy, but each process must rebuild the balance and idempotency index on startup. The service supports one writer process per journal. The next storage implementation should use PostgreSQL row locking, unique constraints and transactional posting in a single commit. A future ingestion adapter should stream events in bounded chunks instead of loading the entire input file.
