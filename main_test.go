package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const sample = "examples/seq-scan-filter.txt"

func TestReview(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"review", sample}, nil, &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	for _, want := range []string{"Where the time goes", "PG101", "CREATE INDEX CONCURRENTLY ON orders (customer_id)", "leads from one run"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestFailOnAndJSON(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"review", sample, "--fail-on", "high", "--json"}, nil, &out, &errs); code != 1 {
		t.Fatalf("exit %d", code)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
}

func TestStdinAndCompare(t *testing.T) {
	data, err := os.ReadFile(sample)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if code := run([]string{"review", "-"}, bytes.NewReader(data), &out, &errs); code != 0 {
		t.Fatalf("exit %d: %s", code, errs.String())
	}
	out.Reset()
	if code := run([]string{"compare", sample, "examples/seq-scan-fixed.json"}, nil, &out, &errs); code != 0 || !strings.Contains(out.String(), "Fixed:") {
		t.Fatalf("exit %d: %s", code, out.String())
	}
}

func TestErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{{}, {"nope"}, {"review"}, {"review", "/no/such"}, {"compare", sample}, {"review", sample, "--fail-on", "x"}} {
		var out, errs bytes.Buffer
		if code := run(args, nil, &out, &errs); code != 2 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}

func TestVersion(t *testing.T) {
	var out, errs bytes.Buffer
	if code := run([]string{"version"}, nil, &out, &errs); code != 0 || strings.TrimSpace(out.String()) != version {
		t.Fatal(out.String())
	}
}
