// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

// Package agentframework connects the Microsoft Agent Framework for Go to a Taisce deployment, on
// four of the framework's own seams: a context provider that recalls before a run and observes after
// it, a second that replaces a long history with the deployment's context, a store that keeps the
// framework's session where the memory formed from it already lives, and a binding of the
// deployment's passage search to the framework's text-search seam.
//
// It decides what the model is handed from memory and what of a run is stored. It must not decide
// what memory holds, rank what a recall returns, or summarise anything itself: an adapter that
// decides things locally is how behaviour forks per language. The conformance suite holds it to the
// same rules as every other adapter.
//
// The session store is the one seam an application drives rather than the framework: when a session
// is worth persisting is the application's decision, and a provider that wrote on every run would
// make it invisibly, at the cost of a request per turn.
//
// Nothing here offers erasure or export to a model. Those are the application's, through the client,
// with a key the agent process does not hold.
package agentframework
