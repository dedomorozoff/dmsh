package config

import "testing"

func TestValidate_DefaultsOK(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestValidate_EmptyModeNormalized(t *testing.T) {
	cfg := Default()
	cfg.Mode = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty mode should be normalized, got: %v", err)
	}
	if cfg.Mode != ModeAI {
		t.Errorf("expected ModeAI, got %q", cfg.Mode)
	}
}

func TestValidate_InvalidMode(t *testing.T) {
	cfg := Default()
	cfg.Mode = "auto"
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid mode should fail validation")
	}
}

func TestValidate_NegativeThreads(t *testing.T) {
	cfg := Default()
	cfg.Threads = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("negative threads should fail validation")
	}
}

func TestValidate_BadTemperature(t *testing.T) {
	cfg := Default()
	cfg.Temperature = 3.0
	if err := cfg.Validate(); err == nil {
		t.Fatal("temperature out of range should fail validation")
	}
}

func TestValidate_BadRegex(t *testing.T) {
	cfg := Default()
	cfg.DangerPatterns = []string{`[unclosed`}
	if err := cfg.Validate(); err == nil {
		t.Fatal("invalid danger pattern regex should fail validation")
	}
}
