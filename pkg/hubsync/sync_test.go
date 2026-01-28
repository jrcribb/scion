package hubsync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncResult_IsInSync(t *testing.T) {
	tests := []struct {
		name     string
		result   SyncResult
		expected bool
	}{
		{
			name: "empty result is in sync",
			result: SyncResult{
				ToRegister: nil,
				ToRemove:   nil,
				InSync:     nil,
			},
			expected: true,
		},
		{
			name: "only in sync agents",
			result: SyncResult{
				ToRegister: nil,
				ToRemove:   nil,
				InSync:     []string{"agent1", "agent2"},
			},
			expected: true,
		},
		{
			name: "agents to register",
			result: SyncResult{
				ToRegister: []string{"new-agent"},
				ToRemove:   nil,
				InSync:     []string{"agent1"},
			},
			expected: false,
		},
		{
			name: "agents to remove",
			result: SyncResult{
				ToRegister: nil,
				ToRemove:   []AgentRef{{Name: "old-agent", ID: "old-agent-id"}},
				InSync:     []string{"agent1"},
			},
			expected: false,
		},
		{
			name: "both register and remove",
			result: SyncResult{
				ToRegister: []string{"new-agent"},
				ToRemove:   []AgentRef{{Name: "old-agent", ID: "old-agent-id"}},
				InSync:     nil,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsInSync(); got != tt.expected {
				t.Errorf("IsInSync() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGetLocalAgents(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "hubsync-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create agents directory structure
	agentsDir := filepath.Join(tmpDir, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents dir: %v", err)
	}

	// Create agent1 with YAML config
	agent1Dir := filepath.Join(agentsDir, "agent1")
	if err := os.MkdirAll(agent1Dir, 0755); err != nil {
		t.Fatalf("Failed to create agent1 dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agent1Dir, "scion-agent.yaml"), []byte("harness: claude"), 0644); err != nil {
		t.Fatalf("Failed to write agent1 config: %v", err)
	}

	// Create agent2 with JSON config
	agent2Dir := filepath.Join(agentsDir, "agent2")
	if err := os.MkdirAll(agent2Dir, 0755); err != nil {
		t.Fatalf("Failed to create agent2 dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agent2Dir, "scion-agent.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to write agent2 config: %v", err)
	}

	// Create a directory without config (should be ignored)
	orphanDir := filepath.Join(agentsDir, "orphan")
	if err := os.MkdirAll(orphanDir, 0755); err != nil {
		t.Fatalf("Failed to create orphan dir: %v", err)
	}

	// Test GetLocalAgents
	agents, err := GetLocalAgents(tmpDir)
	if err != nil {
		t.Fatalf("GetLocalAgents failed: %v", err)
	}

	if len(agents) != 2 {
		t.Errorf("Expected 2 agents, got %d", len(agents))
	}

	// Check that both agents are found
	agentMap := make(map[string]bool)
	for _, a := range agents {
		agentMap[a] = true
	}

	if !agentMap["agent1"] {
		t.Error("Expected to find agent1")
	}
	if !agentMap["agent2"] {
		t.Error("Expected to find agent2")
	}
	if agentMap["orphan"] {
		t.Error("Should not find orphan directory")
	}
}

func TestGetLocalAgents_EmptyDir(t *testing.T) {
	// Create a temporary directory without agents
	tmpDir, err := os.MkdirTemp("", "hubsync-test-empty-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	agents, err := GetLocalAgents(tmpDir)
	if err != nil {
		t.Fatalf("GetLocalAgents failed: %v", err)
	}

	if len(agents) != 0 {
		t.Errorf("Expected 0 agents, got %d", len(agents))
	}
}

func TestGetLocalAgents_NoDir(t *testing.T) {
	// Test with a path that doesn't exist
	agents, err := GetLocalAgents("/nonexistent/path")
	if err != nil {
		t.Fatalf("GetLocalAgents should not error on missing dir: %v", err)
	}

	if len(agents) != 0 {
		t.Errorf("Expected 0 agents for nonexistent path, got %d", len(agents))
	}
}

func TestSyncResult_ExcludeAgent(t *testing.T) {
	tests := []struct {
		name           string
		result         SyncResult
		excludeAgent   string
		expectedSync   bool
		expectedRegLen int
		expectedRemLen int
	}{
		{
			name: "exclude agent from ToRegister",
			result: SyncResult{
				ToRegister: []string{"agent1", "agent2"},
				ToRemove:   []AgentRef{},
				InSync:     []string{"agent3"},
			},
			excludeAgent:   "agent1",
			expectedSync:   false, // still has agent2 to register
			expectedRegLen: 1,
			expectedRemLen: 0,
		},
		{
			name: "exclude agent from ToRemove",
			result: SyncResult{
				ToRegister: []string{},
				ToRemove:   []AgentRef{{Name: "agent1", ID: "id1"}, {Name: "agent2", ID: "id2"}},
				InSync:     []string{"agent3"},
			},
			excludeAgent:   "agent1",
			expectedSync:   false, // still has agent2 to remove
			expectedRegLen: 0,
			expectedRemLen: 1,
		},
		{
			name: "exclude only agent in ToRegister makes it in sync",
			result: SyncResult{
				ToRegister: []string{"agent1"},
				ToRemove:   []AgentRef{},
				InSync:     []string{"agent2"},
			},
			excludeAgent:   "agent1",
			expectedSync:   true,
			expectedRegLen: 0,
			expectedRemLen: 0,
		},
		{
			name: "exclude only agent in ToRemove makes it in sync",
			result: SyncResult{
				ToRegister: []string{},
				ToRemove:   []AgentRef{{Name: "agent1", ID: "id1"}},
				InSync:     []string{"agent2"},
			},
			excludeAgent:   "agent1",
			expectedSync:   true,
			expectedRegLen: 0,
			expectedRemLen: 0,
		},
		{
			name: "exclude agent from both lists",
			result: SyncResult{
				ToRegister: []string{"agent1"},
				ToRemove:   []AgentRef{{Name: "agent1", ID: "id1"}}, // unlikely but test the logic
				InSync:     []string{},
			},
			excludeAgent:   "agent1",
			expectedSync:   true,
			expectedRegLen: 0,
			expectedRemLen: 0,
		},
		{
			name: "exclude non-existent agent has no effect",
			result: SyncResult{
				ToRegister: []string{"agent1"},
				ToRemove:   []AgentRef{{Name: "agent2", ID: "id2"}},
				InSync:     []string{},
			},
			excludeAgent:   "agent3",
			expectedSync:   false,
			expectedRegLen: 1,
			expectedRemLen: 1,
		},
		{
			name: "empty exclude agent has no effect",
			result: SyncResult{
				ToRegister: []string{"agent1"},
				ToRemove:   []AgentRef{{Name: "agent2", ID: "id2"}},
				InSync:     []string{},
			},
			excludeAgent:   "",
			expectedSync:   false,
			expectedRegLen: 1,
			expectedRemLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := tt.result.ExcludeAgent(tt.excludeAgent)
			if filtered.IsInSync() != tt.expectedSync {
				t.Errorf("IsInSync() = %v, want %v", filtered.IsInSync(), tt.expectedSync)
			}
			if len(filtered.ToRegister) != tt.expectedRegLen {
				t.Errorf("len(ToRegister) = %d, want %d", len(filtered.ToRegister), tt.expectedRegLen)
			}
			if len(filtered.ToRemove) != tt.expectedRemLen {
				t.Errorf("len(ToRemove) = %d, want %d", len(filtered.ToRemove), tt.expectedRemLen)
			}
		})
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "llo wor", true},
		{"404 Not Found", "404", true},
		{"404 Not Found", "not found", true},
		{"Hello World", "goodbye", false},
		{"", "test", false},
		{"test", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.substr, func(t *testing.T) {
			if got := containsIgnoreCase(tt.s, tt.substr); got != tt.expected {
				t.Errorf("containsIgnoreCase(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.expected)
			}
		})
	}
}
