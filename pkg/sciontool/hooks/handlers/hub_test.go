/*
Copyright 2025 The Scion Authors.
*/

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
)

// TestHubHandler_EventMapping tests that events are correctly mapped to Hub status updates.
func TestHubHandler_EventMapping(t *testing.T) {
	tests := []struct {
		name           string
		eventName      string
		eventData      hooks.EventData
		expectCall     bool
		expectedStatus string
	}{
		{
			name:           "session start sends running",
			eventName:      hooks.EventSessionStart,
			expectCall:     true,
			expectedStatus: "running",
		},
		{
			name:           "prompt submit sends busy",
			eventName:      hooks.EventPromptSubmit,
			expectCall:     true,
			expectedStatus: "busy",
		},
		{
			name:           "agent start sends busy",
			eventName:      hooks.EventAgentStart,
			expectCall:     true,
			expectedStatus: "busy",
		},
		{
			name:           "tool start sends busy",
			eventName:      hooks.EventToolStart,
			eventData:      hooks.EventData{ToolName: "Bash"},
			expectCall:     true,
			expectedStatus: "busy",
		},
		{
			name:           "tool end sends idle",
			eventName:      hooks.EventToolEnd,
			expectCall:     true,
			expectedStatus: "idle",
		},
		{
			name:           "agent end sends idle",
			eventName:      hooks.EventAgentEnd,
			expectCall:     true,
			expectedStatus: "idle",
		},
		{
			name:           "notification sends waiting_for_input",
			eventName:      hooks.EventNotification,
			eventData:      hooks.EventData{Message: "What should I do?"},
			expectCall:     true,
			expectedStatus: "waiting_for_input",
		},
		{
			name:           "session end sends stopped",
			eventName:      hooks.EventSessionEnd,
			expectCall:     true,
			expectedStatus: "stopped",
		},
		{
			name:       "pre start does not send",
			eventName:  hooks.EventPreStart,
			expectCall: false,
		},
		{
			name:       "post start does not send",
			eventName:  hooks.EventPostStart,
			expectCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedStatus string
			var mu sync.Mutex
			callCount := 0

			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				callCount++

				var payload map[string]interface{}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("Failed to decode request body: %v", err)
					http.Error(w, "bad request", http.StatusBadRequest)
					return
				}

				// All status updates now use the "status" field
				if status, ok := payload["status"].(string); ok {
					receivedStatus = status
				}

				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{}`))
			}))
			defer server.Close()

			// Set environment variables for the Hub client
			os.Setenv("SCION_HUB_ENDPOINT", server.URL)
			os.Setenv("SCION_SERVER_AUTH_DEV_TOKEN", "test-token")
			os.Setenv("SCION_AGENT_ID", "test-agent-id")
			defer func() {
				os.Unsetenv("SCION_HUB_ENDPOINT")
				os.Unsetenv("SCION_HUB_URL")
				os.Unsetenv("SCION_SERVER_AUTH_DEV_TOKEN")
				os.Unsetenv("SCION_AGENT_ID")
			}()

			// Create handler
			handler := NewHubHandler()
			if handler == nil {
				t.Fatal("Expected handler to be created, got nil")
			}

			// Process event
			event := &hooks.Event{
				Name: tt.eventName,
				Data: tt.eventData,
			}

			err := handler.Handle(event)
			if err != nil {
				t.Errorf("Handle returned error: %v", err)
			}

			mu.Lock()
			gotCalls := callCount
			gotStatus := receivedStatus
			mu.Unlock()

			if tt.expectCall {
				if gotCalls != 1 {
					t.Errorf("Expected 1 call, got %d", gotCalls)
				}
				if gotStatus != tt.expectedStatus {
					t.Errorf("Expected status %q, got %q", tt.expectedStatus, gotStatus)
				}
			} else {
				if gotCalls != 0 {
					t.Errorf("Expected no calls, got %d", gotCalls)
				}
			}
		})
	}
}

// TestHubHandler_NotConfigured tests that nil handler doesn't panic.
func TestHubHandler_NotConfigured(t *testing.T) {
	// Clear environment to ensure client is not configured
	os.Unsetenv("SCION_HUB_ENDPOINT")
	os.Unsetenv("SCION_HUB_URL")
	os.Unsetenv("SCION_SERVER_AUTH_DEV_TOKEN")
	os.Unsetenv("SCION_AGENT_ID")

	handler := NewHubHandler()
	if handler != nil {
		t.Error("Expected handler to be nil when not configured")
	}

	// Nil handler should not panic when Handle is called
	var nilHandler *HubHandler
	err := nilHandler.Handle(&hooks.Event{Name: hooks.EventSessionStart})
	if err != nil {
		t.Errorf("Nil handler returned error: %v", err)
	}
}

// TestHubHandler_ReportMethods tests the explicit report methods.
func TestHubHandler_ReportMethods(t *testing.T) {
	var receivedPayload map[string]interface{}
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	os.Setenv("SCION_HUB_ENDPOINT", server.URL)
	os.Setenv("SCION_SERVER_AUTH_DEV_TOKEN", "test-token")
	os.Setenv("SCION_AGENT_ID", "test-agent-id")
	defer func() {
		os.Unsetenv("SCION_HUB_ENDPOINT")
		os.Unsetenv("SCION_HUB_URL")
		os.Unsetenv("SCION_SERVER_AUTH_DEV_TOKEN")
		os.Unsetenv("SCION_AGENT_ID")
	}()

	handler := NewHubHandler()
	if handler == nil {
		t.Fatal("Expected handler to be created")
	}

	t.Run("ReportWaitingForInput", func(t *testing.T) {
		mu.Lock()
		receivedPayload = nil
		mu.Unlock()

		err := handler.ReportWaitingForInput("What should I do?")
		if err != nil {
			t.Errorf("ReportWaitingForInput returned error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if receivedPayload["status"] != "waiting_for_input" {
			t.Errorf("Expected status 'waiting_for_input', got %v", receivedPayload["status"])
		}
		if receivedPayload["message"] != "What should I do?" {
			t.Errorf("Expected message 'What should I do?', got %v", receivedPayload["message"])
		}
	})

	t.Run("ReportTaskCompleted", func(t *testing.T) {
		mu.Lock()
		receivedPayload = nil
		mu.Unlock()

		err := handler.ReportTaskCompleted("Fixed the bug")
		if err != nil {
			t.Errorf("ReportTaskCompleted returned error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		if receivedPayload["status"] != "completed" {
			t.Errorf("Expected status 'completed', got %v", receivedPayload["status"])
		}
		if receivedPayload["taskSummary"] != "Fixed the bug" {
			t.Errorf("Expected taskSummary 'Fixed the bug', got %v", receivedPayload["taskSummary"])
		}
	})
}

// TestTruncateMessage tests the truncation helper function.
func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer message", 10, "this is..."},
		{"", 10, ""},
	}

	for _, tt := range tests {
		result := truncateMessage(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateMessage(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}
