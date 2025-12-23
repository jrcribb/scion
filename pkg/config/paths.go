package config

import (
	"os"
	"path/filepath"

	"github.com/ptone/gswarm/pkg/util"
)

const (
	DotGswarm = ".gswarm"
	GlobalDir = ".gswarm"
)

// GetRepoDir returns the .gswarm directory at the root of the git repo, if it exists.
func GetRepoDir() (string, bool) {
	if !util.IsGitRepo() {
		return "", false
	}
	root, err := util.RepoRoot()
	if err != nil {
		return "", false
	}
	p := filepath.Join(root, DotGswarm)
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return p, true
	}
	return "", false
}

func GetProjectDir() (string, error) {
	// 1. Check if we are in a repo with a .gswarm dir at the root
	if p, ok := GetRepoDir(); ok {
		return p, nil
	}

	// 2. Fallback to current directory (legacy/non-repo behavior)
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, DotGswarm), nil
}

func GetGlobalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, GlobalDir), nil
}

func GetProjectTemplatesDir() (string, error) {
	p, err := GetProjectDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "templates"), nil
}

func GetGlobalTemplatesDir() (string, error) {
	g, err := GetGlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(g, "templates"), nil
}

func GetProjectAgentsDir() (string, error) {
	p, err := GetProjectDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(p, "agents"), nil
}

func GetGlobalAgentsDir() (string, error) {
	g, err := GetGlobalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(g, "agents"), nil
}
