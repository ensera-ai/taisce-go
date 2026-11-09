// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

// Command conformance is the adapter wrapped in the subprocess protocol the Taisce conformance suite
// speaks: one JSON instruction per line on stdin, one report per line on stdout.
//
// The agent is the framework's own agent.Agent with the providers attached; the model is a stub that
// records what it was handed and answers the turn's reply, or fails when the turn says so. Nothing
// about the deployment is stubbed. Run it as the suite's driver:
//
//	taisce conformance --api … --cases cases.json --driver "go run ./conformance"
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"

	"github.com/ensera-ai/taisce-go"
	"github.com/ensera-ai/taisce-go/agentframework"
)

type instruction struct {
	Case          string `json:"case"`
	Action        string `json:"action"`
	API           string `json:"api"`
	Token         string `json:"token"`
	DataSubjectID string `json:"data_subject_id"`
	Turn          struct {
		History      []said                          `json:"history"`
		User         string                          `json:"user"`
		ToolCalls    []struct{ Call, Result string } `json:"tool_calls"`
		Synthetic    []said                          `json:"synthetic"`
		Assistant    string                          `json:"assistant"`
		ModelFailure bool                            `json:"model_failure"`
		Compact      bool                            `json:"compact"`
	} `json:"turn"`
}

type said struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Untrusted bool   `json:"untrusted,omitempty"`
}

type erasure struct {
	Deleted  map[string]int `json:"deleted,omitempty"`
	Residual map[string]int `json:"residual,omitempty"`
	Clean    bool           `json:"clean"`
	Error    string         `json:"error,omitempty"`
}

type report struct {
	ModelMessages []said   `json:"model_messages"`
	Fatal         bool     `json:"fatal"`
	Observed      bool     `json:"observed"`
	StoreError    string   `json:"store_error,omitempty"`
	Erasure       *erasure `json:"erasure,omitempty"`
}

func main() {
	if err := serve(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(ctx context.Context) error {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 16<<20)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var ins instruction
		if err := json.Unmarshal([]byte(line), &ins); err != nil {
			return err
		}
		r, err := run(ctx, ins)
		if err != nil {
			return err
		}
		if err := out.Encode(r); err != nil {
			return err
		}
	}
	return in.Err()
}

// run takes one instruction. An action this driver does not know is an error rather than an empty
// turn, because a turn nobody asked for would be stored.
func run(ctx context.Context, ins instruction) (report, error) {
	switch ins.Action {
	case "":
		return runTurn(ctx, ins)
	case "erase":
		return runErase(ctx, ins), nil
	default:
		return report{}, fmt.Errorf("unknown action %q", ins.Action)
	}
}

// runErase erases the person through the client this adapter is built on and reports the receipt as
// the client handed it over, with the error it returned when it returned one.
func runErase(ctx context.Context, ins instruction) report {
	client, err := taisce.New(ins.API, ins.Token)
	if err != nil {
		return report{Erasure: &erasure{Error: err.Error()}}
	}
	receipt, err := client.EraseSubject(ctx, ins.DataSubjectID, "conformance")
	var notClean *taisce.ErasureNotCleanError
	switch {
	case errors.As(err, &notClean):
		return report{Erasure: &erasure{Deleted: notClean.Receipt.Deleted, Residual: notClean.Receipt.Residual, Clean: notClean.Receipt.Clean, Error: err.Error()}}
	case err != nil:
		return report{Erasure: &erasure{Error: err.Error()}}
	}
	return report{Erasure: &erasure{Deleted: receipt.Deleted, Residual: receipt.Residual, Clean: receipt.Clean}}
}

// watching marks the report observed when the adapter sends a store, whatever the answer.
type watching struct {
	next     http.RoundTripper
	observed *atomic.Bool
}

func (w watching) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/observations") {
		w.observed.Store(true)
	}
	return w.next.RoundTrip(r)
}

