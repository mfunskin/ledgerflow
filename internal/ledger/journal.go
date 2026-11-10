package ledger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
)

// Book is a process-local transactional journal. Its filesystem must support
// fsync and truncate. Use one process per journal path, not multiple writers.
type Book struct {
	mu       sync.Mutex
	file     *os.File
	records  []JournalRecord
	index    map[string]JournalRecord
	balances map[string]int64
	hash     string
	poisoned bool
}

func Open(path string) (*Book, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	book := &Book{file: file, index: make(map[string]JournalRecord), balances: make(map[string]int64)}
	if err := book.replay(); err != nil {
		file.Close()
		return nil, err
	}
	return book, nil
}

func (b *Book) replay() error {
	if _, err := b.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	scanner := bufio.NewScanner(b.file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var line int
	for scanner.Scan() {
		line++
		raw := scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			return fmt.Errorf("empty journal line %d", line)
		}
		var record JournalRecord
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&record); err != nil {
			return fmt.Errorf("journal line %d: %w", line, err)
		}
		if err := requireEOF(dec); err != nil {
			return fmt.Errorf("journal line %d: %w", line, err)
		}
		if err := record.Transaction.Validate(); err != nil {
			return fmt.Errorf("journal line %d: %w", line, err)
		}
		if record.Sequence != uint64(len(b.records)+1) || record.PreviousHash != b.hash ||
			!isValidHash(record.Hash) || record.Hash != computeHash(record.Sequence, b.hash, record.Transaction) {
			return fmt.Errorf("journal integrity failure at line %d", line)
		}
		if _, exists := b.index[record.Transaction.Key()]; exists {
			return fmt.Errorf("duplicate source key at journal line %d", line)
		}
		if err := applyBalance(b.balances, record.Transaction); err != nil {
			return err
		}
		b.index[record.Transaction.Key()] = record
		b.records = append(b.records, record)
		b.hash = record.Hash
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading journal: %w", err)
	}
	_, err := b.file.Seek(0, io.SeekEnd)
	return err
}

func requireEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values on one line")
	}
	return nil
}

func applyBalance(balances map[string]int64, tx Transaction) error {
	debitKey := accountKey(tx.Debit, tx.Currency)
	creditKey := accountKey(tx.Credit, tx.Currency)
	debit, err := safeAdd(balances[debitKey], -tx.AmountMinor)
	if err != nil {
		return err
	}
	credit, err := safeAdd(balances[creditKey], tx.AmountMinor)
	if err != nil {
		return err
	}
	balances[debitKey], balances[creditKey] = debit, credit
	return nil
}

// PostBatch validates every new transaction before writing anything.
// Identical source/external-id events are acknowledged without posting twice.
func (b *Book) PostBatch(transactions []Transaction) ([]Outcome, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.poisoned {
		return nil, errors.New("journal requires recovery after a storage failure")
	}
	if len(transactions) == 0 || len(transactions) > 10000 {
		return nil, errors.New("batch size must be between 1 and 10000")
	}
	staged := make(map[string]JournalRecord)
	balances := make(map[string]int64, len(b.balances))
	for key, value := range b.balances {
		balances[key] = value
	}
	var newRecords []JournalRecord
	outcomes := make([]Outcome, 0, len(transactions))
	hash := b.hash
	for _, tx := range transactions {
		if err := tx.Validate(); err != nil {
			return nil, fmt.Errorf("transaction %q: %w", tx.ID, err)
		}
		key := tx.Key()
		if previous, found := b.index[key]; found {
			if previous.Transaction.Fingerprint() != tx.Fingerprint() {
				return nil, fmt.Errorf("idempotency conflict for %s", key)
			}
			outcomes = append(outcomes, Outcome{tx.ID, "duplicate", previous.Sequence})
			continue
		}
		if previous, found := staged[key]; found {
			if previous.Transaction.Fingerprint() != tx.Fingerprint() {
				return nil, fmt.Errorf("idempotency conflict within batch for %s", key)
			}
			outcomes = append(outcomes, Outcome{tx.ID, "duplicate", previous.Sequence})
			continue
		}
		if err := applyBalance(balances, tx); err != nil {
			return nil, fmt.Errorf("transaction %q: %w", tx.ID, err)
		}
		sequence := uint64(len(b.records) + len(newRecords) + 1)
		record := JournalRecord{Sequence: sequence, PreviousHash: hash, Transaction: tx}
		record.Hash = computeHash(sequence, hash, tx)
		newRecords = append(newRecords, record)
		staged[key] = record
		hash = record.Hash
		outcomes = append(outcomes, Outcome{tx.ID, "posted", sequence})
	}
	if len(newRecords) == 0 {
		return outcomes, nil
	}
	before, err := b.file.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	var data bytes.Buffer
	for _, record := range newRecords {
		encoded, _ := json.Marshal(record)
		data.Write(encoded)
		data.WriteByte('\n')
	}
	n, err := b.file.Write(data.Bytes())
	if err == nil && n != data.Len() {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = b.file.Sync()
	}
	if err != nil {
		// Once persistence fails, a new process must reopen and validate the journal.
		b.poisoned = true
		_ = b.file.Truncate(before)
		_ = b.file.Sync()
		return nil, fmt.Errorf("journal commit failed: %w", err)
	}
	for _, record := range newRecords {
		b.index[record.Transaction.Key()] = record
		b.records = append(b.records, record)
	}
	b.balances = balances
	b.hash = hash
	return outcomes, nil
}

func (b *Book) Balances() map[string]int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	copy := make(map[string]int64, len(b.balances))
	for key, value := range b.balances {
		copy[key] = value
	}
	return copy
}

func (b *Book) Records() []JournalRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]JournalRecord(nil), b.records...)
}

type Integrity struct {
	Entries  int    `json:"entries"`
	LastHash string `json:"last_hash"`
	Balanced bool   `json:"balanced"`
}

func (b *Book) Audit() Integrity {
	b.mu.Lock()
	defer b.mu.Unlock()
	byCurrency := make(map[string]int64)
	balanced := true
	for key, value := range b.balances {
		currency := key[len(key)-3:]
		next, err := safeAdd(byCurrency[currency], value)
		if err != nil {
			balanced = false
			break
		}
		byCurrency[currency] = next
	}
	for _, value := range byCurrency {
		if value != 0 {
			balanced = false
		}
	}
	return Integrity{Entries: len(b.records), LastHash: b.hash, Balanced: balanced && !b.poisoned}
}

func (b *Book) Close() error { b.mu.Lock(); defer b.mu.Unlock(); return b.file.Close() }

func SortedAccounts(balances map[string]int64) []string {
	keys := make([]string, 0, len(balances))
	for key := range balances {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
