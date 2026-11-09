// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Message is one message of a turn as it is observed: who said it, and what they said.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ObserveRequest records a turn.
type ObserveRequest struct {
	IdempotencyKey string    `json:"idempotency_key"`
	DataSubjectID  string    `json:"data_subject_id,omitempty"`
	Messages       []Message `json:"messages"`
	// OccurredAt is RFC 3339, or empty for the deployment's own clock.
	OccurredAt string `json:"occurred_at,omitempty"`
}

// ObserveReceipt is the observation as stored: durable when Observe returns; formation follows.
type ObserveReceipt struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	LogOffset int64  `json:"log_offset"`
}

// Observe records a turn. A retry with the same key is folded into what is already held; the same
// key with a different turn is refused, with Code "idempotency_conflict", and is never stored.
func (c *Client) Observe(ctx context.Context, req ObserveRequest) (ObserveReceipt, error) {
	if len(req.Messages) == 0 {
		return ObserveReceipt{}, errors.New("taisce: a turn has at least one message")
	}
	var out ObserveReceipt
	return out, c.post(ctx, "/v1/observations", req, 201, &out)
}

// Freshness is how far behind memory is. Stored and Formed are absent before the first turn is
// stored or formed, and absent is not zero.
type Freshness struct {
	Scope  string `json:"scope"`
	Stored *int64 `json:"stored"`
	Formed *int64 `json:"formed"`
	Parked int    `json:"parked"`
}

// Freshness reads the watermark.
func (c *Client) Freshness(ctx context.Context) (Freshness, error) {
	var out Freshness
	return out, c.get(ctx, "/v1/freshness", 200, &out)
}

// RecallRequest asks memory a question. The zero value of every control means the deployment's
// default.
type RecallRequest struct {
	Question      string   `json:"question"`
	DataSubjectID string   `json:"data_subject_id,omitempty"`
	MaxCharacters int      `json:"max_characters,omitempty"`
	SourceRoles   []string `json:"source_roles,omitempty"`
	AsOf          string   `json:"as_of,omitempty"`
	AsKnownAt     string   `json:"as_known_at,omitempty"`
}

// RecallBundle is a recall's answer. Its arrays are kept as the deployment sent them: an adapter
// hands them to a model unchanged, and a client that decoded them would be the place a field added
// by the deployment went missing.
type RecallBundle struct {
	Controls json.RawMessage `json:"controls"`
	Degraded json.RawMessage `json:"degraded"`
	Reach    json.RawMessage `json:"reach"`
	Facts    json.RawMessage `json:"facts"`
	Reports  json.RawMessage `json:"reports"`
	Passages json.RawMessage `json:"passages"`
}

// Empty reports whether the recall returned nothing at all: no facts, no reports, no passages.
func (b RecallBundle) Empty() bool {
	return emptyArray(b.Facts) && emptyArray(b.Reports) && emptyArray(b.Passages)
}

func emptyArray(raw json.RawMessage) bool {
	var items []json.RawMessage
	return len(raw) == 0 || json.Unmarshal(raw, &items) != nil || len(items) == 0
}

// Recall asks memory a question.
func (c *Client) Recall(ctx context.Context, req RecallRequest) (RecallBundle, error) {
	if strings.TrimSpace(req.Question) == "" {
		return RecallBundle{}, errors.New("taisce: a recall asks a question")
	}
	var out RecallBundle
	return out, c.post(ctx, "/v1/recalls", req, 200, &out)
}

// ContextRequest asks for one subject's history under a budget.
type ContextRequest struct {
	DataSubjectID string `json:"data_subject_id"`
	MaxCharacters int    `json:"max_characters,omitempty"`
}

// AssembledContext is the newest turns verbatim and, over the rest, the segments the deployment
// wrote. Segments and turns are kept as sent, for the reason RecallBundle's arrays are.
type AssembledContext struct {
	Watermark  Freshness       `json:"watermark"`
	Segments   json.RawMessage `json:"segments"`
	Turns      json.RawMessage `json:"turns"`
	Characters int             `json:"characters"`
	Truncated  bool            `json:"truncated"`
}

// Context assembles one subject's history. No model call is made.
func (c *Client) Context(ctx context.Context, req ContextRequest) (AssembledContext, error) {
	if strings.TrimSpace(req.DataSubjectID) == "" {
		return AssembledContext{}, errors.New("taisce: a context is one subject's history, and names it")
	}
	var out AssembledContext
	return out, c.post(ctx, "/v1/contexts", req, 200, &out)
}

// ResolveCitation resolves a fact to the record and the words it stands on. The answer is returned
// as sent.
func (c *Client) ResolveCitation(ctx context.Context, factID string, limit int) (json.RawMessage, error) {
	if strings.TrimSpace(factID) == "" {
		return nil, errors.New("taisce: a citation names a fact")
	}
	body := map[string]any{"id": factID}
	if limit > 0 {
		body["limit"] = limit
	}
	var out json.RawMessage
	return out, c.post(ctx, "/v1/citations/resolve", body, 200, &out)
}
