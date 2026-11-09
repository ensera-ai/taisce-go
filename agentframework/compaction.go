// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package agentframework

import (
	"context"
	"errors"
	"strings"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"

	"github.com/ensera-ai/taisce-go"
)

// CompactionConfig configures a Compaction provider.
type CompactionConfig struct {
	// Client reaches the deployment. Required.
	Client *taisce.Client
	// DataSubjectID is whose history the context is. Required: a context is one person's history.
	DataSubjectID string
	// AfterMessages is the loaded history length, system messages aside, above which the history is
	// replaced. Required and positive.
	AfterMessages int
	// MaxCharacters bounds the context; zero is the deployment's default.
	MaxCharacters int
	// OnError hears a context that could not be fetched, with stage "context". It is survived.
	OnError func(stage string, err error)
}

// Compaction replaces a long loaded history with the deployment's context before the model runs.
//
// Replacement, never a summary: the history a history provider loaded is swapped for one untrusted
// user message carrying the deployment's segments and verbatim turns unchanged. The deployment
// already holds every turn, and a summary written here would be a second, unprovable account of
// them. A context that cannot be fetched leaves the history as it was, so the model reads more
// rather than less and the run goes on.
type Compaction struct {
	config CompactionConfig
}

// NewCompaction builds the provider.
func NewCompaction(config CompactionConfig) (*Compaction, error) {
	switch {
	case config.Client == nil:
		return nil, errors.New("agentframework: a client is required")
	case strings.TrimSpace(config.DataSubjectID) == "":
		return nil, errors.New("agentframework: a data subject is required: a context is one subject's history")
	case config.AfterMessages <= 0:
		return nil, errors.New("agentframework: AfterMessages must be positive: it is the history length that triggers compaction")
	}
	return &Compaction{config: config}, nil
}

var _ agent.ContextProvider = (*Compaction)(nil)

// Invoking replaces the loaded history when it is longer than AfterMessages.
func (c *Compaction) Invoking(ctx context.Context, invoking agent.InvokingContext) ([]*message.Message, []agent.Option, error) {
	loaded := 0
	for _, m := range invoking.Messages {
		if replaceable(m) {
			loaded++
		}
	}
	if loaded <= c.config.AfterMessages {
		return invoking.Messages, invoking.Options, nil
	}
	assembled, err := c.config.Client.Context(ctx, taisce.ContextRequest{DataSubjectID: c.config.DataSubjectID, MaxCharacters: c.config.MaxCharacters})
	if err != nil {
		if c.config.OnError != nil {
			c.config.OnError("context", err)
		}
		return invoking.Messages, invoking.Options, nil
	}
	out := make([]*message.Message, 0, len(invoking.Messages)-loaded+1)
	placed := false
	for _, m := range invoking.Messages {
		if !replaceable(m) {
			out = append(out, m)
			continue
		}
		if !placed {
			out = append(out, memoryMessage(taisce.RenderContextMessage(assembled)))
			placed = true
		}
	}
	return out, invoking.Options, nil
}

// Invoked does nothing: what a run said is stored by the ContextProvider.
func (c *Compaction) Invoked(context.Context, agent.InvokedContext) error { return nil }

// replaceable is a message the history provider loaded that is history the deployment holds. A
// system message is the application's instructions, and a memory message is another provider's
// contribution to this run; neither is replaced.
func replaceable(m *message.Message) bool {
	return m.Source.Type == agent.SourceTypeHistoryProvider && m.Role != message.RoleSystem &&
		!taisce.IsMemoryText(m.Contents.Text())
}
