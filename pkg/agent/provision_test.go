package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptone/scion-agent/pkg/config"
)

func TestProvisionAgentEnvMerging(t *testing.T) {
	tmpDir := t.TempDir()

	// Move to tmpDir to avoid being inside the project's git repo
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Mock HOME for global settings and templates
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	globalScionDir := filepath.Join(tmpDir, ".scion")
	globalTemplatesDir := filepath.Join(globalScionDir, "templates")
	os.MkdirAll(globalTemplatesDir, 0755)

	// Create a dummy template
	tplDir := filepath.Join(globalTemplatesDir, "test-tpl")
	os.MkdirAll(tplDir, 0755)
	tplConfig := `{
		"harness": "test-harness",
		"env": {
			"TPL_VAR": "tpl-val",
			"OVERRIDE_VAR": "tpl-override"
		}
	}`
	os.WriteFile(filepath.Join(tplDir, "scion-agent.json"), []byte(tplConfig), 0644)

	// Global settings
	globalSettings := `{
		"harnesses": {
			"test-harness": {
				"env": {
					"GLOBAL_VAR": "global-val",
					"OVERRIDE_VAR": "global-override"
				}
			}
		}
	}`
	os.WriteFile(filepath.Join(globalScionDir, "settings.json"), []byte(globalSettings), 0644)

	// Project settings
	projectDir := filepath.Join(tmpDir, "project")
	projectScionDir := filepath.Join(projectDir, ".scion")
	os.MkdirAll(projectScionDir, 0755)
	projectSettings := `{
		"profiles": {
			"test-profile": {
				"env": {
					"PROJECT_VAR": "project-val",
					"OVERRIDE_VAR": "project-override"
				}
			}
		}
	}`
	os.WriteFile(filepath.Join(projectScionDir, "settings.json"), []byte(projectSettings), 0644)

	// Provision agent
	agentName := "test-agent"
	_, _, cfg, err := ProvisionAgent(context.Background(), agentName, "test-tpl", "", projectScionDir, "test-profile", "", "", "")
	if err != nil {
		t.Fatalf("ProvisionAgent failed: %v", err)
	}

	// Priority (user requested): Global (lowest) -> Project -> Template (highest)
	// So OVERRIDE_VAR should be "tpl-override"
	
	expectedEnv := map[string]string{
		"GLOBAL_VAR":   "global-val",
		"PROJECT_VAR":  "project-val",
		"TPL_VAR":      "tpl-val",
		"OVERRIDE_VAR": "tpl-override",
	}

	for k, v := range expectedEnv {
		if cfg.Env[k] != v {
			t.Errorf("expected env[%s] = %q, got %q", k, v, cfg.Env[k])
		}
	}

	// Verify it was persisted to scion-agent.json
	agentScionJSON := filepath.Join(projectScionDir, "agents", agentName, "scion-agent.json")
	data, err := os.ReadFile(agentScionJSON)
	if err != nil {
		t.Fatal(err)
	}
	var persistedCfg struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(data, &persistedCfg); err != nil {
		t.Fatal(err)
	}

	for k, v := range expectedEnv {
		if persistedCfg.Env[k] != v {
			t.Errorf("persisted: expected env[%s] = %q, got %q", k, v, persistedCfg.Env[k])
		}
	}
}

func TestProvisionGeminiAgentSettings(t *testing.T) {
	tmpDir := t.TempDir()

	// Move to tmpDir
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Mock HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Initialize a mock project
	projectDir := filepath.Join(tmpDir, "project")
	projectScionDir := filepath.Join(projectDir, ".scion")
	if err := config.InitProject(projectScionDir); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Chdir to projectDir so GetProjectDir finds it
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}

	// Provision a gemini agent
	agentName := "gemini-agent"
	_, _, _, err := ProvisionAgent(context.Background(), agentName, "gemini", "", projectScionDir, "", "", "", "")
	if err != nil {
		t.Fatalf("ProvisionAgent failed: %v", err)
	}

	// Verify agent's settings.json
	agentSettingsPath := filepath.Join(projectScionDir, "agents", agentName, "home", ".gemini", "settings.json")
	data, err := os.ReadFile(agentSettingsPath)
	if err != nil {
		t.Fatalf("failed to read agent settings.json: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to unmarshal agent settings.json: %v", err)
	}

	security := settings["security"].(map[string]interface{})
	auth := security["auth"].(map[string]interface{})
	if auth["selectedType"] != "gemini-api-key" {
		t.Errorf("expected selectedType gemini-api-key, got %v", auth["selectedType"])
	}
}

