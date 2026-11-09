// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce

import (
	"crypto/sha256"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// MemoryMessagePrefix is the first line of every memory message an adapter injects. The conformance
// suite parses what follows it, so every adapter renders the same document under the same line.
const MemoryMessagePrefix = "taisce-memory/v1 untrusted"

// TurnKeyDerivation is the derivation TurnKey implements, and the first field it hashes, so a key
// from it never equals one from another derivation.
const TurnKeyDerivation = "taisce-turn-key/2"

// TurnKey is a turn's idempotency key: a version-5 UUID over SHA-256 of netstrings — length in UTF-8
// bytes, a colon, the bytes, a comma — of the derivation, the subject, the run, and each message's
// role and content. A retried turn derives the same key and is stored once; a different turn derives
// another.
//
// The lengths are the point. Fields joined by a separator let content holding the separator and a
// role name move a boundary, so two different turns hashed the same bytes and the deployment refused
// the second as a conflicting retry, for good. Go's len counts UTF-8 bytes already; another
// language's string length may not, which is why the vectors this is tested against include a
// character outside the Basic Multilingual Plane.
func TurnKey(dataSubjectID, runID string, messages []Message) string {
	h := sha256.New()
	netstring := func(field string) {
		h.Write([]byte(strconv.Itoa(len(field))))
		h.Write([]byte{':'})
		h.Write([]byte(field))
		h.Write([]byte{','})
	}
	netstring(TurnKeyDerivation)
	netstring(dataSubjectID)
	netstring(runID)
	for _, m := range messages {
		netstring(m.Role)
		netstring(m.Content)
	}
	var id uuid.UUID
	copy(id[:], h.Sum(nil)[:16])
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	return id.String()
}

type watermark struct {
	Stored *int64 `json:"stored"`
	Formed *int64 `json:"formed"`
	Parked int    `json:"parked"`
}

// RenderMemoryMessage is the prefix line, then a JSON document with the watermark, the plan (what
// the recall did) and the recall's own arrays unchanged.
func RenderMemoryMessage(fresh Freshness, bundle RecallBundle) string {
	doc := struct {
		Watermark watermark `json:"watermark"`
		Plan      struct {
			Controls json.RawMessage `json:"controls"`
			Degraded json.RawMessage `json:"degraded"`
			Reach    json.RawMessage `json:"reach"`
		} `json:"plan"`
		Facts    json.RawMessage `json:"facts"`
		Reports  json.RawMessage `json:"reports"`
		Passages json.RawMessage `json:"passages"`
	}{Watermark: watermark{fresh.Stored, fresh.Formed, fresh.Parked}}
	doc.Plan.Controls = orNull(bundle.Controls)
	doc.Plan.Degraded = orEmpty(bundle.Degraded)
	doc.Plan.Reach = orNull(bundle.Reach)
	doc.Facts, doc.Reports, doc.Passages = orEmpty(bundle.Facts), orEmpty(bundle.Reports), orEmpty(bundle.Passages)
	return render(doc)
}

// RenderContextMessage is the prefix line, then a JSON document with the watermark, the assembly's
// cost as the plan, and the context's segments and turns unchanged: what replaces a history is the
// deployment's answer and nothing an adapter wrote.
func RenderContextMessage(assembled AssembledContext) string {
	doc := struct {
		Watermark watermark `json:"watermark"`
		Plan      struct {
			Characters int  `json:"characters"`
			Truncated  bool `json:"truncated"`
		} `json:"plan"`
		Segments json.RawMessage `json:"segments"`
		Turns    json.RawMessage `json:"turns"`
	}{Watermark: watermark{assembled.Watermark.Stored, assembled.Watermark.Formed, assembled.Watermark.Parked}}
	doc.Plan.Characters, doc.Plan.Truncated = assembled.Characters, assembled.Truncated
	doc.Segments, doc.Turns = orEmpty(assembled.Segments), orEmpty(assembled.Turns)
	return render(doc)
}

// IsMemoryText reports whether a message's text is one an adapter injected, by its first line.
func IsMemoryText(text string) bool { return strings.HasPrefix(text, MemoryMessagePrefix) }

func render(doc any) string {
	raw, err := json.Marshal(doc)
	if err != nil {
		// Every field is either a fixed type or JSON the deployment sent and json.Unmarshal accepted,
		// so a failure here is a bug in this file, not an input to handle.
		panic("taisce: rendering a memory message: " + err.Error())
	}
	return MemoryMessagePrefix + "\n" + string(raw)
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("[]")
	}
	return raw
}

func orNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
