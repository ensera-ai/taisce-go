// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package taisce

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Opaque storage, owned by the person it is about.
//
// # Why a memory service stores bytes it never reads
//
// An agent has state that is not memory: a session, a plan, a scratch file. Put in the
// application's own database it is a second place the same person's data lives, and a deletion then
// has two places to sweep — one of which can produce a counted residual and one of which cannot. The
// honest answer to "is it gone" becomes "it is gone from the part we can measure".
//
// So an artifact is bytes under a subject. The deployment never parses them, expires them with that
// person's retention, and removes them with that person's erasure, on the same receipt as the facts
// formed from their conversations.

// ArtifactPut is one artifact being written.
type ArtifactPut struct {
	// ID is the caller's name for it, unique within the project.
	ID string `json:"id"`
	// ExpectedVersion is the version this write expects to replace, and is how two writers of the
	// same artifact cannot silently lose one of their writes. Empty is not last-write-wins: the
	// deployment replays an identical write and refuses a changed one, so a second writer who did
	// not read first is told rather than obeyed.
	ExpectedVersion string `json:"expected_version,omitempty"`
	// DataSubjectID is whose artifact it is. Required by every caller here, because without it the
	// deployment cannot tell whose it is and one person's state is reachable to another.
	DataSubjectID string `json:"data_subject_id"`
	// Kind is an opaque label the deployment lists by, so an operator can see one kind of stored
	// object apart from another.
	Kind string `json:"kind"`
	// Name is a label within the kind.
	Name string `json:"name"`
	// Content is the bytes. They travel as base64 and the deployment stores them as given.
	Content []byte `json:"content"`
}

// ArtifactReceipt is what a write returned, including the version the next write should expect.
type ArtifactReceipt struct {
	ID                  string    `json:"id"`
	Version             string    `json:"version"`
	DataSubjectID       string    `json:"data_subject_id"`
	Kind                string    `json:"kind"`
	Name                string    `json:"name"`
	SourceObservationID string    `json:"source_observation_id"`
	Bytes               int       `json:"bytes"`
	AuthoredBy          string    `json:"authored_by"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	ExpiresAt           time.Time `json:"expires_at"`
	// Replayed says the deployment recognised this write as one it had already performed.
	Replayed bool `json:"replayed"`
}

// Artifact is a stored artifact and its bytes.
type Artifact struct {
	ID                  string    `json:"id"`
	Version             string    `json:"version"`
	DataSubjectID       string    `json:"data_subject_id"`
	Kind                string    `json:"kind"`
	Name                string    `json:"name"`
	SourceObservationID string    `json:"source_observation_id"`
	Bytes               int       `json:"bytes"`
	AuthoredBy          string    `json:"authored_by"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	ExpiresAt           time.Time `json:"expires_at"`
	Content             []byte    `json:"content"`
}

// ArtifactRef names one artifact, and whose it is.
type ArtifactRef struct {
	ID              string `json:"id"`
	DataSubjectID   string `json:"data_subject_id"`
	ExpectedVersion string `json:"expected_version,omitempty"`
}

// ErrArtifactNotFound is an artifact this subject does not have.
//
// A sentinel rather than a status, because "this person has not been here before" is the ordinary
// first turn of every conversation and a caller should not have to read HTTP codes to handle it.
// Another person's artifact answers the same way as one that does not exist, which is the check that
// makes one end user's state unreachable to another inside a project a single credential opens.
var ErrArtifactNotFound = errors.New("taisce: no such artifact for this subject")

// ErrArtifactConflict is a write whose expected version is not the stored one.
var ErrArtifactConflict = errors.New("taisce: the artifact changed since the version this write expected")

// PutArtifact writes one artifact under one subject.
func (c *Client) PutArtifact(ctx context.Context, req ArtifactPut) (ArtifactReceipt, error) {
	if strings.TrimSpace(req.ID) == "" {
		return ArtifactReceipt{}, errors.New("taisce: an artifact has an id")
	}
	if strings.TrimSpace(req.DataSubjectID) == "" {
		return ArtifactReceipt{}, errArtifactSubject
	}
	var out ArtifactReceipt
	return out, c.post(ctx, "/v1/artifacts/put", req, 200, &out)
}

// GetArtifact reads one subject's artifact. It returns ErrArtifactNotFound when there is none.
func (c *Client) GetArtifact(ctx context.Context, ref ArtifactRef) (Artifact, error) {
	if strings.TrimSpace(ref.ID) == "" {
		return Artifact{}, errors.New("taisce: an artifact has an id")
	}
	if strings.TrimSpace(ref.DataSubjectID) == "" {
		return Artifact{}, errArtifactSubject
	}
	var out Artifact
	err := c.post(ctx, "/v1/artifacts/get", struct {
		ID            string `json:"id"`
		DataSubjectID string `json:"data_subject_id"`
	}{ref.ID, ref.DataSubjectID}, 200, &out)
	return out, err
}

// DeleteArtifact removes one subject's artifact. An artifact that is not there is already removed,
// so that is not an error.
func (c *Client) DeleteArtifact(ctx context.Context, ref ArtifactRef) error {
	if strings.TrimSpace(ref.ID) == "" {
		return errors.New("taisce: an artifact has an id")
	}
	if strings.TrimSpace(ref.DataSubjectID) == "" {
		return errArtifactSubject
	}
	err := c.post(ctx, "/v1/artifacts/delete", ref, 200, nil)
	if errors.Is(err, ErrArtifactNotFound) {
		return nil
	}
	return err
}

// ArtifactListing is one page of a subject's artifacts, without their bytes.
type ArtifactListing struct {
	Artifacts []Artifact `json:"artifacts"`
	// Next carries the id to continue after, and is absent on the last page.
	Next *struct {
		ID string `json:"id"`
	} `json:"next"`
}

// ListArtifacts lists one subject's artifacts of a kind, newest page first by the deployment's
// order. Bytes are not returned.
func (c *Client) ListArtifacts(ctx context.Context, dataSubjectID, kind, namePrefix, after string, limit int) (ArtifactListing, error) {
	if strings.TrimSpace(dataSubjectID) == "" {
		return ArtifactListing{}, errArtifactSubject
	}
	var out ArtifactListing
	return out, c.post(ctx, "/v1/artifacts/list", struct {
		Kind          string `json:"kind,omitempty"`
		NamePrefix    string `json:"name_prefix,omitempty"`
		DataSubjectID string `json:"data_subject_id"`
		After         string `json:"after,omitempty"`
		Limit         int    `json:"limit,omitempty"`
	}{kind, namePrefix, dataSubjectID, after, limit}, 200, &out)
}

// Required here rather than left to the deployment, because the message is the thing worth saying:
// the subject is what makes one person's state unreachable to another, and a caller who omitted it
// has written the bug this sentence names.
var errArtifactSubject = errors.New(
	"taisce: an artifact belongs to a person: without a subject, another person's artifact is reachable")
