// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package agentframework

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"

	"github.com/ensera-ai/taisce-go"
)

// UntrustedProperty marks the memory message in its AdditionalProperties. A model sees the text and
// not the property; the property is for the application and for any middleware that routes untrusted
// input differently.
const UntrustedProperty = "taisce.untrusted"

// SourceID stamps the messages this package adds, so a filter can tell them from what a person said.
const SourceID = "taisce"

// Config configures a ContextProvider.
type Config struct {
	// Client reaches the deployment. Required.
	Client *taisce.Client
	// DataSubjectID is the person this agent's memory is about. Empty recalls and stores without one.
	DataSubjectID string
	// RunID is part of every turn's key: the same turn in another run is another memory.
	RunID string
	// MaxCharacters bounds what a recall returns; zero is the deployment's default.
	MaxCharacters int
	// SourceRoles narrows a recall to facts said by these roles; empty is the deployment's default.
	SourceRoles []string
	// OnError hears every failure: "recall", which is survived, and "observe", which is also returned
	// to the run. It is how an operator finds out about a recall the agent went on without.
	OnError func(stage string, err error)
}

// ContextProvider recalls before a run and observes after it.
type ContextProvider struct {
	config Config
}

// NewContextProvider builds the provider. It refuses a configuration with no client.
func NewContextProvider(config Config) (*ContextProvider, error) {
	if config.Client == nil {
		return nil, errors.New("agentframework: a client is required")
	}
	return &ContextProvider{config: config}, nil
}

var _ agent.ContextProvider = (*ContextProvider)(nil)

// Invoking recalls for the person's latest message and, when memory holds something, adds exactly one
// user message carrying it, marked untrusted. Never a system message: recalled text was written by
// whoever the agent talked to, and a system message is the highest trust a model gives. A recall that
// fails is reported and survived: the agent runs without memory rather than not at all.
func (p *ContextProvider) Invoking(ctx context.Context, invoking agent.InvokingContext) ([]*message.Message, []agent.Option, error) {
	question := lastUserText(invoking.Messages)
	if question == "" {
		return invoking.Messages, invoking.Options, nil
	}
	fresh, err := p.config.Client.Freshness(ctx)
	if err == nil {
		var bundle taisce.RecallBundle
		bundle, err = p.config.Client.Recall(ctx, taisce.RecallRequest{
			Question: question, DataSubjectID: p.config.DataSubjectID,
			MaxCharacters: p.config.MaxCharacters, SourceRoles: p.config.SourceRoles,
		})
		if err == nil {
			if bundle.Empty() {
				return invoking.Messages, invoking.Options, nil
			}
			return insertBeforeLastUser(invoking.Messages, memoryMessage(taisce.RenderMemoryMessage(fresh, bundle))), invoking.Options, nil
		}
	}
	p.report("recall", err)
	return invoking.Messages, invoking.Options, nil
}

// Invoked stores the turn after a run that succeeded: the person's messages from this run's input and
// the assistant's final words. Nothing else becomes memory by default — not history a history provider
// loaded, which was stored when it happened; not a message a provider or the framework added; not a
// tool call or a tool's answer. A failed run stores nothing, because a failed turn is not a memory.
// A store that fails is reported and returned, and the framework hands it to the application: a lost
// turn is invisible until somebody asks what the system holds about them.
func (p *ContextProvider) Invoked(ctx context.Context, invoked agent.InvokedContext) error {
	if invoked.Err != nil {
		return nil
	}
	var turn []taisce.Message
	for _, m := range invoked.RequestMessages {
		if m.Source.Type != message.SourceTypeExternal || m.Role != message.RoleUser {
			continue
		}
		if text := m.Contents.Text(); strings.TrimSpace(text) != "" && !taisce.IsMemoryText(text) {
			turn = append(turn, taisce.Message{Role: "user", Content: text})
		}
	}
	for _, m := range invoked.ResponseMessages {
		if m.Role != message.RoleAssistant || toolTraffic(m) {
			continue
		}
		if text := m.Contents.Text(); strings.TrimSpace(text) != "" {
			turn = append(turn, taisce.Message{Role: "assistant", Content: text})
		}
	}
	if len(turn) == 0 {
		return nil
	}
	_, err := p.config.Client.Observe(ctx, taisce.ObserveRequest{
		IdempotencyKey: taisce.TurnKey(p.config.DataSubjectID, p.config.RunID, turn),
		DataSubjectID:  p.config.DataSubjectID,
		Messages:       turn,
		// To the second, because a retry stamps its own time and the key, not the clock, is what
		// makes it the same turn.
		OccurredAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	})
	if err != nil {
		p.report("observe", err)
		return err
	}
	return nil
}

func (p *ContextProvider) report(stage string, err error) {
	if p.config.OnError != nil {
		p.config.OnError(stage, err)
	}
}

func memoryMessage(text string) *message.Message {
	m := message.NewText(text)
	m.Role = message.RoleUser
	m.AuthorName = SourceID
	m.AdditionalProperties = map[string]any{UntrustedProperty: true}
	return m.WithSource(message.Source{Type: agent.SourceTypeContextProvider, ID: SourceID})
}

// insertBeforeLastUser returns a new slice with m placed just before the person's latest message, so
// the person's words stay the last thing the model reads.
func insertBeforeLastUser(messages []*message.Message, m *message.Message) []*message.Message {
	at := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == message.RoleUser && !taisce.IsMemoryText(messages[i].Contents.Text()) {
			at = i
			break
		}
	}
	out := slices.Clone(messages)
	return slices.Insert(out, at, m)
}

func lastUserText(messages []*message.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != message.RoleUser {
			continue
		}
		if text := strings.TrimSpace(m.Contents.Text()); text != "" && !taisce.IsMemoryText(text) {
			return text
		}
	}
	return ""
}

// toolTraffic is a message carrying a function call or its result: the model talking to a tool, or
// the tool talking back. Neither is something a person said.
func toolTraffic(m *message.Message) bool {
	for _, c := range m.Contents {
		switch c.(type) {
		case *message.FunctionCallContent, *message.FunctionResultContent:
			return true
		}
	}
	return false
}
