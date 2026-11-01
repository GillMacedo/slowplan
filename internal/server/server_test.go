package server

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReadNeedsExactlyOneSource(t *testing.T) {
	if _, err := Read("", ""); err == nil {
		t.Error("neither")
	}
	if _, err := Read("x", "y"); err == nil {
		t.Error("both")
	}
	if _, err := Read("", "/no/such/file"); err == nil {
		t.Error("missing file")
	}
}

func TestTools(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "seq-scan-filter.json")
	fixed := filepath.Join("..", "..", "examples", "seq-scan-fixed.txt")
	_, result, err := review(context.Background(), nil, ReviewInput{Path: path})
	if err != nil || len(result.Findings) == 0 {
		t.Fatalf("%v %+v", err, result)
	}
	_, comparison, err := compare(context.Background(), nil, CompareInput{BeforePath: path, AfterPath: fixed})
	if err != nil || len(comparison.Fixed) == 0 {
		t.Fatalf("%v %+v", err, comparison)
	}
	if _, _, err := compare(context.Background(), nil, CompareInput{BeforePath: path}); err == nil {
		t.Fatal("expected an error for a missing after plan")
	}
}
