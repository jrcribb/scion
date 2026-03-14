// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"github.com/spf13/cobra"
)

// applyWorkstationDefaults sets workstation-mode defaults on the package-level
// flag variables. It is called from both the daemon launcher and the foreground
// runner, ensuring the two paths stay in sync. Explicit flag overrides
// (cmd.Flags().Changed) are respected — only unset flags receive defaults.
func applyWorkstationDefaults(cmd *cobra.Command) {
	if !cmd.Flags().Changed("enable-hub") {
		enableHub = true
	}
	if !cmd.Flags().Changed("enable-runtime-broker") {
		enableRuntimeBroker = true
	}
	if !cmd.Flags().Changed("enable-web") {
		enableWeb = true
	}
	if !cmd.Flags().Changed("dev-auth") {
		enableDevAuth = true
	}
	if !cmd.Flags().Changed("auto-provide") {
		serverAutoProvide = true
	}
	if !cmd.Flags().Changed("host") {
		hubHost = "127.0.0.1"
	}
}
