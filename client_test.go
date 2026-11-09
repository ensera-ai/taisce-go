// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ensera-ai/taisce-go"
)

func TestHTTPSOrPlaintextToALoopbackHostIsAccepted(t *testing.T) {
	for _, address := range []string{
		"https://memory.example", "https://memory.example:8443/base",
		"http://localhost:18080", "http://127.0.0.1:5", "http://127.9.9.9", "http://[::1]:8080",
	} {
		if _, err := taisce.New(address, "tsk"); err != nil {
			t.Errorf("%s: %v", address, err)
		}
	}
}

// The credential is sent with every request; one mistyped scheme would hand it to the network path.
func TestAnAddressTheCredentialWouldCrossTheNetworkInClearToReachIsRefused(t *testing.T) {
	for _, address := range []string{
		"http://memory.example", "http://10.0.0.5:8080", "http://localhost.evil.example",
		"ftp://localhost", "memory.example", "https://", "",
	} {
		if _, err := taisce.New(address, "tsk"); err == nil {
			t.Errorf("%q was accepted", address)
		}
	}
	if _, err := taisce.New("https://memory.example", " "); err == nil {
		t.Error("a client was built without a credential")
	}
}

// A redirect is not followed: the credential is not sent a second time, to anybody.
func TestARedirectIsNotFollowedWithTheCredential(t *testing.T) {
	var leaked bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer elsewhere.Close()
	deployment := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer deployment.Close()
	client, err := taisce.New(deployment.URL, "tsk_secret")
	if err != nil {
		t.Fatal(err)
	}
	var refusal *taisce.Error
	if _, err := client.Freshness(context.Background()); !errors.As(err, &refusal) || refusal.Status != http.StatusTemporaryRedirect {
		t.Fatalf("a redirect was not refused: %v", err)
	}
	if leaked {
		t.Fatal("the credential followed the redirect")
	}
}

// answering is a stand-in for the two branches a live deployment cannot be made to take on demand —
// a receipt that is not clean, an export over the limit — and for checking a refusal's mapping. The
// behaviour itself is proved against a deployment in live_test.go and by the conformance suite.
func answering(t *testing.T, status int, body string) *taisce.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	client, err := taisce.New(server.URL, "tsk")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestAReceiptThatIsNotCleanIsAnErrorCarryingTheWholeReceipt(t *testing.T) {
	client := answering(t, 200, `{"request_id":"r","scope":"p1","data_subject_id":"alice","completed_at":"2026-11-09T00:00:00Z","deleted":{"message":2},"residual":{"message":1},"clean":false}`)
	_, err := client.EraseSubject(context.Background(), "alice", "request")
	var notClean *taisce.ErasureNotCleanError
	if !errors.As(err, &notClean) || notClean.Receipt.Residual["message"] != 1 || notClean.Receipt.Deleted["message"] != 2 {
		t.Fatalf("a receipt that is not clean came back as %v", err)
	}
	if !strings.Contains(err.Error(), "message:1") {
		t.Fatalf("the error does not say what is left: %v", err)
	}
}

func TestARefusedErasureAndAnOversizedExportAreTheirOwnErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := answering(t, 403, `{"error":{"code":"forbidden","message":"no"}}`).EraseSubject(ctx, "alice", "r"); !errors.Is(err, taisce.ErrForbidden) {
		t.Fatalf("403 forbidden is not ErrForbidden: %v", err)
	}
	if _, err := answering(t, 422, `{"error":{"code":"export_too_large","message":"too many"}}`).ExportSubject(ctx, "alice"); !errors.Is(err, taisce.ErrExportTooLarge) {
		t.Fatalf("422 export_too_large is not ErrExportTooLarge: %v", err)
	}
	_, err := answering(t, 409, `{"error":{"code":"idempotency_conflict","message":"x"}}`).EraseSubject(ctx, "alice", "r")
	var refusal *taisce.Error
	if !errors.As(err, &refusal) || refusal.Code != "idempotency_conflict" || errors.Is(err, taisce.ErrForbidden) || errors.Is(err, taisce.ErrExportTooLarge) {
		t.Fatalf("another refusal was mapped to a sentinel: %v", err)
	}
	if _, err := answering(t, 500, `not json`).Freshness(ctx); !errors.As(err, &refusal) || refusal.Code != "unexpected_response" {
		t.Fatalf("an unreadable refusal: %v", err)
	}
}

func TestARequestNamingNobodyIsRefusedBeforeItIsSent(t *testing.T) {
	ctx := context.Background()
	client := answering(t, 500, `{}`)
	many := make([]string, taisce.MaxErasureSources+1)
	for i := range many {
		many[i] = "00000000-0000-4000-8000-000000000000"
	}
	for name, call := range map[string]func() error{
		"erase nobody":        func() error { _, err := client.EraseSubject(ctx, " ", "r"); return err },
		"erase no sources":    func() error { _, err := client.EraseSources(ctx, nil, "r"); return err },
		"erase too many":      func() error { _, err := client.EraseSources(ctx, many, "r"); return err },
		"erase a non-id":      func() error { _, err := client.EraseSources(ctx, []string{"x"}, "r"); return err },
		"export nobody":       func() error { _, err := client.ExportSubject(ctx, ""); return err },
		"export no sources":   func() error { _, err := client.ExportSources(ctx, nil); return err },
		"observe nothing":     func() error { _, err := client.Observe(ctx, taisce.ObserveRequest{}); return err },
		"recall no question":  func() error { _, err := client.Recall(ctx, taisce.RecallRequest{}); return err },
		"context nobody":      func() error { _, err := client.Context(ctx, taisce.ContextRequest{}); return err },
		"citation of no fact": func() error { _, err := client.ResolveCitation(ctx, "", 0); return err },
	} {
		var refusal *taisce.Error
		if err := call(); err == nil || errors.As(err, &refusal) {
			t.Errorf("%s: not refused locally: %v", name, err)
		}
	}
}

func TestTheMemoryMessageIsThePrefixLineAndOneDocument(t *testing.T) {
	stored := int64(4)
	rendered := taisce.RenderMemoryMessage(taisce.Freshness{Stored: &stored}, taisce.RecallBundle{Facts: []byte(`[{"fact_id":"f1"}]`)})
	if !strings.HasPrefix(rendered, taisce.MemoryMessagePrefix+"\n{") || !taisce.IsMemoryText(rendered) {
		t.Fatalf("unexpected rendering %q", rendered)
	}
	for _, want := range []string{`"stored":4`, `"formed":null`, `"facts":[{"fact_id":"f1"}]`, `"reports":[]`, `"degraded":[]`} {
		if !strings.Contains(rendered, want) {
			t.Errorf("%s missing from %s", want, rendered)
		}
	}
	if (taisce.RecallBundle{Facts: []byte(`[]`)}).Empty() != true || (taisce.RecallBundle{Reports: []byte(`[{}]`)}).Empty() {
		t.Error("Empty misjudged a bundle")
	}
	ctxMsg := taisce.RenderContextMessage(taisce.AssembledContext{Characters: 12, Segments: []byte(`[{"segment_id":"s"}]`)})
	if !strings.Contains(ctxMsg, `"plan":{"characters":12,"truncated":false}`) || !strings.Contains(ctxMsg, `"turns":[]`) {
		t.Fatalf("unexpected context rendering %q", ctxMsg)
	}
}
