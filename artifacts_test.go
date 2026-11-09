// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ensera-ai/taisce-go"
)

// Every artifact call names the subject, and one that does not is refused here rather than by the
// deployment.
//
// Not duplication of a server rule: the subject is what makes one end user's state unreachable to
// another inside a project that one credential opens, and a caller who omitted it has written that
// bug. Refusing before the request is what lets the message say so, and the deployment is still the
// thing that enforces it.
func TestAnArtifactCallWithoutTheSubjectThatBoundsItIsRefusedBeforeItIsSent(t *testing.T) {
	ctx := context.Background()
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer server.Close()
	client, err := taisce.New(server.URL, "tsk")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.PutArtifact(ctx, taisce.ArtifactPut{ID: "s1", Content: []byte("{}")}); err == nil {
		t.Error("an artifact was written without a subject")
	}
	if _, err := client.GetArtifact(ctx, taisce.ArtifactRef{ID: "s1"}); err == nil {
		t.Error("an artifact was read without a subject")
	}
	if err := client.DeleteArtifact(ctx, taisce.ArtifactRef{ID: "s1"}); err == nil {
		t.Error("an artifact was deleted without a subject")
	}
	if _, err := client.ListArtifacts(ctx, "", "state", "", "", 10); err == nil {
		t.Error("artifacts were listed without a subject")
	}
	// And one that does not name the artifact either.
	if _, err := client.PutArtifact(ctx, taisce.ArtifactPut{DataSubjectID: "alice"}); err == nil {
		t.Error("an artifact was written without an id")
	}
	if reached {
		t.Fatal("a request without a subject reached the deployment")
	}
}

// The bytes go up and come back as bytes, and the version a write returns is the one the next write
// expects to replace.
func TestArtifactBytesRoundTripAndCarryTheVersionTheNextWriteExpects(t *testing.T) {
	ctx := context.Background()
	var sent struct {
		ID              string `json:"id"`
		ExpectedVersion string `json:"expected_version"`
		DataSubjectID   string `json:"data_subject_id"`
		Kind            string `json:"kind"`
		Content         []byte `json:"content"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/artifacts/put":
			if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{"id":"s1","version":"v2","bytes":2}`))
		case "/v1/artifacts/get":
			_, _ = w.Write([]byte(`{"id":"s1","version":"v2","content":"e30="}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := taisce.New(server.URL, "tsk")
	if err != nil {
		t.Fatal(err)
	}

	receipt, err := client.PutArtifact(ctx, taisce.ArtifactPut{
		ID: "s1", ExpectedVersion: "v1", DataSubjectID: "alice", Kind: "state", Name: "session",
		Content: []byte("{}"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Version != "v2" {
		t.Fatalf("the write returned version %q", receipt.Version)
	}
	if sent.ExpectedVersion != "v1" || sent.DataSubjectID != "alice" || sent.Kind != "state" ||
		string(sent.Content) != "{}" {
		t.Fatalf("the write did not carry what it was given: %+v", sent)
	}

	stored, err := client.GetArtifact(ctx, taisce.ArtifactRef{ID: "s1", DataSubjectID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.Content) != "{}" || stored.Version != "v2" {
		t.Fatalf("the read came back as %+v", stored)
	}
}

// An artifact this subject does not have is ErrArtifactNotFound, a stale write is
// ErrArtifactConflict, and a delete of something already gone is not an error at all.
//
// The first matters most: "this person has not been here before" is the ordinary first turn of every
// conversation, and a caller should not read HTTP codes to handle it. Another person's artifact
// answers the same way, which is the check that keeps one person's state unreachable to another.
func TestAnArtifactThatIsNotThereIsNotFoundAndAStaleWriteIsAConflict(t *testing.T) {
	ctx := context.Background()
	notFound := answering(t, 404, `{"error":{"code":"not_found","message":"artifact not found"}}`)
	if _, err := notFound.GetArtifact(ctx, taisce.ArtifactRef{ID: "s1", DataSubjectID: "alice"}); !errors.Is(err, taisce.ErrArtifactNotFound) {
		t.Fatalf("a missing artifact came back as %v", err)
	}
	if err := notFound.DeleteArtifact(ctx, taisce.ArtifactRef{ID: "s1", DataSubjectID: "alice"}); err != nil {
		t.Fatalf("deleting an artifact that is already gone was an error: %v", err)
	}
	conflict := answering(t, 409, `{"error":{"code":"artifact_conflict","message":"version"}}`)
	if _, err := conflict.PutArtifact(ctx, taisce.ArtifactPut{
		ID: "s1", DataSubjectID: "alice", ExpectedVersion: "stale", Content: []byte("{}"),
	}); !errors.Is(err, taisce.ErrArtifactConflict) {
		t.Fatalf("a stale write came back as %v", err)
	}
	// Another refusal is not either sentinel.
	other := answering(t, 403, `{"error":{"code":"forbidden","message":"no"}}`)
	if _, err := other.GetArtifact(ctx, taisce.ArtifactRef{ID: "s1", DataSubjectID: "alice"}); errors.Is(err, taisce.ErrArtifactNotFound) {
		t.Fatal("a forbidden read was reported as a missing artifact")
	}
}

// A passage search asks a question, and one with nothing to ask is refused before it is sent.
func TestAPassageSearchAsksAQuestionAndReportsHowCompleteTheIndexWas(t *testing.T) {
	ctx := context.Background()
	client := answering(t, 200, `{"generation_id":"g1","build_state":"building","approximate":true,
	  "passages":[{"chunk_id":"c1","source_id":"o1","ordinal":2,"role":"user","preview":"I work at Ensera","similarity":0.81}]}`)
	results, err := client.SearchPassages(ctx, taisce.PassageRequest{Question: "where does Atlas work", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !results.Approximate || len(results.Passages) != 1 || results.Passages[0].ChunkID != "c1" ||
		results.Passages[0].Similarity != 0.81 {
		t.Fatalf("the search came back as %+v", results)
	}
	if _, err := client.SearchPassages(ctx, taisce.PassageRequest{Question: "  "}); err == nil {
		t.Fatal("a passage search with no question was sent")
	}
}
