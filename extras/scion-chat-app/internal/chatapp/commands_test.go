package chatapp

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// newTestRouter creates a CommandRouter backed by an ephemeral store and a
// fakeMessenger. The idMapper is nil because these tests exercise code paths
// that check the space link before reaching identity resolution.
func newTestRouter(t *testing.T) (*CommandRouter, *fakeMessenger) {
	t.Helper()
	store := newTestStore(t)
	fm := &fakeMessenger{}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	router := &CommandRouter{
		store:          store,
		messenger:      fm,
		log:            log,
		pendingAuth:    make(map[string]*pendingDeviceAuth),
		pendingDeletes: make(map[string]string),
	}
	return router, fm
}

// TestCmdStart_RequiresSpaceLink verifies that /scion start now requires a
// space link (grove context) before attempting to start an agent.
func TestCmdStart_RequiresSpaceLink(t *testing.T) {
	router, _ := newTestRouter(t)
	event := &ChatEvent{
		Type:     EventCommand,
		Platform: "googlechat",
		SpaceID:  "spaces/unlinked",
		UserID:   "user-1",
	}

	resp, err := router.cmdStart(context.Background(), event, []string{"deploy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Message == nil {
		t.Fatal("expected a response")
	}
	if !strings.Contains(resp.Message.Text, "not linked") {
		t.Errorf("expected 'not linked' message, got: %s", resp.Message.Text)
	}
}

// TestCmdStop_RequiresSpaceLink verifies that /scion stop now requires a
// space link (grove context) before attempting to stop an agent.
func TestCmdStop_RequiresSpaceLink(t *testing.T) {
	router, _ := newTestRouter(t)
	event := &ChatEvent{
		Type:     EventCommand,
		Platform: "googlechat",
		SpaceID:  "spaces/unlinked",
		UserID:   "user-1",
	}

	resp, err := router.cmdStop(context.Background(), event, []string{"deploy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Message == nil {
		t.Fatal("expected a response")
	}
	if !strings.Contains(resp.Message.Text, "not linked") {
		t.Errorf("expected 'not linked' message, got: %s", resp.Message.Text)
	}
}

// TestCmdUnsubscribe_RequiresSpaceLink verifies that /scion unsubscribe now
// requires a space link to scope the deletion to the correct grove.
func TestCmdUnsubscribe_RequiresSpaceLink(t *testing.T) {
	router, _ := newTestRouter(t)
	event := &ChatEvent{
		Type:     EventCommand,
		Platform: "googlechat",
		SpaceID:  "spaces/unlinked",
		UserID:   "user-1",
	}

	resp, err := router.cmdUnsubscribe(context.Background(), event, []string{"deploy"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || resp.Message == nil {
		t.Fatal("expected a response")
	}
	if !strings.Contains(resp.Message.Text, "not linked") {
		t.Errorf("expected 'not linked' message, got: %s", resp.Message.Text)
	}
}

// TestHandleAgentAction_RequiresSpaceLink verifies that agent button actions
// (start, stop, logs) now require a space link for grove scoping.
func TestHandleAgentAction_RequiresSpaceLink(t *testing.T) {
	router, fm := newTestRouter(t)
	event := &ChatEvent{
		Type:     EventAction,
		Platform: "googlechat",
		SpaceID:  "spaces/unlinked",
		UserID:   "user-1",
	}

	for _, verb := range []string{"start", "stop", "logs"} {
		t.Run(verb, func(t *testing.T) {
			fm.messages = nil
			err := router.handleAgentAction(context.Background(), event, verb, "agent-123")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(fm.messages) == 0 {
				t.Fatal("expected a reply message")
			}
			if !strings.Contains(fm.messages[0].Text, "not linked") {
				t.Errorf("expected 'not linked' reply, got: %s", fm.messages[0].Text)
			}
		})
	}
}

// TestExecuteDelete_RequiresSpaceLink verifies that the delete confirmation
// handler requires a space link for grove scoping.
func TestExecuteDelete_RequiresSpaceLink(t *testing.T) {
	router, fm := newTestRouter(t)
	event := &ChatEvent{
		Type:     EventAction,
		Platform: "googlechat",
		SpaceID:  "spaces/unlinked",
		UserID:   "user-1",
	}

	err := router.executeDelete(context.Background(), event, "agent-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.messages) == 0 {
		t.Fatal("expected a reply message")
	}
	if !strings.Contains(fm.messages[0].Text, "not linked") {
		t.Errorf("expected 'not linked' reply, got: %s", fm.messages[0].Text)
	}
}

// TestDialogSubmitRespond_RequiresSpaceLink verifies that the agent.respond
// dialog handler requires a space link for grove scoping.
func TestDialogSubmitRespond_RequiresSpaceLink(t *testing.T) {
	router, fm := newTestRouter(t)
	event := &ChatEvent{
		Type:     EventDialogSubmit,
		Platform: "googlechat",
		SpaceID:  "spaces/unlinked",
		UserID:   "user-1",
		ActionID: "agent.respond.agent-123",
		DialogData: map[string]string{
			"response": "yes, proceed",
		},
	}

	err := router.handleDialogSubmit(context.Background(), event)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fm.messages) == 0 {
		t.Fatal("expected a reply message")
	}
	if !strings.Contains(fm.messages[0].Text, "not linked") {
		t.Errorf("expected 'not linked' reply, got: %s", fm.messages[0].Text)
	}
}
