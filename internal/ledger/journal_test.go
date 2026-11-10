package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func sample(id string, minor int64) Transaction {
	return Transaction{ID: id, Source: "bank-a", ExternalID: id, Debit: "cash", Credit: "revenue", Currency: "USD", AmountMinor: minor}
}

func openBook(t *testing.T) (*Book, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b, path
}

func TestTransactionValidation(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Transaction)
	}{
		{"invalid id", func(tx *Transaction) { tx.ID = "bad id" }},
		{"invalid external id", func(tx *Transaction) { tx.ExternalID = "" }},
		{"missing source", func(tx *Transaction) { tx.Source = "" }},
		{"lowercase currency", func(tx *Transaction) { tx.Currency = "usd" }},
		{"nonstandard currency", func(tx *Transaction) { tx.Currency = "USD4" }},
		{"negative amount", func(tx *Transaction) { tx.AmountMinor = -1 }},
		{"zero amount", func(tx *Transaction) { tx.AmountMinor = 0 }},
		{"same account", func(tx *Transaction) { tx.Credit = tx.Debit }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := sample("t1", 100)
			tc.change(&tx)
			if tx.Validate() == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestPostPersistsBalancedEntries(t *testing.T) {
	b, path := openBook(t)
	outcomes, err := b.PostBatch([]Transaction{sample("t1", 100), sample("t2", 30)})
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 || outcomes[0].Sequence != 1 || outcomes[1].Sequence != 2 {
		t.Fatal(outcomes)
	}
	if b.Balances()["cash:USD"] != -130 || b.Balances()["revenue:USD"] != 130 {
		t.Fatal(b.Balances())
	}
	if audit := b.Audit(); !audit.Balanced || audit.Entries != 2 || len(audit.LastHash) != 64 {
		t.Fatal(audit)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Audit() != b.Audit() {
		t.Fatal("journal changed after replay")
	}
}

func TestDuplicateIsIdempotent(t *testing.T) {
	b, _ := openBook(t)
	tx := sample("t1", 200)
	outcomes, err := b.PostBatch([]Transaction{tx, tx, tx})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Status != "posted" || outcomes[1].Status != "duplicate" || outcomes[2].Status != "duplicate" {
		t.Fatal(outcomes)
	}
	outcomes, err = b.PostBatch([]Transaction{tx})
	if err != nil || outcomes[0].Sequence != 1 || outcomes[0].Status != "duplicate" {
		t.Fatal(outcomes, err)
	}
	if b.Audit().Entries != 1 {
		t.Fatal("duplicate was posted")
	}
}

func TestConflictDoesNotChangeBalance(t *testing.T) {
	b, _ := openBook(t)
	first := sample("t1", 100)
	if _, err := b.PostBatch([]Transaction{first}); err != nil {
		t.Fatal(err)
	}
	modified := first
	modified.AmountMinor = 101
	if _, err := b.PostBatch([]Transaction{modified}); err == nil || !strings.Contains(err.Error(), "idempotency conflict") {
		t.Fatal(err)
	}
	if b.Audit().Entries != 1 || b.Balances()["cash:USD"] != -100 {
		t.Fatal(b.Audit(), b.Balances())
	}
}

func TestInvalidBatchRollsBackEntireBatch(t *testing.T) {
	b, _ := openBook(t)
	invalid := sample("t2", 20)
	invalid.Credit = invalid.Debit
	if _, err := b.PostBatch([]Transaction{sample("t1", 10), invalid}); err == nil {
		t.Fatal("expected error")
	}
	if b.Audit().Entries != 0 || len(b.Balances()) != 0 {
		t.Fatal("partial transaction")
	}
}

func TestConflictInsideBatchRollsBack(t *testing.T) {
	b, _ := openBook(t)
	tx1 := sample("t1", 10)
	tx2 := tx1
	tx2.AmountMinor++
	if _, err := b.PostBatch([]Transaction{tx1, tx2}); err == nil {
		t.Fatal("expected conflict")
	}
	if b.Audit().Entries != 0 {
		t.Fatal("partial batch commit")
	}
}

func TestBatchBounds(t *testing.T) {
	b, _ := openBook(t)
	if _, err := b.PostBatch(nil); err == nil {
		t.Fatal("accepted empty batch")
	}
	if _, err := b.PostBatch(make([]Transaction, 10001)); err == nil {
		t.Fatal("accepted oversized batch")
	}
}

func TestReplayRejectsTamperedJournal(t *testing.T) {
	b, path := openBook(t)
	if _, err := b.PostBatch([]Transaction{sample("t1", 100)}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"amount_minor":100`, `"amount_minor":200`, 1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("accepted tampering")
	}
}

func TestReplayRejectsTruncatedJSON(t *testing.T) {
	b, path := openBook(t)
	if _, err := b.PostBatch([]Transaction{sample("t1", 100)}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"sequence\":"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("accepted torn journal")
	}
}

func TestConcurrentWritersHaveUniqueSequences(t *testing.T) {
	b, _ := openBook(t)
	const count = 100
	var wg sync.WaitGroup
	errors := make(chan error, count)
	for i := range count {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := b.PostBatch([]Transaction{sample(fmt.Sprintf("t%d", i), 1)})
			errors <- err
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if audit := b.Audit(); !audit.Balanced || audit.Entries != count {
		t.Fatal(audit)
	}
	seen := make(map[uint64]bool)
	for _, record := range b.Records() {
		if seen[record.Sequence] {
			t.Fatal("duplicate journal sequence")
		}
		seen[record.Sequence] = true
	}
}

func TestBalanceSnapshotIsIndependent(t *testing.T) {
	b, _ := openBook(t)
	if _, err := b.PostBatch([]Transaction{sample("t1", 12)}); err != nil {
		t.Fatal(err)
	}
	snap := b.Balances()
	snap["cash:USD"] = 1000
	if b.Balances()["cash:USD"] != -12 {
		t.Fatal("external snapshot mutated ledger")
	}
	history := b.Records()
	history[0].Transaction.AmountMinor = 0
	if b.Records()[0].Transaction.AmountMinor != 12 {
		t.Fatal("external history mutated ledger")
	}
}

func TestReconcileAllCategories(t *testing.T) {
	b, _ := openBook(t)
	if _, err := b.PostBatch([]Transaction{sample("t1", 100), sample("t2", 200), sample("t3", 300)}); err != nil {
		t.Fatal(err)
	}
	rows := []StatementRow{
		{Source: "bank-a", ExternalID: "t1", Debit: "cash", Credit: "revenue", Currency: "USD", AmountMinor: 100},
		{Source: "bank-a", ExternalID: "t2", Debit: "cash", Credit: "revenue", Currency: "USD", AmountMinor: 201},
		{Source: "bank-a", ExternalID: "t4", Debit: "cash", Credit: "revenue", Currency: "USD", AmountMinor: 400},
	}
	result, err := b.Reconcile(rows)
	if err != nil {
		t.Fatal(err)
	}
	if result.Matched != 1 || len(result.Discrepancies) != 3 {
		t.Fatal(result)
	}
	want := []string{"mismatch", "unexpected_in_journal", "missing_in_journal"}
	for i, row := range result.Discrepancies {
		if row.Kind != want[i] {
			t.Errorf("%d: want %s, got %s", i, want[i], row.Kind)
		}
	}
}

func TestReconcileRejectsDuplicateStatementKeys(t *testing.T) {
	b, _ := openBook(t)
	row := StatementRow{"bank-a", "t1", "cash", "revenue", "USD", 100}
	if _, err := b.Reconcile([]StatementRow{row, row}); err == nil {
		t.Fatal("accepted duplicate statement")
	}
}
