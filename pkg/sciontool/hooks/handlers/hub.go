/*
Copyright 2025 The Scion Authors.
*/

package handlers

import (
	"context"
	"time"

	"github.com/ptone/scion-agent/pkg/sciontool/hooks"
	"github.com/ptone/scion-agent/pkg/sciontool/hub"
	"github.com/ptone/scion-agent/pkg/sciontool/log"
)

// HubHandler sends status updates to the Scion Hub.
type HubHandler struct {
	client *hub.Client
}

// NewHubHandler creates a new hub handler.
// Returns nil if the Hub client is not configured.
func NewHubHandler() *HubHandler {
	client := hub.NewClient()
	if client == nil || !client.IsConfigured() {
		return nil
	}
	return &HubHandler{
		client: client,
	}
}

// Handle processes an event and sends a status update to the Hub.
func (h *HubHandler) Handle(event *hooks.Event) error {
	if h == nil || h.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	switch event.Name {
	case hooks.EventSessionStart:
		// Session starting - report running
		log.Debug("Hub: Reporting running (session start)")
		err = h.client.ReportRunning(ctx, "Session started")

	case hooks.EventPromptSubmit, hooks.EventAgentStart, hooks.EventModelStart:
		// Agent is thinking/working
		message := "Processing"
		if event.Data.Prompt != "" {
			message = truncateMessage(event.Data.Prompt, 100)
		}
		log.Debug("Hub: Reporting busy (thinking)")
		err = h.client.ReportBusy(ctx, message)

	case hooks.EventToolStart:
		// Claude-specific: ExitPlanMode and AskUserQuestion mean waiting for user
		if event.Dialect == "claude" && (event.Data.ToolName == "ExitPlanMode" || event.Data.ToolName == "AskUserQuestion") {
			message := "Waiting for input"
			if event.Data.ToolName == "ExitPlanMode" {
				message = "Waiting for plan approval"
			}
			log.Debug("Hub: Reporting waiting_for_input (waiting: %s)", event.Data.ToolName)
			err = h.client.UpdateStatus(ctx, hub.StatusUpdate{
				Status:  hub.StatusWaitingForInput,
				Message: message,
			})
			break
		}

		// Agent is executing a tool
		message := "Executing tool"
		if event.Data.ToolName != "" {
			message = "Executing: " + event.Data.ToolName
		}
		log.Debug("Hub: Reporting busy (tool: %s)", event.Data.ToolName)
		err = h.client.ReportBusy(ctx, message)

	case hooks.EventToolEnd, hooks.EventAgentEnd, hooks.EventModelEnd:
		// Agent finished a step - report idle
		log.Debug("Hub: Reporting idle (step completed)")
		err = h.client.ReportIdle(ctx, "Ready")

	case hooks.EventNotification:
		// Agent is waiting for input
		message := "Waiting for input"
		if event.Data.Message != "" {
			message = truncateMessage(event.Data.Message, 100)
		}
		log.Debug("Hub: Reporting waiting_for_input (notification)")
		err = h.client.UpdateStatus(ctx, hub.StatusUpdate{
			Status:  hub.StatusWaitingForInput,
			Message: message,
		})

	case hooks.EventSessionEnd:
		// Session ended
		log.Debug("Hub: Reporting stopped (session end)")
		err = h.client.UpdateStatus(ctx, hub.StatusUpdate{
			Status:  hub.StatusStopped,
			Message: "Session ended",
		})

	default:
		// No status update for this event
		return nil
	}

	if err != nil {
		log.Error("Hub status update failed: %v", err)
		// Don't return error - we don't want Hub failures to break the hook chain
	} else {
		log.Debug("Hub status update sent successfully")
	}

	return nil
}

// ReportWaitingForInput sends a waiting-for-input status to the Hub.
func (h *HubHandler) ReportWaitingForInput(message string) error {
	if h == nil || h.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Debug("Hub: Reporting waiting_for_input (ask_user: %s)", truncateMessage(message, 50))
	return h.client.UpdateStatus(ctx, hub.StatusUpdate{
		Status:  hub.StatusWaitingForInput,
		Message: message,
	})
}

// ReportTaskCompleted sends a task-completed status to the Hub.
func (h *HubHandler) ReportTaskCompleted(taskSummary string) error {
	if h == nil || h.client == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Debug("Hub: Reporting task completed: %s", truncateMessage(taskSummary, 50))
	return h.client.UpdateStatus(ctx, hub.StatusUpdate{
		Status:      hub.StatusCompleted,
		TaskSummary: taskSummary,
	})
}

// truncateMessage truncates a message to the specified length.
func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
