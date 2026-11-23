package config

import (
	"encoding/json"
	"testing"
)

// AGENTS.md requires a safe default. Pin it so a future edit cannot silently
// turn on automatic command execution for new users.
func TestDefaultDryRun(t *testing.T) {
	if !Default().DryRun {
		t.Fatal("the default config must keep dry-run enabled")
	}
}

func TestDryRunConfigMerge(t *testing.T) {
	// An explicit legacy value survives the merge.
	cfg := Default()
	if err := json.Unmarshal([]byte(`{"dry_run": false}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.DryRun {
		t.Fatal("dry_run=false in the config must enable execution")
	}

	// An older config without the key keeps the safe default.
	cfg = Default()
	if err := json.Unmarshal([]byte(`{"provider": "ollama"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.DryRun {
		t.Fatal("a config without dry_run must keep the safe default")
	}
}

func TestConfigExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	if ConfigExists() {
		t.Fatal("a fresh config dir must report no config")
	}
	if err := Save(Default()); err != nil {
		t.Fatal(err)
	}
	if !ConfigExists() {
		t.Fatal("a saved config must be detected")
	}
}
