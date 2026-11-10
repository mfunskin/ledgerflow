package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// Transaction records a movement of whole minor currency units.
// Debit and credit are distinct accounts and are always posted together.
type Transaction struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	ExternalID  string `json:"external_id"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	Currency    string `json:"currency"`
	AmountMinor int64  `json:"amount_minor"`
}

func (t Transaction) Validate() error {
	for name, value := range map[string]string{
		"id": t.ID, "source": t.Source, "external_id": t.ExternalID,
		"debit": t.Debit, "credit": t.Credit,
	} {
		if !identifier.MatchString(value) {
			return fmt.Errorf("invalid %s: expected 1-80 identifier characters", name)
		}
	}
	if t.Debit == t.Credit {
		return errors.New("debit and credit must be different accounts")
	}
	if !currencyCode.MatchString(t.Currency) {
		return errors.New("currency must be a three-letter uppercase ISO-style code")
	}
	if t.AmountMinor <= 0 {
		return errors.New("amount_minor must be positive")
	}
	if t.AmountMinor == math.MaxInt64 {
		return errors.New("amount_minor is too large")
	}
	return nil
}

func (t Transaction) Key() string { return t.Source + ":" + t.ExternalID }
func (t Transaction) Fingerprint() string {
	data, _ := json.Marshal(t)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type Outcome struct {
	ID       string `json:"id"`
	Status   string `json:"status"` // posted or duplicate
	Sequence uint64 `json:"sequence"`
}

type JournalRecord struct {
	Sequence     uint64      `json:"sequence"`
	PreviousHash string      `json:"previous_hash"`
	Hash         string      `json:"hash"`
	Transaction  Transaction `json:"transaction"`
}

func computeHash(sequence uint64, previous string, tx Transaction) string {
	data, _ := json.Marshal(struct {
		Sequence     uint64      `json:"sequence"`
		PreviousHash string      `json:"previous_hash"`
		Transaction  Transaction `json:"transaction"`
	}{sequence, previous, tx})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func accountKey(account, currency string) string { return account + ":" + currency }

func safeAdd(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b || b < 0 && a < math.MinInt64-b {
		return 0, errors.New("account balance would overflow")
	}
	return a + b, nil
}

func isValidHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(strings.ToLower(s))
	return err == nil && strings.ToLower(s) == s
}
