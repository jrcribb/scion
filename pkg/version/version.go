package version

import (
	"fmt"
	"runtime/debug"
)

var (
	// Version is the current version of the application.
	// It should be set via ldflags -X.
	Version string

	// Commit is the git commit hash of the build.
	// It should be set via ldflags -X.
	Commit string

	// BuildTime is the timestamp of the build.
	// It should be set via ldflags -X.
	BuildTime string
)

// Get returns the version string based on the current build information.
func Get() string {
	// If Version is set, we assume it's a semantic version tag injected at build time.
	if Version != "" {
		return Version
	}

	// Fallback to commit and build time.
	commit := Commit
	buildTime := BuildTime

	// If variables are empty (e.g. go run or simple go build), try to read from debug info.
	if commit == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					commit = setting.Value
				}
				// Note: vcs.time is commit time, not build time.
			}
		}
	}

	// Shorten commit hash if it's long
	if len(commit) > 7 {
		commit = commit[:7]
	}

	if commit == "" {
		commit = "unknown"
	}

	if buildTime == "" {
		buildTime = "unknown"
	}

	return fmt.Sprintf("Commit: %s\nBuild Time: %s", commit, buildTime)
}
