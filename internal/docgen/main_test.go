package main

import (
	"bytes"
	"os"
	"testing"
)

func TestRulesReferenceIsCurrent(t *testing.T) {
	have, err := os.ReadFile("../../docs/rules.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(have, render()) {
		t.Fatal("docs/rules.md is stale: run `go generate ./...`")
	}
}
