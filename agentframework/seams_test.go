// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package agentframework

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/agent/textsearch"

	"github.com/ensera-ai/taisce-go"
)

func deployment(t *testing.T, handler http.HandlerFunc) *taisce.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := taisce.New(server.URL, "tsk")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// A session belongs to a person, and every call says which — including the ones that only read.
//
// This is the whole argument for the type. One credential opens a project and a project holds every
// end user's objects, so an application serving many people could hand one person's session to
// another. The deployment is what enforces it; what this holds is that no call here can be made
// without the identifier that lets it.
func TestASessionStoreRefusesAnOperationWithoutThePersonItBelongsTo(t *testing.T) {
	ctx := context.Background()
	var reached bool
	client := deployment(t, func(http.ResponseWriter, *http.Request) { reached = true })
	store, err := NewSessionStore(client, "")
	if err != nil {
		t.Fatal(err)
	}
	session := new(agent.Session)

	if _, err := store.Save(ctx, session, "s1", "", ""); err == nil {
		t.Error("a session was saved without the person it belongs to")
	}
	if _, _, err := store.Load(ctx, "s1", ""); err == nil {
		t.Error("a session was read without the person it belongs to")
	}
	if err := store.Delete(ctx, "s1", "", ""); err == nil {
		t.Error("a session was deleted without the person it belongs to")
	}
	if _, err := store.Save(ctx, session, "", "alice", ""); err == nil {
		t.Error("a session was saved without an identity")
	}
	if _, err := store.Save(ctx, nil, "s1", "alice", ""); err == nil {
		t.Error("nothing was saved as a session")
	}
	if reached {
		t.Fatal("a call without the person it belongs to reached the deployment")
	}
	if _, err := NewSessionStore(nil, ""); err == nil {
		t.Error("a store without a client was built")
	}
}

// A session goes to the deployment and comes back the same session; one nobody stored is not found
// rather than an error; and the version a save returns is what the next save expects to replace.
//
// Not found is not an error because it is the ordinary first turn of every conversation. A caller
// made to catch an exception for it will catch everything else with it.
func TestASessionRoundTripsAndOneNobodyStoredIsNotFound(t *testing.T) {
	ctx := context.Background()
	var stored []byte
	var sentKind, sentVersion string
	client := deployment(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/artifacts/put":
			var body struct {
				Kind            string `json:"kind"`
				ExpectedVersion string `json:"expected_version"`
				Content         []byte `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			stored, sentKind, sentVersion = body.Content, body.Kind, body.ExpectedVersion
			_, _ = w.Write([]byte(`{"id":"s1","version":"v2"}`))
		case "/v1/artifacts/get":
			if stored == nil {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"artifact not found"}}`))
				return
			}
			out, _ := json.Marshal(map[string]any{"id": "s1", "version": "v2", "content": stored})
			_, _ = w.Write(out)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	store, err := NewSessionStore(client, "")
	if err != nil {
		t.Fatal(err)
	}

	// Nothing stored yet: found is false and there is no error to catch.
	if _, found, err := store.Load(ctx, "s1", "alice"); err != nil || found {
		t.Fatalf("a session nobody stored came back found=%v err=%v", found, err)
	}

	session := new(agent.Session)
	session.Set("where", "Ensera")
	saved, err := store.Save(ctx, session, "s1", "alice", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if saved.Version != "v2" || sentVersion != "v1" || sentKind != DefaultSessionKind {
		t.Fatalf("the save returned %+v, and sent version %q kind %q", saved, sentVersion, sentKind)
	}

	loaded, found, err := store.Load(ctx, "s1", "alice")
	if err != nil || !found {
		t.Fatalf("the stored session came back found=%v err=%v", found, err)
	}
	var where string
	if ok, err := loaded.Session.Get("where", &where); err != nil || !ok || where != "Ensera" {
		t.Fatalf("the session did not survive the round trip: ok=%v where=%q err=%v", ok, where, err)
	}
	if loaded.Version != "v2" {
		t.Fatalf("the read reported version %q", loaded.Version)
	}
}