func runTurn(ctx context.Context, ins instruction) (report, error) {
	r := report{ModelMessages: []said{}}
	var observed atomic.Bool
	client, err := taisce.New(ins.API, ins.Token, taisce.WithHTTPClient(&http.Client{
		Transport:     watching{next: http.DefaultTransport, observed: &observed},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}))
	if err != nil {
		return r, err
	}
	onError := func(stage string, err error) {
		if stage == "observe" {
			r.StoreError = err.Error()
		}
	}
	provider, err := agentframework.NewContextProvider(agentframework.Config{
		Client: client, DataSubjectID: ins.DataSubjectID, RunID: ins.Case, OnError: onError,
	})
	if err != nil {
		return r, err
	}
	providers := []agent.ContextProvider{provider}
	if ins.Turn.Compact {
		compaction, err := agentframework.NewCompaction(agentframework.CompactionConfig{
			Client: client, DataSubjectID: ins.DataSubjectID, AfterMessages: 1, OnError: onError,
		})
		if err != nil {
			return r, err
		}
		providers = []agent.ContextProvider{compaction, provider}
	}

	a := agent.New(agent.ProviderConfig{ProviderName: "conformance-stub", Run: stub(ins, &r)}, agent.Config{ContextProviders: providers})
	session, err := a.CreateSession(ctx)
	if err != nil {
		return r, err
	}
	// The framework's own in-memory history holds the earlier turns the application kept.
	var history []*message.Message
	for _, h := range ins.Turn.History {
		history = append(history, text(h.Role, h.Content))
	}
	if err := session.SetInMemoryHistory("", history); err != nil {
		return r, err
	}
	var input []*message.Message
	for _, s := range ins.Turn.Synthetic {
		input = append(input, text(s.Role, s.Content))
	}
	input = append(input, text("user", ins.Turn.User))

	for _, err := range a.Run(ctx, input, agent.WithSession(session)) {
		if err != nil {
			// The run's error reaches the application; whether it was the store is in the report.
			r.Fatal = r.StoreError == ""
		}
	}
	r.Observed = observed.Load()
	return r, nil
}

// stub is the model: it records what it was handed, then answers the turn's tool traffic and reply,
// or fails.
func stub(ins instruction, r *report) agent.RunFunc {
	return func(ctx context.Context, messages []*message.Message, _ ...agent.Option) iter.Seq2[*agent.ResponseUpdate, error] {
		return func(yield func(*agent.ResponseUpdate, error) bool) {
			r.ModelMessages = r.ModelMessages[:0]
			for _, m := range messages {
				untrusted, _ := m.AdditionalProperties[agentframework.UntrustedProperty].(bool)
				r.ModelMessages = append(r.ModelMessages, said{Role: string(m.Role), Content: m.Contents.Text(), Untrusted: untrusted})
			}
			if ins.Turn.ModelFailure {
				yield(nil, errors.New("the model failed"))
				return
			}
			for i, t := range ins.Turn.ToolCalls {
				id := fmt.Sprintf("call-%d", i+1)
				args, _ := json.Marshal(map[string]string{"call": t.Call})
				if !yield(&agent.ResponseUpdate{MessageID: id + "-call", Role: message.RoleAssistant,
					Contents: message.Contents{&message.FunctionCallContent{CallID: id, Name: "tool", Arguments: string(args)}}}, nil) {
					return
				}
				if !yield(&agent.ResponseUpdate{MessageID: id + "-result", Role: message.RoleTool,
					Contents: message.Contents{&message.FunctionResultContent{CallID: id, Result: t.Result}}}, nil) {
					return
				}
			}
			if ins.Turn.Assistant != "" {
				yield(&agent.ResponseUpdate{MessageID: "reply", Role: message.RoleAssistant,
					Contents: message.Contents{&message.TextContent{Text: ins.Turn.Assistant}}}, nil)
			}
		}
	}
}

func text(role, content string) *message.Message {
	m := message.NewText(content)
	m.Role = message.Role(role)
	return m
}
