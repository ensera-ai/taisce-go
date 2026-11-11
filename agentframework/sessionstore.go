// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package agentframework

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/microsoft/agent-framework-go/agent"

	"github.com/ensera-ai/taisce-go"
)

// SessionStore keeps an agent session where the memory it belongs to already lives.
//
// # Why this is not a convenience
//
// The framework marshals a session and hands it back; it ships no durable store, so every
// application invents one. Inventing it in the application's own database is the expensive mistake,
// and the reason is governance rather than tidiness: a session and the memory formed from it are
// erased by the same request from the same person. Split across two systems, a deletion has two
// places to sweep, one of which can produce a counted residual and one of which cannot, and the
// honest answer to "is it gone" becomes "it is gone from the part we can measure".
//
// So a session is an opaque object under the person it belongs to. The deployment never reads it,
// expires it with that person's retention, and removes it with that person's erasure, on the same
// receipt as the facts formed from their conversations.
//
// # Whose session it is, is checked by the deployment
//
// One credential opens a project and a project holds every end user's objects, so an application
// serving many people could hand one person's session to another; nothing in the application is a
// boundary against that, only care. Every call here names the subject, and the deployment answers
// another person's session exactly as one that does not exist.
//
// # Concurrency is the caller's to declare
//
// A save carries the version it expects to replace and the deployment refuses a stale one, so two
// turns of the same session cannot silently lose one of their writes. Passing no version is not
// last-write-wins: the deployment replays an identical save and refuses a changed one, so a second
// writer who did not read first is told rather than obeyed. Naming the version you expect to replace
// is still what to do.
//
// # What this does not do
//
// It does not run inside the framework's history seam. An application decides when a session is
// worth persisting — which is usually after a run, and sometimes not at all — and a provider that
// wrote on every run would make that decision for it, invisibly, at the cost of a request per turn.
type SessionStore struct {
	client *taisce.Client
	kind   string
}

// DefaultSessionKind is the object kind sessions are stored under. The deployment treats it as an
// opaque label and lists by it, which is what lets an operator see sessions apart from other stored
// objects.
const DefaultSessionKind = "state"

// NewSessionStore builds a store over one deployment. An empty kind takes DefaultSessionKind.
func NewSessionStore(client *taisce.Client, kind string) (*SessionStore, error) {
	if client == nil {
		return nil, errors.New("agentframework: a client is required")
	}
	if strings.TrimSpace(kind) == "" {
		kind = DefaultSessionKind
	}
	return &SessionStore{client: client, kind: kind}, nil
}

// SavedSession is what a save returned: the version the next save should expect to replace.
type SavedSession struct {
	SessionID string
	Version   string
}

// LoadedSession is a session read back, with the version it was read at.
type LoadedSession struct {
	Session *agent.Session
	Version string
}

// Save marshals a session and stores it under the person it belongs to.
//
// expectedVersion is the version this save expects to replace; empty on the first save of a session.
func (s *SessionStore) Save(ctx context.Context, session *agent.Session,
	sessionID, dataSubjectID, expectedVersion string) (SavedSession, error) {
	if session == nil {
		return SavedSession{}, errors.New("agentframework: there is no session to save")
	}
	// Marshalled by value, which the framework supports and documents: the snapshot is shallow and
	// is taken now, so a run that mutates the session afterwards is a later save and not this one.
	state, err := json.Marshal(session)
	if err != nil {
		return SavedSession{}, err
	}
	receipt, err := s.client.PutArtifact(ctx, taisce.ArtifactPut{
		ID:              sessionID,
		ExpectedVersion: expectedVersion,
		DataSubjectID:   dataSubjectID,
		Kind:            s.kind,
		Name:            "session",
		Content:         state,
	})
	if err != nil {
		return SavedSession{}, err
	}
	return SavedSession{SessionID: receipt.ID, Version: receipt.Version}, nil
}

// Load reads a person's session back. found is false when that person has no session by that id.
//
// Not an error, because "this person has not been here before" is the ordinary first turn of every
// conversation and not something anybody should have to catch. A session belonging to somebody else
// is also not found, which is the check that makes one person's state unreachable to another.
func (s *SessionStore) Load(ctx context.Context, sessionID, dataSubjectID string) (LoadedSession, bool, error) {
	stored, err := s.client.GetArtifact(ctx, taisce.ArtifactRef{ID: sessionID, DataSubjectID: dataSubjectID})
	if errors.Is(err, taisce.ErrArtifactNotFound) {
		return LoadedSession{}, false, nil
	}
	if err != nil {
		return LoadedSession{}, false, err
	}
	session := new(agent.Session)
	if err := json.Unmarshal(stored.Content, session); err != nil {
		// Reported rather than swallowed into an empty session. A session that silently came back
		// blank would read to the agent as a first turn, and the conversation it lost would be
		// nowhere — which is the one failure a store exists to prevent.
		return LoadedSession{}, false, err
	}
	return LoadedSession{Session: session, Version: stored.Version}, true, nil
}

// Delete removes a person's session. A session that is not there is already removed.
func (s *SessionStore) Delete(ctx context.Context, sessionID, dataSubjectID, expectedVersion string) error {
	return s.client.DeleteArtifact(ctx, taisce.ArtifactRef{
		ID: sessionID, DataSubjectID: dataSubjectID, ExpectedVersion: expectedVersion,
	})
}

// The identity and the subject are not checked here.
//
// The client refuses both, and its refusal says why — a session belongs to a person, and without one
// another person's session is reachable. Restating the rule would put it in two places, and the copy
// no caller reaches is the one that drifts. What is checked here is the one thing the client cannot
// see: whether there is a session to save at all.
