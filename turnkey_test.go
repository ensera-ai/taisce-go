// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ensera-ai/taisce-go"
)

// testdata/turn-keys.json is the conformance suite's vector file, copied unchanged. Agreement with it
// is what makes a retried turn fold into its first attempt, and every adapter's keys the same keys.
func TestTurnKeyDerivesEveryPublishedVector(t *testing.T) {
	raw, err := os.ReadFile("testdata/turn-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Derivation string `json:"derivation"`
		Vectors    []struct {
			Name     string           `json:"name"`
			Subject  string           `json:"data_subject_id"`
			Run      string           `json:"run"`
			Messages []taisce.Message `json:"messages"`
			Key      string           `json:"key"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Derivation != taisce.TurnKeyDerivation {
		t.Fatalf("the vectors are for %q and this package derives %q", file.Derivation, taisce.TurnKeyDerivation)
	}
	for _, v := range file.Vectors {
		if got := taisce.TurnKey(v.Subject, v.Run, v.Messages); got != v.Key {
			t.Errorf("%s: derived %s, the vector says %s", v.Name, got, v.Key)
		}
	}
	// The opening pair reads identically joined line by line, and must not share a key.
	joined := func(ms []taisce.Message) string {
		var b strings.Builder
		for _, m := range ms {
			b.WriteString(m.Role + "\n" + m.Content + "\n")
		}
		return b.String()
	}
	a, b := file.Vectors[0], file.Vectors[1]
	if joined(a.Messages) != joined(b.Messages) || a.Key == b.Key {
		t.Fatal("the opening pair no longer shows why the derivation is length-prefixed")
	}
}
