// Copyright 2026 The Taisce Authors
// SPDX-License-Identifier: Apache-2.0

package agentframework

import (
	"testing"

	"github.com/microsoft/agent-framework-go/agent"
	"github.com/microsoft/agent-framework-go/message"

	"github.com/ensera-ai/taisce-go"
)

// The providers refuse a configuration that could only fail later, at the first turn.
func TestAProviderWithoutWhatItNeedsIsRefusedAtConstruction(t *testing.T) {
	client, err := taisce.New("http://localhost:1", "tsk")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewContextProvider(Config{}); err == nil {
		t.Error("a context provider without a client was built")
	}
	for name, cfg := range map[string]CompactionConfig{
		"no client":    {DataSubjectID: "a", AfterMessages: 1},
		"no subject":   {Client: client, AfterMessages: 1},
		"no threshold": {Client: client, DataSubjectID: "a"},
	} {
		if _, err := NewCompaction(cfg); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}

// The memory message goes just before the person's latest message, as a user message marked
// untrusted; tool traffic is recognised by its contents, whatever its role; and only history the
// history provider loaded is replaceable — never a system message, never a memory message.
func TestTheSeamsDecideByRoleSourceAndContent(t *testing.T) {
	user := message.NewText("Where does Marta work?")
	earlier := message.NewText("earlier")
	earlier.Role = message.RoleAssistant
	out := insertBeforeLastUser([]*message.Message{earlier, user}, memoryMessage(taisce.MemoryMessagePrefix+"\n{}"))
	if len(out) != 3 || out[2] != user || out[1].Role != message.RoleUser || out[1].AdditionalProperties[UntrustedProperty] != true ||
		out[1].Source.Type != agent.SourceTypeContextProvider {
		t.Fatalf("unexpected placement %v", out)
	}
	if lastUserText(out) != "Where does Marta work?" || lastUserText([]*message.Message{earlier}) != "" {
		t.Error("the question is not the person's latest message")
	}
	call := &message.Message{Role: message.RoleAssistant, Contents: message.Contents{&message.FunctionCallContent{CallID: "c"}}}
	result := &message.Message{Role: message.RoleTool, Contents: message.Contents{&message.FunctionResultContent{CallID: "c"}}}
	if !toolTraffic(call) || !toolTraffic(result) || toolTraffic(earlier) {
		t.Error("tool traffic misjudged")
	}
	loaded := earlier.WithSource(message.Source{Type: agent.SourceTypeHistoryProvider})
	system := message.NewText("be brief").WithSource(message.Source{Type: agent.SourceTypeHistoryProvider})
	system.Role = message.RoleSystem
	if !replaceable(loaded) || replaceable(system) || replaceable(earlier) {
		t.Error("replaceable history misjudged")
	}
}
