package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mfunskin/ledgerflow/internal/ledger"
)

func handler(t *testing.T) http.Handler {
	t.Helper()
	book, err := ledger.Open(filepath.Join(t.TempDir(), "journal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { book.Close() })
	return Handler(book)
}

func request(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	return recorder
}

func TestHTTPCreateDuplicateConflictAndAudit(t *testing.T) {
	h := handler(t)
	tx := ledger.Transaction{ID: "tx1", Source: "bank-a", ExternalID: "evt1", Debit: "cash", Credit: "merchant", Currency: "USD", AmountMinor: 300}
	encoded, _ := json.Marshal(tx)
	if rr := request(t, h, "POST", "/v1/transactions", string(encoded)); rr.Code != 201 {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := request(t, h, "POST", "/v1/transactions", string(encoded)); rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte("duplicate")) {
		t.Fatal(rr.Code, rr.Body)
	}
	tx.AmountMinor = 301
	changed, _ := json.Marshal(tx)
	if rr := request(t, h, "POST", "/v1/transactions", string(changed)); rr.Code != 409 {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := request(t, h, "GET", "/v1/audit", ""); rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte(`"entries":1`)) {
		t.Fatal(rr.Code, rr.Body)
	}
	if rr := request(t, h, "GET", "/v1/balances", ""); rr.Code != 200 || !bytes.Contains(rr.Body.Bytes(), []byte(`"cash:USD":-300`)) {
		t.Fatal(rr.Code, rr.Body)
	}
}

func TestHTTPValidatesPayloadAndMethod(t *testing.T) {
	h := handler(t)
	cases := []struct {
		method, path, body string
		code               int
	}{
		{"POST", "/v1/transactions", `{`, 400},
		{"POST", "/v1/transactions", `{} {}`, 400},
		{"POST", "/v1/transactions", `{"unknown":true}`, 400},
		{"POST", "/v1/batches", `[]`, 400},
		{"GET", "/v1/transactions", "", 405},
		{"GET", "/v1/health", "", 200},
	}
	for _, tc := range cases {
		rr := request(t, h, tc.method, tc.path, tc.body)
		if rr.Code != tc.code {
			t.Errorf("%s %s: expected %d got %d", tc.method, tc.path, tc.code, rr.Code)
		}
	}
}

func TestHTTPReconciliation(t *testing.T) {
	h := handler(t)
	tx := `{"id":"t1","source":"bank","external_id":"1","debit":"a","credit":"b","currency":"USD","amount_minor":15}`
	request(t, h, "POST", "/v1/transactions", tx)
	statement := `[{"source":"bank","external_id":"1","debit":"a","credit":"b","currency":"USD","amount_minor":15}]`
	rr := request(t, h, "POST", "/v1/reconcile", statement)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"matched":1`) {
		t.Fatal(rr.Code, rr.Body.String())
	}
}
