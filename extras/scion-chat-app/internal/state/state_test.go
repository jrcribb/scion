package state

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("creating test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDeleteAgentSubscription_GroveScoped(t *testing.T) {
	s := newTestStore(t)

	// Create subscriptions for the same user+agent in two different groves.
	subA := &AgentSubscription{
		PlatformUserID: "user-1",
		Platform:       "googlechat",
		AgentID:        "deploy",
		GroveID:        "grove-A",
		Activities:     "COMPLETED",
	}
	subB := &AgentSubscription{
		PlatformUserID: "user-1",
		Platform:       "googlechat",
		AgentID:        "deploy",
		GroveID:        "grove-B",
		Activities:     "ERROR",
	}

	// The primary key is (platform_user_id, platform, agent_id) so inserting
	// both with the same agent_id but different grove_id would normally
	// overwrite. To test grove-scoped delete, insert them sequentially and
	// verify delete targets the right one. We need unique keys, so use
	// different agent_ids that simulate the same-name-different-grove scenario.
	// Actually, the PK doesn't include grove_id, so let's test with different
	// users to make them unique rows, then verify the grove filter.

	sub1 := &AgentSubscription{
		PlatformUserID: "user-1",
		Platform:       "googlechat",
		AgentID:        "deploy",
		GroveID:        "grove-A",
		Activities:     "COMPLETED",
	}
	sub2 := &AgentSubscription{
		PlatformUserID: "user-2",
		Platform:       "googlechat",
		AgentID:        "deploy",
		GroveID:        "grove-B",
		Activities:     "ERROR",
	}

	if err := s.SetAgentSubscription(sub1); err != nil {
		t.Fatalf("set sub1: %v", err)
	}
	if err := s.SetAgentSubscription(sub2); err != nil {
		t.Fatalf("set sub2: %v", err)
	}

	// Delete user-1's subscription but only in grove-A.
	if err := s.DeleteAgentSubscription("user-1", "googlechat", "deploy", "grove-A"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify user-1's subscription in grove-A is gone.
	got, err := s.GetAgentSubscription("user-1", "googlechat", "deploy")
	if err != nil {
		t.Fatalf("get sub1 after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected user-1 subscription to be deleted, got %+v", got)
	}

	// Verify user-2's subscription in grove-B is untouched.
	got, err = s.GetAgentSubscription("user-2", "googlechat", "deploy")
	if err != nil {
		t.Fatalf("get sub2 after delete: %v", err)
	}
	if got == nil {
		t.Fatal("expected user-2 subscription to still exist")
	}
	if got.GroveID != "grove-B" {
		t.Errorf("grove_id = %q, want %q", got.GroveID, "grove-B")
	}

	// Also test: deleting with the wrong grove_id should not remove anything.
	_ = subA
	_ = subB
	if err := s.DeleteAgentSubscription("user-2", "googlechat", "deploy", "grove-WRONG"); err != nil {
		t.Fatalf("delete with wrong grove: %v", err)
	}
	got, err = s.GetAgentSubscription("user-2", "googlechat", "deploy")
	if err != nil {
		t.Fatalf("get sub2 after wrong-grove delete: %v", err)
	}
	if got == nil {
		t.Fatal("subscription should not have been deleted with wrong grove_id")
	}
}

func TestListAgentSubscriptions_GroveScoped(t *testing.T) {
	s := newTestStore(t)

	// Insert subscriptions across two groves.
	for _, sub := range []*AgentSubscription{
		{PlatformUserID: "user-1", Platform: "googlechat", AgentID: "deploy", GroveID: "grove-A"},
		{PlatformUserID: "user-2", Platform: "googlechat", AgentID: "deploy", GroveID: "grove-B"},
	} {
		if err := s.SetAgentSubscription(sub); err != nil {
			t.Fatalf("set subscription: %v", err)
		}
	}

	// List for grove-A should only return user-1.
	subs, err := s.ListAgentSubscriptions("deploy", "grove-A")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription for grove-A, got %d", len(subs))
	}
	if subs[0].PlatformUserID != "user-1" {
		t.Errorf("expected user-1, got %s", subs[0].PlatformUserID)
	}

	// List for grove-B should only return user-2.
	subs, err = s.ListAgentSubscriptions("deploy", "grove-B")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription for grove-B, got %d", len(subs))
	}
	if subs[0].PlatformUserID != "user-2" {
		t.Errorf("expected user-2, got %s", subs[0].PlatformUserID)
	}
}
