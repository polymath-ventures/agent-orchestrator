package chat_test

import (
	"context"
	"log/slog"
	"testing"

	chatsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/chat"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// A TUI -> chat handoff carries a native conversation id and therefore takes
// the Resume branch. Resume has no start-time model or effort fields, so the
// controller must seed the conversation's durable turn settings before the next
// prompt is dispatched.
func TestChatResumeCarriesTheResolvedModelAndEffort(t *testing.T) {
	st := openStore(t)
	conv := newFakeConversation()
	svc := chatsvc.New(chatsvc.Options{
		Store: st, Sessions: st,
		Drivers: fakeRegistry{driver: fakeDriver{conv: conv}},
		Log:     slog.New(slog.DiscardHandler),
		NewID:   func() string { return "conversation-config" },
	})
	t.Cleanup(func() { _ = svc.Stop(context.Background(), testSession) })

	if _, err := svc.Start(context.Background(), chatsvc.StartConfig{
		SessionID: testSession, ProjectID: testProject, Harness: domain.HarnessClaudeCode,
		DataDir: t.TempDir(), WorkspacePath: t.TempDir(),
		ProviderConversationID: "thread-1",
		Model:                  "claude-opus-4",
		Effort:                 "high",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := svc.StartChatTurn(context.Background(), testSession, "hello"); err != nil {
		t.Fatalf("StartChatTurn: %v", err)
	}
	sent := conv.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("sent %d turns, want 1", len(sent))
	}
	if sent[0].Settings.Model != "claude-opus-4" {
		t.Fatalf("turn model = %q, want the resolved pin claude-opus-4", sent[0].Settings.Model)
	}
	if sent[0].Settings.Effort != "high" {
		t.Fatalf("turn effort = %q, want the resolved pin high", sent[0].Settings.Effort)
	}
}