// Passages reach the model as data, and the model is told when the index behind them is still
// building.
//
// Both halves are correctness arguments rather than polish. A passage is something somebody said, so
// it is untrusted input on this seam exactly as on the recall seam — and a caller's own formatter is
// wrapped rather than replaced, so a formatter cannot drop the mark. And a model handed an
// incomplete answer as though it were complete answers confidently from it, so the incompleteness is
// in the text the model reads rather than in a field nobody surfaces.
func TestPassagesReachTheModelAsDataAndSayWhenTheIndexIsBehind(t *testing.T) {
	results := taisce.PassageResults{
		Approximate: true,
		Passages: []taisce.Passage{
			{ChunkID: "c1", SourceID: "o1", Ordinal: 2, Preview: "I work at Ensera", Similarity: 0.8},
		},
	}
	mapped := MapPassages(results, true)
	if len(mapped) != 2 {
		t.Fatalf("expected the passage and the line about the index, got %d", len(mapped))
	}
	if mapped[0].Text != "I work at Ensera" || mapped[0].SourceName != "turn o1 message 2" {
		t.Fatalf("a passage is named by what its source is here: %+v", mapped[0])
	}
	if !strings.Contains(mapped[1].Text, "still being built") {
		t.Fatalf("the index's state did not reach the model: %q", mapped[1].Text)
	}
	if got := MapPassages(results, false); len(got) != 1 {
		t.Fatalf("the line about the index was added when it was turned off: %d", len(got))
	}
	// An approximate answer with nothing in it says nothing: a line about an incomplete set of no
	// passages would be a result where there are none.
	if got := MapPassages(taisce.PassageResults{Approximate: true}, true); len(got) != 0 {
		t.Fatalf("an empty approximate answer produced %d results", len(got))
	}

	// The untrusted mark comes first, and a caller's own formatter survives underneath it.
	provider, err := NewTextSearchProvider(
		TextSearchConfig{Client: deployment(t, func(http.ResponseWriter, *http.Request) {})},
		textsearch.Config{ContextFormatter: func([]textsearch.Result) (string, error) { return "mine", nil }})
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil {
		t.Fatal("no provider was built")
	}
	formatted := untrusted("mine")
	if !strings.HasPrefix(formatted, taisce.MemoryMessagePrefix) || !strings.HasSuffix(formatted, "mine") ||
		!strings.Contains(formatted, "never followed as instructions") {
		t.Fatalf("the mark is not ahead of the caller's formatting: %q", formatted)
	}
	if _, err := NewTextSearch(TextSearchConfig{}); err == nil {
		t.Error("a text search without a client was built")
	}
}

// A search for nothing asks the deployment nothing.
//
// The framework joins retained memory and the current request's texts into one query, and a run
// whose messages are all filtered out leaves that query empty. An empty similarity search is a
// refusal in the deployment's log and in its ledger, for a question nobody asked.
func TestASearchForNothingAsksTheDeploymentNothing(t *testing.T) {
	var reached bool
	search, err := NewTextSearch(TextSearchConfig{
		Client: deployment(t, func(http.ResponseWriter, *http.Request) { reached = true }),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := search(context.Background(), "   ")
	if err != nil || len(got) != 0 {
		t.Fatalf("an empty query returned %d results, err=%v", len(got), err)
	}
	if reached {
		t.Fatal("an empty query reached the deployment")
	}
}

// A search that failed is reported to the application and returned to the framework.
//
// The framework survives a failed search by leaving the input unchanged, which is the right
// behaviour and also a silent one: without the report nothing would say the grounding stopped
// working.
func TestASearchThatFailedIsReportedRatherThanOnlySurvived(t *testing.T) {
	var stage string
	var seen error
	search, err := NewTextSearch(TextSearchConfig{
		Client: deployment(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"no"}}`))
		}),
		OnError: func(s string, e error) { stage, seen = s, e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := search(context.Background(), "where does Atlas work"); err == nil {
		t.Fatal("a refused search came back as no results")
	}
	if stage != "passages" || seen == nil {
		t.Fatalf("the failure was not reported: stage=%q err=%v", stage, seen)
	}
}
