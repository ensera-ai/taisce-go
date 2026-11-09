// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

//go:build live

package taisce_test

// These run against a live deployment and nothing else: `go test -tags live ./...` with TAISCE_API,
// TAISCE_TOKEN (read_write) and TAISCE_READER_TOKEN (read_only) set. A missing variable fails the
// run rather than skipping it, because a green run that skipped asserted nothing. The conformance
// workflow stands a deployment up and runs them.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ensera-ai/taisce-go"
)

func required(t *testing.T, name string) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		t.Fatalf("%s is not set: these tests run against a live deployment", name)
	}
	return v
}

func live(t *testing.T, token string) *taisce.Client {
	t.Helper()
	client, err := taisce.New(required(t, "TAISCE_API"), required(t, token))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func observe(t *testing.T, client *taisce.Client, subject, words string) string {
	t.Helper()
	turn := []taisce.Message{{Role: "user", Content: words}, {Role: "assistant", Content: "Noted."}}
	receipt, err := client.Observe(context.Background(), taisce.ObserveRequest{
		IdempotencyKey: taisce.TurnKey(subject, "live", turn), DataSubjectID: subject, Messages: turn})
	if err != nil {
		t.Fatal(err)
	}
	return receipt.ID
}

func total(rows map[string]int) int {
	n := 0
	for _, v := range rows {
		n += v
	}
	return n
}

func TestAnErasureBySubjectReturnsACleanReceiptAndLeavesNothingToExport(t *testing.T) {
	ctx, client, subject := context.Background(), live(t, "TAISCE_TOKEN"), "live-"+uuid.NewString()
	observe(t, client, subject, "My badge number is 4471.")
	receipt, err := client.EraseSubject(ctx, subject, "live test")
	if err != nil || !receipt.Clean || receipt.DataSubjectID != subject || total(receipt.Deleted) == 0 || total(receipt.Residual) != 0 {
		t.Fatalf("receipt %+v, %v", receipt, err)
	}
	left, err := client.ExportSubject(ctx, subject)
	if err != nil || total(left.Rows) != 0 {
		t.Fatalf("after the erasure the deployment exports %v, %v", left.Rows, err)
	}
}

func TestAnErasureBySourcesErasesTheObservationItNamesAndNotItsNeighbour(t *testing.T) {
	ctx, client, subject := context.Background(), live(t, "TAISCE_TOKEN"), "live-"+uuid.NewString()
	doomed := observe(t, client, subject, "Delete this sentence.")
	observe(t, client, subject, "Keep this one.")
	receipt, err := client.EraseSources(ctx, []string{doomed}, "live test")
	if err != nil || !receipt.Clean || len(receipt.SourceObservationIDs) != 1 || receipt.SourceObservationIDs[0] != doomed {
		t.Fatalf("receipt %+v, %v", receipt, err)
	}
	if gone, err := client.ExportSources(ctx, []string{doomed}); err != nil || total(gone.Rows) != 0 {
		t.Fatalf("the erased source still exports %v, %v", gone.Rows, err)
	}
	if kept, err := client.ExportSubject(ctx, subject); err != nil || total(kept.Rows) == 0 {
		t.Fatalf("the neighbour went too: %v, %v", kept.Rows, err)
	}
	_, _ = client.EraseSubject(ctx, subject, "live test cleanup")
}

func TestAReadOnlyKeyIsRefusedAnErasureAndAllowedAnExport(t *testing.T) {
	ctx, writer, reader, subject := context.Background(), live(t, "TAISCE_TOKEN"), live(t, "TAISCE_READER_TOKEN"), "live-"+uuid.NewString()
	observe(t, writer, subject, "Readers may look and not delete.")
	if _, err := reader.EraseSubject(ctx, subject, "live test"); !errors.Is(err, taisce.ErrForbidden) {
		t.Fatalf("a read-only key's erasure: %v", err)
	}
	if exported, err := reader.ExportSubject(ctx, subject); err != nil || total(exported.Rows) == 0 {
		t.Fatalf("a read-only key's export: %v, %v", exported.Rows, err)
	}
	_, _ = writer.EraseSubject(ctx, subject, "live test cleanup")
}

func TestAnExportsRowCountsMatchItsSections(t *testing.T) {
	ctx, client, subject := context.Background(), live(t, "TAISCE_TOKEN"), "live-"+uuid.NewString()
	observe(t, client, subject, "Count what you hand back.")
	exported, err := client.ExportSubject(ctx, subject)
	if err != nil || exported.DataSubjectID != subject {
		t.Fatalf("%+v, %v", exported, err)
	}
	if len(exported.Rows) != len(exported.Sections) {
		t.Fatalf("%d counts for %d sections", len(exported.Rows), len(exported.Sections))
	}
	for name, rows := range exported.Sections {
		if exported.Rows[name] != len(rows) {
			t.Errorf("%s: counted %d, holds %d", name, exported.Rows[name], len(rows))
		}
	}
	if exported.Rows["message"] < 2 {
		t.Fatalf("the turn's two messages are not in the export: %v", exported.Rows)
	}
	_, _ = client.EraseSubject(ctx, subject, "live test cleanup")
}
