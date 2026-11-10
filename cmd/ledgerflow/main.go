// LedgerFlow is a local-first, append-only financial transfer reconciliation service.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mfunskin/ledgerflow/internal/httpapi"
	"github.com/mfunskin/ledgerflow/internal/ledger"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ledgerflow:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: ledgerflow <generate|post|balances|audit|statement|reconcile|serve> [flags]")
	}
	command := args[0]
	if command == "generate" {
		flags := flag.NewFlagSet("generate", flag.ContinueOnError)
		output := flags.String("out", "", "destination JSONL")
		count := flags.Int("count", 100, "number of transactions (max 10000)")
		seed := flags.Int64("seed", 42, "deterministic seed")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *output == "" || *count < 1 || *count > 10000 {
			return errors.New("provide -out and -count between 1 and 10000")
		}
		random := rand.New(rand.NewSource(*seed))
		transactions := make([]ledger.Transaction, *count)
		accounts := []string{"customer-a", "customer-b", "customer-c", "merchant-a", "merchant-b", "clearing"}
		for i := range transactions {
			a := random.Intn(len(accounts))
			b := random.Intn(len(accounts) - 1)
			if b >= a {
				b++
			}
			transactions[i] = ledger.Transaction{
				ID: fmt.Sprintf("tx-%06d", i+1), Source: "simulator",
				ExternalID: fmt.Sprintf("evt-%06d", i+1),
				Debit:      accounts[a], Credit: accounts[b], Currency: "USD",
				AmountMinor: int64(1 + random.Intn(100000)),
			}
		}
		if err := writeJSONL(*output, transactions); err != nil {
			return err
		}
		return encode(out, map[string]any{"transactions": len(transactions), "output": *output, "seed": *seed})
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	journalPath := flags.String("journal", "runs/journal.jsonl", "journal file")
	input := flags.String("input", "", "input JSONL")
	output := flags.String("out", "", "output JSONL")
	address := flags.String("addr", "127.0.0.1:8081", "HTTP listen address")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	switch command {
	case "post", "balances", "audit", "statement", "reconcile", "serve":
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
	if err := os.MkdirAll(filepath.Dir(*journalPath), 0700); err != nil {
		return err
	}
	book, err := ledger.Open(*journalPath)
	if err != nil {
		return err
	}
	defer book.Close()
	switch command {
	case "post":
		if *input == "" {
			return errors.New("post requires -input")
		}
		txs, err := readJSONL[ledger.Transaction](*input)
		if err != nil {
			return err
		}
		outcomes, err := book.PostBatch(txs)
		if err != nil {
			return err
		}
		posted, duplicate := 0, 0
		for _, item := range outcomes {
			if item.Status == "posted" {
				posted++
			} else {
				duplicate++
			}
		}
		return encode(out, map[string]any{"posted": posted, "duplicates": duplicate, "audit": book.Audit()})
	case "balances":
		return encode(out, book.Balances())
	case "audit":
		return encode(out, book.Audit())
	case "statement":
		if *output == "" {
			return errors.New("statement requires -out")
		}
		var rows []ledger.StatementRow
		for _, record := range book.Records() {
			tx := record.Transaction
			rows = append(rows, ledger.StatementRow{Source: tx.Source, ExternalID: tx.ExternalID,
				Debit: tx.Debit, Credit: tx.Credit, Currency: tx.Currency, AmountMinor: tx.AmountMinor})
		}
		if err := writeJSONL(*output, rows); err != nil {
			return err
		}
		return encode(out, map[string]any{"statement_rows": len(rows), "output": *output})
	case "reconcile":
		if *input == "" {
			return errors.New("reconcile requires -input")
		}
		rows, err := readJSONL[ledger.StatementRow](*input)
		if err != nil {
			return err
		}
		report, err := book.Reconcile(rows)
		if err != nil {
			return err
		}
		return encode(out, report)
	case "serve":
		server := &http.Server{Addr: *address, Handler: httpapi.Handler(book),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second}
		log.Printf("LedgerFlow listening on %s", *address)
		return server.ListenAndServe()
	}
	return errors.New("unreachable command")
}

func encode(out io.Writer, payload any) error { return json.NewEncoder(out).Encode(payload) }

func readJSONL[T any](path string) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var records []T
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var item T
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("line %d: %w", len(records)+1, err)
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("line %d: trailing JSON data", len(records)+1)
		}
		records = append(records, item)
		if len(records) > 10000 {
			return nil, errors.New("input exceeds 10000 records per batch")
		}
	}
	return records, scanner.Err()
}

func writeJSONL[T any](path string, rows []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, row := range rows {
		if err := json.NewEncoder(file).Encode(row); err != nil {
			return err
		}
	}
	return file.Sync()
}
