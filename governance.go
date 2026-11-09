// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxErasureSources is the most source observations one erasure may name. The deployment refuses
// more; refusing here says so before a request is spent on it.
const MaxErasureSources = 10000

// ErasureReceipt is what an erasure did, as the deployment counted it. Residual is the product: a
// count of what still matches, taken after the delete in the same transaction. Clean is that every
// residual is zero.
type ErasureReceipt struct {
	RequestID            string         `json:"request_id"`
	Scope                string         `json:"scope"`
	DataSubjectID        string         `json:"data_subject_id"`
	SourceObservationIDs []string       `json:"source_observation_ids,omitempty"`
	CompletedAt          time.Time      `json:"completed_at"`
	Deleted              map[string]int `json:"deleted"`
	Residual             map[string]int `json:"residual"`
	Clean                bool           `json:"clean"`
}

// ErasureNotCleanError is an erasure that completed with rows still matching what it named. It is
// returned as an error rather than as a receipt with Clean false, so that an application which
// never reads the receipt cannot tell a person their data is gone while it is not. The receipt is
// carried whole.
type ErasureNotCleanError struct {
	Receipt ErasureReceipt
}

func (e *ErasureNotCleanError) Error() string {
	left := map[string]int{}
	for kind, n := range e.Receipt.Residual {
		if n != 0 {
			left[kind] = n
		}
	}
	return fmt.Sprintf("taisce: the erasure left rows matching what it named: %v", left)
}

// EraseSubject erases everything held for one person and returns the receipt.
//
// It is a separate method from EraseSources because the deployment refuses a request naming both a
// person and sources: the receipt says what was asked, and it was one thing. Give it the read_write
// key your erasure path holds; an agent key is refused by design, with ErrForbidden.
func (c *Client) EraseSubject(ctx context.Context, dataSubjectID, reason string) (ErasureReceipt, error) {
	if strings.TrimSpace(dataSubjectID) == "" {
		return ErasureReceipt{}, errors.New("taisce: an erasure names the person it is for; one naming nobody would be a scope-wide delete")
	}
	return c.erase(ctx, map[string]any{"data_subject_id": dataSubjectID, "reason": reason})
}

// EraseSources erases what the named observations said and everything formed from them.
func (c *Client) EraseSources(ctx context.Context, sourceObservationIDs []string, reason string) (ErasureReceipt, error) {
	if err := checkSources(sourceObservationIDs); err != nil {
		return ErasureReceipt{}, err
	}
	return c.erase(ctx, map[string]any{"source_observation_ids": sourceObservationIDs, "reason": reason})
}

func (c *Client) erase(ctx context.Context, body map[string]any) (ErasureReceipt, error) {
	var receipt ErasureReceipt
	if err := c.post(ctx, "/v1/erasures", body, 200, &receipt); err != nil {
		return ErasureReceipt{}, err
	}
	if !receipt.Clean {
		return receipt, &ErasureNotCleanError{Receipt: receipt}
	}
	return receipt, nil
}

// Export is everything held for a person or a set of sources. Sections are raw rows keyed by what
// produced them, unshaped so that a column a migration added is in the export without anybody
// remembering to add it here; Rows counts each section, so an export can be compared with an
// erasure receipt without parsing either.
type Export struct {
	Scope                string                       `json:"scope"`
	DataSubjectID        string                       `json:"data_subject_id"`
	SourceObservationIDs []string                     `json:"source_observation_ids,omitempty"`
	Sections             map[string][]json.RawMessage `json:"sections"`
	Rows                 map[string]int               `json:"rows"`
}

// ExportSubject returns everything held for one person, or ErrExportTooLarge rather than part of it.
func (c *Client) ExportSubject(ctx context.Context, dataSubjectID string) (Export, error) {
	if strings.TrimSpace(dataSubjectID) == "" {
		return Export{}, errors.New("taisce: an export names the person it is for")
	}
	var out Export
	return out, c.post(ctx, "/v1/exports", map[string]any{"data_subject_id": dataSubjectID}, 200, &out)
}

// ExportSources returns everything held from the named observations.
func (c *Client) ExportSources(ctx context.Context, sourceObservationIDs []string) (Export, error) {
	if err := checkSources(sourceObservationIDs); err != nil {
		return Export{}, err
	}
	var out Export
	return out, c.post(ctx, "/v1/exports", map[string]any{"source_observation_ids": sourceObservationIDs}, 200, &out)
}

func checkSources(ids []string) error {
	if len(ids) == 0 {
		return errors.New("taisce: a request by source names at least one observation")
	}
	if len(ids) > MaxErasureSources {
		return fmt.Errorf("taisce: a request names at most %d source observations", MaxErasureSources)
	}
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("taisce: %q is not an observation id", id)
		}
	}
	return nil
}
