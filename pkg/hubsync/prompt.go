// Package hubsync provides Hub synchronization checks for agent operations.
package hubsync

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ConfirmAction prompts user for Y/n confirmation.
// Returns true if confirmed, false otherwise.
// If autoConfirm is true, returns defaultYes without prompting.
func ConfirmAction(prompt string, defaultYes bool, autoConfirm bool) bool {
	if autoConfirm {
		return defaultYes
	}

	suffix := " (Y/n): "
	if !defaultYes {
		suffix = " (y/N): "
	}

	fmt.Print(prompt + suffix)

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		// On error, return the default
		return defaultYes
	}

	input = strings.TrimSpace(strings.ToLower(input))

	// Empty input returns the default
	if input == "" {
		return defaultYes
	}

	return input == "y" || input == "yes"
}

// ShowSyncPlan displays what will be synced and asks for confirmation.
// Returns true if the user confirms, false otherwise.
func ShowSyncPlan(result *SyncResult, autoConfirm bool) bool {
	if result.IsInSync() {
		return true // Nothing to sync
	}

	fmt.Println()
	fmt.Println("Hub Agent Sync Required")
	fmt.Println("=======================")

	if len(result.ToRegister) > 0 {
		fmt.Println("Agents to register on Hub:")
		for _, name := range result.ToRegister {
			fmt.Printf("  + %s\n", name)
		}
	}

	if len(result.ToRemove) > 0 {
		fmt.Println("Agents to remove from Hub (not on this host):")
		for _, ref := range result.ToRemove {
			fmt.Printf("  - %s\n", ref.Name)
		}
	}

	fmt.Println()
	return ConfirmAction("Proceed with sync?", true, autoConfirm)
}

// ShowRegistrationPrompt displays the grove registration prompt.
// Returns true if the user confirms, false otherwise.
func ShowRegistrationPrompt(groveName string, autoConfirm bool) bool {
	fmt.Println()
	fmt.Printf("Grove '%s' is not registered with the Hub.\n", groveName)
	return ConfirmAction("Register grove with Hub?", true, autoConfirm)
}

// ShowInitRegistrationPrompt displays the post-init registration prompt.
// Returns true if the user confirms, false otherwise.
func ShowInitRegistrationPrompt(autoConfirm bool) bool {
	return ConfirmAction("Grove initialized. Register with Hub?", true, autoConfirm)
}
