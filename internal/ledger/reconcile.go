package ledger

import (
	"fmt"
	"sort"
)

// StatementRow is an external view of an expected transfer.
// The transaction ID is not compared because external systems may assign their own.
type StatementRow struct {
	Source      string `json:"source"`
	ExternalID  string `json:"external_id"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	Currency    string `json:"currency"`
	AmountMinor int64  `json:"amount_minor"`
}

func (row StatementRow) validate() error {
	return (Transaction{ID: "statement-row", Source: row.Source, ExternalID: row.ExternalID,
		Debit: row.Debit, Credit: row.Credit, Currency: row.Currency, AmountMinor: row.AmountMinor}).Validate()
}

func (row StatementRow) matches(tx Transaction) bool {
	return row.Source == tx.Source && row.ExternalID == tx.ExternalID &&
		row.Debit == tx.Debit && row.Credit == tx.Credit &&
		row.Currency == tx.Currency && row.AmountMinor == tx.AmountMinor
}

type Discrepancy struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type Reconciliation struct {
	StatementRows int           `json:"statement_rows"`
	JournalRows   int           `json:"journal_rows"`
	Matched       int           `json:"matched"`
	Discrepancies []Discrepancy `json:"discrepancies"`
}

func (b *Book) Reconcile(rows []StatementRow) (Reconciliation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := Reconciliation{StatementRows: len(rows), JournalRows: len(b.records), Discrepancies: []Discrepancy{}}
	expected := make(map[string]StatementRow)
	for i, row := range rows {
		if err := row.validate(); err != nil {
			return result, fmt.Errorf("statement row %d: %w", i+1, err)
		}
		key := row.Source + ":" + row.ExternalID
		if _, exists := expected[key]; exists {
			return result, fmt.Errorf("duplicate statement key: %s", key)
		}
		expected[key] = row
		if record, found := b.index[key]; !found {
			result.Discrepancies = append(result.Discrepancies, Discrepancy{key, "missing_in_journal", "statement transfer was not posted"})
		} else if !row.matches(record.Transaction) {
			result.Discrepancies = append(result.Discrepancies, Discrepancy{key, "mismatch", "posted fields differ from statement"})
		} else {
			result.Matched++
		}
	}
	for key := range b.index {
		if _, found := expected[key]; !found {
			result.Discrepancies = append(result.Discrepancies, Discrepancy{key, "unexpected_in_journal", "posted transfer absent from statement"})
		}
	}
	sort.Slice(result.Discrepancies, func(i, j int) bool {
		if result.Discrepancies[i].Key == result.Discrepancies[j].Key {
			return result.Discrepancies[i].Kind < result.Discrepancies[j].Kind
		}
		return result.Discrepancies[i].Key < result.Discrepancies[j].Key
	})
	return result, nil
}
