# Local verification results

These observations were produced with the synthetic `examples/transactions.jsonl` file.

| Check | Observed |
| --- | --- |
| First posting | 3 new journal entries |
| Identical retry | 0 new entries, 3 duplicates |
| Balance audit | Balanced |
| Statement reconciliation | 1 matched, 3 discrepancies |

Discrepancies: `gateway:pay-2` is a mismatch; `gateway:pay-3` is unexpected in the journal; `gateway:pay-4` is missing from the journal. These are intentional sample defects. Re-run the documented CLI commands to reproduce them.

The Go unit tests, race detector and `go vet ./...` passed in the development environment. The repository has not yet been published to GitHub or validated on the user's WSL machine.
