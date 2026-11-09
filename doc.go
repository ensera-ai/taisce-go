// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

// Package taisce is the client for a Taisce memory deployment over its v1 contract, and the
// renderings every adapter shares: the memory message and the turn key.
//
// It decides how a request is sent and how an answer is read, and nothing about any agent
// framework; the adapter in package agentframework is built on it. It must not decide what is
// remembered or how a recall is ranked: that is the deployment's, and a client that second-guessed
// it would be a second memory system with no proof behind it.
//
// The credential is a bearer token bound to one project. It is sent to the one deployment the client
// was built for, never over plaintext to a host that is not loopback, never across a redirect, and
// never logged.
package taisce
