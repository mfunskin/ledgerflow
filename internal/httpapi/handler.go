package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mfunskin/ledgerflow/internal/ledger"
)

func Handler(book *ledger.Book) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/balances", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, book.Balances())
	})
	mux.HandleFunc("GET /v1/audit", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, book.Audit())
	})
	mux.HandleFunc("POST /v1/transactions", func(w http.ResponseWriter, r *http.Request) {
		var tx ledger.Transaction
		if !decodeBody(w, r, &tx) {
			return
		}
		outcomes, err := book.PostBatch([]ledger.Transaction{tx})
		if err != nil {
			writeBookError(w, err)
			return
		}
		code := http.StatusCreated
		if outcomes[0].Status == "duplicate" {
			code = http.StatusOK
		}
		respond(w, code, outcomes[0])
	})
	mux.HandleFunc("POST /v1/batches", func(w http.ResponseWriter, r *http.Request) {
		var txs []ledger.Transaction
		if !decodeBody(w, r, &txs) {
			return
		}
		outcomes, err := book.PostBatch(txs)
		if err != nil {
			writeBookError(w, err)
			return
		}
		respond(w, http.StatusCreated, outcomes)
	})
	mux.HandleFunc("POST /v1/reconcile", func(w http.ResponseWriter, r *http.Request) {
		var rows []ledger.StatementRow
		if !decodeBody(w, r, &rows) {
			return
		}
		report, err := book.Reconcile(rows)
		if err != nil {
			problem(w, http.StatusBadRequest, err.Error())
			return
		}
		respond(w, http.StatusOK, report)
	})
	return mux
}

func decodeBody(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(destination); err != nil {
		problem(w, 400, fmt.Sprintf("invalid JSON request: %v", err))
		return false
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			problem(w, 400, "unexpected additional JSON document")
		} else {
			problem(w, 400, "invalid JSON after first value")
		}
		return false
	}
	return true
}

func writeBookError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if strings.Contains(err.Error(), "idempotency conflict") {
		status = http.StatusConflict
	}
	if strings.Contains(err.Error(), "storage failure") || strings.Contains(err.Error(), "commit failed") {
		status = http.StatusServiceUnavailable
	}
	problem(w, status, err.Error())
}

func respond(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
func problem(w http.ResponseWriter, code int, message string) {
	respond(w, code, map[string]string{"error": message})
}
