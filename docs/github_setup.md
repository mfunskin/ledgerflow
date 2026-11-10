# Publish LedgerFlow

Run these commands from the root of the extracted `ledgerflow` directory after reviewing the source and running the tests.

```bash
git init -b main
git add .
git commit -m "feat: initialize LedgerFlow transaction service"
gh repo create ledgerflow --public --source=. --remote=origin --push
```

The GitHub CLI (`gh`) must be installed and authenticated. The `runs/` directory contains generated records and is excluded from version control. Check the GitHub Actions workflow after pushing; review the README and demonstrate idempotency and reconciliation before presenting the repository for external assessment.

Suggested GitHub description: `Go transaction journal and reconciliation service with idempotent batch posting, hash-linked audit records, and concurrency tests.`
