package storage

import (
	"context"
	"errors"
	"testing"
)

func TestAgentActionLog(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	for _, tool := range []string{"mark_read", "delete_message"} {
		if err := db.RecordAgentAction(ctx, AgentAction{
			Tool: tool, MessageID: 4, Summary: tool + " happened",
		}); err != nil {
			t.Fatalf("record %s: %v", tool, err)
		}
	}
	got, err := db.ListAgentActions(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	// newest first, so the log reads as a history rather than an archive.
	if got[0].Tool != "delete_message" {
		t.Errorf("first entry is %q, want the newest", got[0].Tool)
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("an entry with no timestamp cannot be placed in time")
	}

	if err := db.ClearAgentActions(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got, _ = db.ListAgentActions(ctx, 10); len(got) != 0 {
		t.Errorf("%d entries survived the clear", len(got))
	}
}

// A failed action is recorded too. An attempt that did not work is exactly what
// somebody looking at this log wants to see.
func TestAgentActionKeepsTheError(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()

	if err := db.RecordAgentAction(ctx, AgentAction{
		Tool: "move_message", Summary: "moved", Error: "folder is gone",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	got, err := db.ListAgentActions(ctx, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v (%d entries)", err, len(got))
	}
	if got[0].Error != "folder is gone" {
		t.Errorf("error = %q, want it kept", got[0].Error)
	}
}

func TestAgentProposalLifecycle(t *testing.T) {
	t.Parallel()
	db := newTestDB(t)
	ctx := context.Background()
	acctID, err := db.CreateAccount(ctx, &Account{Email: "me@example.com"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	id, err := db.CreateAgentProposal(ctx, AgentProposal{
		AccountID: acctID,
		To:        "someone@example.com",
		Subject:   "hello",
		Body:      "text",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	all, err := db.ListAgentProposals(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("list: %v (%d proposals)", err, len(all))
	}
	if all[0].Subject != "hello" || all[0].To != "someone@example.com" {
		t.Errorf("proposal came back as %+v", all[0])
	}

	one, err := db.GetAgentProposal(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if one.Body != "text" {
		t.Errorf("body = %q", one.Body)
	}

	if err := db.DeleteAgentProposal(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := db.GetAgentProposal(ctx, id); !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("err = %v, want ErrProposalNotFound", err)
	}
	// answering the same proposal twice must not silently succeed, or two
	// clicks would send one message twice.
	if err := db.DeleteAgentProposal(ctx, id); !errors.Is(err, ErrProposalNotFound) {
		t.Fatalf("second delete err = %v, want ErrProposalNotFound", err)
	}
}
