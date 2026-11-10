package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestCompleteCLIWorkflow(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "sample.jsonl")
	journal := filepath.Join(dir, "journal.jsonl")
	statement := filepath.Join(dir, "statement.jsonl")
	execute(t, "generate", "-out", input, "-seed", "17", "-count", "25")
	first := execute(t, "post", "-journal", journal, "-input", input)
	if !strings.Contains(first, `"posted":25`) {
		t.Fatal(first)
	}
	duplicate := execute(t, "post", "-journal", journal, "-input", input)
	if !strings.Contains(duplicate, `"duplicates":25`) {
		t.Fatal(duplicate)
	}
	execute(t, "statement", "-journal", journal, "-out", statement)
	result := execute(t, "reconcile", "-journal", journal, "-input", statement)
	var report struct {
		Matched       int   `json:"matched"`
		Discrepancies []any `json:"discrepancies"`
	}
	if err := json.Unmarshal([]byte(result), &report); err != nil {
		t.Fatal(err)
	}
	if report.Matched != 25 || len(report.Discrepancies) != 0 {
		t.Fatal(result)
	}
	if !strings.Contains(execute(t, "audit", "-journal", journal), `"balanced":true`) {
		t.Fatal("unbalanced")
	}
	if !strings.Contains(execute(t, "balances", "-journal", journal), "USD") {
		t.Fatal("no balances")
	}
	if _, err := os.Stat(journal); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateDeterministicFiles(t *testing.T) {
	dir := t.TempDir()
	one, two := filepath.Join(dir, "one.jsonl"), filepath.Join(dir, "two.jsonl")
	execute(t, "generate", "-count", "10", "-seed", "44", "-out", one)
	execute(t, "generate", "-count", "10", "-seed", "44", "-out", two)
	a, _ := os.ReadFile(one)
	b, _ := os.ReadFile(two)
	if !bytes.Equal(a, b) {
		t.Fatal("same seed generated different input")
	}
}

func TestCLIRejectsBadInput(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"unknown"}, &out); err == nil {
		t.Fatal("accepted unknown command")
	}
	if err := run([]string{"generate", "-count", "0", "-out", "ignored"}, &out); err == nil {
		t.Fatal("accepted empty generator")
	}
}
