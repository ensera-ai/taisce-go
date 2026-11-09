// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Passage search: what was said, by similarity, with enough identity to cite.
//
// It is the other half of retrieval from a recall. A recall answers from entities — what was
// inferred about a person — and this answers from the words themselves. It infers nothing, stores
// nothing, and forms nothing, which is what makes it the on-ramp for a developer who wants grounding
// without first accepting that a memory service should form facts from conversations.

// PassageRequest asks for the passages nearest a question. A zero control is the deployment's
// default, and the deployment bounds every one of them regardless.
type PassageRequest struct {
	Question string `json:"question"`
	Limit    int    `json:"limit,omitempty"`
	// Candidates is how wide the deployment's own search goes before it keeps Limit of them.
	Candidates int `json:"candidates,omitempty"`
	// DataSubjectID narrows the search to one person's words. A passage is somebody's words, so a
	// search narrowed to one person must not quote anybody else.
	DataSubjectID string `json:"data_subject_id,omitempty"`
	// SourceRole is whose messages to search; the deployment's default is the person's own.
	SourceRole string `json:"source_role,omitempty"`
}

// Passage is one message reached by similarity. It is evidence and never a fact.
type Passage struct {
	ChunkID    string    `json:"chunk_id"`
	SourceID   string    `json:"source_id"`
	Ordinal    int       `json:"ordinal"`
	Role       string    `json:"role"`
	Preview    string    `json:"preview"`
	Similarity float64   `json:"similarity"`
	OccurredAt time.Time `json:"occurred_at"`
	// MessageBytes is the whole message's length, and PreviewComplete says whether the preview is
	// all of it — so a reader knows whether the sentence continues past what they were given.
	MessageBytes    int    `json:"message_bytes"`
	PreviewComplete bool   `json:"preview_complete"`
	PreviewByteEnd  int    `json:"preview_byte_end"`
	ContentDigest   string `json:"content_digest"`
}

// PassageResults is what the search returned, and how complete the index behind it was.
type PassageResults struct {
	GenerationID         string `json:"generation_id"`
	ThroughOffset        int64  `json:"through_offset"`
	CoveredThroughOffset int64  `json:"covered_through_offset"`
	BuildState           string `json:"build_state"`
	// Approximate says the index is still being built, so these are some of what was said and not
	// necessarily all of it. It is reported rather than smoothed over, because a model handed an
	// incomplete answer as though it were complete answers confidently from it.
	Approximate bool      `json:"approximate"`
	Passages    []Passage `json:"passages"`
}

// SearchPassages returns the passages nearest a question.
func (c *Client) SearchPassages(ctx context.Context, req PassageRequest) (PassageResults, error) {
	if strings.TrimSpace(req.Question) == "" {
		return PassageResults{}, errors.New("taisce: a passage search asks a question")
	}
	var out PassageResults
	return out, c.post(ctx, "/v1/passages/search", req, 200, &out)
}
