<!-- Copyright 2026 The Taisce Authors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Security

## Reporting a vulnerability

Report it privately through GitHub's
[private vulnerability reporting](https://github.com/ensera-ai/taisce-go/security/advisories/new)
for this repository, not in a public issue.

## Where this code runs, and what it holds

The adapter runs inside your agent process, beside a model, with your project credential. That is
the most privileged position in a Taisce deployment, and these rules follow from it:

- **The credential never crosses the network in clear.** `taisce.New` refuses an address that is
  not `https`, unless its host is loopback. A redirect is never followed, so the credential is
  never sent a second time, to anybody.
- **What memory says is untrusted.** Recalled text was written by whoever the agent talked to. It
  reaches the model as one `user` message marked untrusted, never as a system message.
- **No model can erase or export.** Nothing in `agentframework` offers either. The client's
  `EraseSubject`, `EraseSources`, `ExportSubject` and `ExportSources` are for your application's own
  code, with a `read_write` key. The agent process should hold an `agent` key, which cannot erase.

## How a version is published

A Go module is published by its tag, and nothing is uploaded anywhere: the Go module proxy fetches
the tagged commit, and the checksum database at `sum.golang.org` records the module's hash in a
public, append-only log the first time anybody fetches that version. `go` refuses a download whose
hash disagrees with the log, so a tag that is moved after it has been fetched is caught by every
user, not just by this project. Tags are made only by a maintainer, on a commit whose CI passed.

## Limits

- The checksum database proves that everybody gets the same bytes for a version. It does not prove
  those bytes are safe, or that they came from a reviewed commit.
- The framework this adapts, `github.com/microsoft/agent-framework-go`, is at `v0.3.0` and its API may
  change in any minor release. CI tests the version `go.mod` names, and nothing newer.
