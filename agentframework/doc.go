// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

// Package agentframework connects the Microsoft Agent Framework for Go to a Taisce deployment, at the
// framework's own seam: a context provider that recalls before a run and observes after it, and a
// second that replaces a long history with the deployment's context.
//
// It decides what the model is handed from memory and what of a run is stored. It must not decide
// what memory holds, rank what a recall returns, or summarise anything itself: an adapter that
// decides things locally is how behaviour forks per language. The conformance suite holds it to the
// same rules as every other adapter.
//
// Nothing here offers erasure or export to a model. Those are the application's, through the client,
// with a key the agent process does not hold.
package agentframework
