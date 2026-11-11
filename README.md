<!-- Copyright 2026 The Taisce Authors -->
<!-- SPDX-License-Identifier: Apache-2.0 -->

# Taisce for Go

Memory for agents built on the [Microsoft Agent Framework for Go](https://github.com/microsoft/agent-framework-go),
backed by a [Taisce](https://github.com/ensera-ai/taisce) deployment you run.

- `github.com/ensera-ai/taisce-go` is the client over the deployment's v1 contract: observe,
  freshness, recall, context, citations, passage search, artifacts, erasure and export.
- `github.com/ensera-ai/taisce-go/agentframework` is the adapter, on four of the framework's own
  seams:
  - `ContextProvider` recalls before a run and stores the turn after it;
  - `Compaction` replaces a long history with the deployment's context;
  - `SessionStore` keeps the framework's session where the memory formed from it already lives;
  - `NewTextSearchProvider` binds the deployment's passage search to the framework's text-search
    seam, for grounding without forming anything.

```go
client, err := taisce.New("https://memory.example", os.Getenv("TAISCE_TOKEN"))
if err != nil {
	return err
}
memory, err := agentframework.NewContextProvider(agentframework.Config{
	Client: client, DataSubjectID: "alice", RunID: sessionID,
	OnError: func(stage string, err error) { log.Printf("taisce %s: %v", stage, err) },
})
if err != nil {
	return err
}
// model is your model provider's agent.ProviderConfig.
a := agent.New(model, agent.Config{ContextProviders: []agent.ContextProvider{memory}})
```

Give the agent process an `agent` key (`taisce credential issue <name>`), which can store and recall
but cannot erase anybody.

## What the provider does, and holds to

- **Memory arrives as exactly one `user` message, marked untrusted.** Never a system message. The
  message is the line `taisce-memory/v1 untrusted` and a JSON document carrying the freshness
  watermark, what the recall did, and the recall's facts, reports and passages unchanged. It sits
  just before the person's latest message. Recall returning nothing injects nothing.
- **A recall failure is not fatal.** The agent runs without memory rather than not at all, and
  `OnError` is how an operator still finds out.
- **A write failure is never silent.** A failed store is returned from the run.
- **A failed turn is not a memory.** The store runs only after a run that succeeded.
- **Only what people said becomes memory by default.** The person's messages from the run's input
  and the assistant's final words are stored. History a history provider loaded is not, because it
  was stored when it happened. Nor are messages a provider added, tool calls, or a tool's answers.
- **A retried store is one observation, and a different turn is another.** The idempotency key is the
  conformance suite's `taisce-turn-key/2` derivation: every field is length-prefixed, so content
  cannot move a boundary between them. It is checked against the suite's published vectors and is
  the same bytes as every other adapter's.
- **Compaction is replacement, never a summary.** When the loaded history is longer than
  `AfterMessages`, `Compaction` asks the deployment for the person's context and puts it, unchanged,
  in place of that history. A context that cannot be fetched leaves the history as it was.
- **A session belongs to a person, and lives where their memory does.** The framework ships no
  durable session store, so every application invents one — and inventing it in the application's own
  database splits one person's data across two systems, which gives a deletion two places to sweep
  and only one that can produce a counted residual. `SessionStore` keeps the serialized session as an
  artifact under the subject: the deployment never reads it, expires it with that person's retention,
  and removes it with that person's erasure. Every call names the subject, and the deployment answers
  another person's session exactly as one that does not exist. A save carries the version it expects
  to replace, so two turns of one session cannot silently lose a write.
- **Passage search grounds without forming anything.** `NewTextSearchProvider` recalls nothing and
  stores nothing: it searches what was actually said and hands it to the model under the same
  untrusted mark the memory message carries, because a passage is something somebody said. When the
  deployment says the index behind the answer is still building, the model is told so in the text it
  reads — a model handed an incomplete answer as though it were complete answers confidently from
  it.

## Erasure and export

These are for your application's code, never the model's: nothing in `agentframework` offers them.

```go
receipt, err := client.EraseSubject(ctx, "alice", "right to erasure")
var notClean *taisce.ErasureNotCleanError
switch {
case errors.As(err, &notClean): // rows still match; notClean.Receipt says which
case errors.Is(err, taisce.ErrForbidden): // this key may not erase: use the read_write key
case err != nil:
}
everything, err := client.ExportSubject(ctx, "bob") // sections of rows, and a count per section
```

- `EraseSubject` and `EraseSources` are separate methods because the deployment refuses a request
  that names both a person and sources.
- **A receipt that is not clean is an error**, `*ErasureNotCleanError`, and it carries the whole
  receipt. Code that ignores the receipt cannot tell a person their data is gone while rows still
  match.
- `ErrExportTooLarge` means the person holds more rows than one export returns. The deployment
  refuses the export rather than returning part of it.

## The address must be https

`taisce.New` refuses an address that is not `https`, unless its host is loopback (`localhost`,
`127.0.0.0/8`, `::1`). The credential travels with every request. The client never follows a
redirect.

## How it is proved

The adapter conformance suite from the service repository runs against a live deployment, with this
repository's driver:

```sh
go build -o driver ./conformance
TAISCE_TOKEN=… taisce conformance --api http://localhost:8080 --cases ../taisce/conformance/cases.json --driver ./driver
```

- `go test ./...` holds what needs no deployment.
- `go test -tags live ./...` runs erasure and export against a deployment. It needs `TAISCE_API`,
  `TAISCE_TOKEN` (read_write) and `TAISCE_READER_TOKEN` (read_only), and fails rather than skips
  without them.
- The `conformance` workflow stands a deployment up and runs both.

## Versions

Built against `github.com/microsoft/agent-framework-go` v0.3.0, which is pre-1.0: its seam may change
in any minor release. Go 1.26 or later.