func TestProvisionAgentNonGitWorkspace(t *testing.T) {
	tmpDir := t.TempDir()

	// Move to tmpDir to avoid being inside the project's git repo
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Mock HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Project-local grove
	projectDir := filepath.Join(tmpDir, "project")
	projectScionDir := filepath.Join(projectDir, ".scion")
	if err := config.InitProject(projectScionDir); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Change into projectDir so FindTemplate (via GetProjectDir) finds it
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}

	evalProjectDir, _ := filepath.EvalSymlinks(projectDir)

	agentName := "test-agent"
	home, ws, cfg, err := ProvisionAgent(context.Background(), agentName, "gemini", "", projectScionDir, "", "", "", "")
	if err != nil {
		t.Fatalf("ProvisionAgent failed: %v", err)
	}

	if ws != "" {
		t.Errorf("expected empty workspace path for non-git agent, got %q", ws)
	}

	if home == "" {
		t.Error("expected non-empty home path")
	}

	// Check volumes in cfg
	found := false
	for _, v := range cfg.Volumes {
		if v.Target == "/workspace" {
			found = true
			evalSource, _ := filepath.EvalSymlinks(v.Source)
			if evalSource != evalProjectDir {
				t.Errorf("expected volume source %q, got %q", evalProjectDir, evalSource)
			}
		}
	}
	if !found {
		t.Error("expected /workspace volume mount not found in config")
	}

	// Global grove
	if err := config.InitGlobal(); err != nil {
		t.Fatalf("InitGlobal failed: %v", err)
	}
	globalScionDir, _ := config.GetGlobalDir()

	// Change into a subdirectory to act as CWD
	cwd := filepath.Join(tmpDir, "some-dir")
	os.MkdirAll(cwd, 0755)
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	evalCWD, _ := filepath.EvalSymlinks(cwd)

	_, ws, cfg, err = ProvisionAgent(context.Background(), "global-agent", "gemini", "", globalScionDir, "", "", "", "")
	if err != nil {
		t.Fatalf("ProvisionAgent failed for global grove: %v", err)
	}

	if ws != "" {
		t.Errorf("expected empty workspace path for global agent, got %q", ws)
	}

	found = false
	for _, v := range cfg.Volumes {
		if v.Target == "/workspace" {
			found = true
			evalSource, _ := filepath.EvalSymlinks(v.Source)
			if evalSource != evalCWD {
				t.Errorf("expected global agent volume source %q (CWD), got %q", evalCWD, evalSource)
			}
		}
	}
	if !found {
		t.Error("expected /workspace volume mount not found in global agent config")
	}
}

func TestProvisionAgentWorkdirFlag(t *testing.T) {
	tmpDir := t.TempDir()

	// Move to tmpDir to avoid being inside the project's git repo
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	// Mock HOME
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)
	os.Setenv("HOME", tmpDir)

	// Project-local grove
	projectDir := filepath.Join(tmpDir, "project")
	projectScionDir := filepath.Join(projectDir, ".scion")
	if err := config.InitProject(projectScionDir); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	// Change into projectDir so FindTemplate (via GetProjectDir) finds it
	if err := os.Chdir(projectDir); err != nil {
		t.Fatal(err)
	}

	customWorkdir := filepath.Join(tmpDir, "custom-workdir")
	os.MkdirAll(customWorkdir, 0755)
	evalCustomWorkdir, _ := filepath.EvalSymlinks(customWorkdir)

	// 1. Test valid --workdir in non-git
	agentName := "workdir-agent"
	_, _, cfg, err := ProvisionAgent(context.Background(), agentName, "gemini", "", projectScionDir, "", "", "", customWorkdir)
	if err != nil {
		t.Fatalf("ProvisionAgent failed: %v", err)
	}

	found := false
	for _, v := range cfg.Volumes {
		if v.Target == "/workspace" {
			found = true
			evalSource, _ := filepath.EvalSymlinks(v.Source)
			if evalSource != evalCustomWorkdir {
				t.Errorf("expected volume source %q, got %q", evalCustomWorkdir, evalSource)
			}
		}
	}
	if !found {
		t.Error("expected /workspace volume mount not found in config")
	}

	// 2. Test relative path for --workdir
	relativeWorkdir := "some-subdir"
	os.MkdirAll(filepath.Join(projectDir, relativeWorkdir), 0755)
	absRelativeWorkdir, _ := filepath.Abs(filepath.Join(projectDir, relativeWorkdir))
	evalAbsRelativeWorkdir, _ := filepath.EvalSymlinks(absRelativeWorkdir)

	_, _, cfg, err = ProvisionAgent(context.Background(), "rel-agent", "gemini", "", projectScionDir, "", "", "", relativeWorkdir)
	if err != nil {
		t.Fatalf("ProvisionAgent failed: %v", err)
	}

	found = false
	for _, v := range cfg.Volumes {
		if v.Target == "/workspace" {
			found = true
			evalSource, _ := filepath.EvalSymlinks(v.Source)
			if evalSource != evalAbsRelativeWorkdir {
				t.Errorf("expected volume source %q, got %q", evalAbsRelativeWorkdir, evalSource)
			}
		}
	}
	if !found {
		t.Error("expected /workspace volume mount not found in config")
	}

	// 3. Test --workdir fails in git repo
	gitDir := filepath.Join(tmpDir, "git-repo")
	os.MkdirAll(gitDir, 0755)
	runCmd(t, gitDir, "git", "init")
	runCmd(t, gitDir, "git", "config", "user.email", "test@example.com")
	runCmd(t, gitDir, "git", "config", "user.name", "Test User")
	os.WriteFile(filepath.Join(gitDir, ".gitignore"), []byte(".scion/agents/\n"), 0644)
	runCmd(t, gitDir, "git", "add", ".gitignore")
	runCmd(t, gitDir, "git", "commit", "-m", "initial commit")

	if err := os.Chdir(gitDir); err != nil {
		t.Fatal(err)
	}
	gitScionDir := filepath.Join(gitDir, ".scion")
	if err := config.InitProject(gitScionDir); err != nil {
		t.Fatalf("InitProject failed: %v", err)
	}

	_, _, _, err = ProvisionAgent(context.Background(), "git-agent", "gemini", "", gitScionDir, "", "", "", customWorkdir)
	if err == nil {
		t.Error("expected error when using --workdir in a git repository, got nil")
	} else if !strings.Contains(err.Error(), "--workdir cannot be used") {
		t.Errorf("expected error message to contain '--workdir cannot be used', got: %v", err)
	}
}
