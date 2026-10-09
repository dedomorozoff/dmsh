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

func TestDefaultProviderIsAutoWithTools(t *testing.T) {
	cfg := Default()
	if cfg.Provider != ProviderAuto {
		t.Fatalf("default provider = %q, want %q", cfg.Provider, ProviderAuto)
	}
	if !cfg.ToolsEnabled {
		t.Fatal("tools should be enabled by default")
	}
	if cfg.RemoteModel != DefaultRemoteModel {
		t.Fatalf("remote model = %q, want %q", cfg.RemoteModel, DefaultRemoteModel)
	}
}

func TestValidate_EmptyProviderNormalized(t *testing.T) {
	cfg := Default()
	cfg.Provider = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("empty provider should be normalized, got: %v", err)
	}
	if cfg.Provider != ProviderAuto {
		t.Fatalf("provider = %q, want %q", cfg.Provider, ProviderAuto)
	}
}

func TestValidate_InvalidProvider(t *testing.T) {
	cfg := Default()
	cfg.Provider = "openrouter"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown provider should fail validation")
	}
}

func TestValidate_BadRemoteBaseURL(t *testing.T) {
	for _, base := range []string{"ftp://example.com", "not a url", "https://"} {
		cfg := Default()
		cfg.RemoteBaseURL = base
		if err := cfg.Validate(); err == nil {
			t.Fatalf("remote_base_url %q should fail validation", base)
		}
	}
}

func TestValidate_RemoteModelDefaulted(t *testing.T) {
	cfg := Default()
	cfg.RemoteModel = "  "
	if err := cfg.Validate(); err != nil {
		t.Fatalf("blank remote model should be normalized, got: %v", err)
	}
	if cfg.RemoteModel != DefaultRemoteModel {
		t.Fatalf("remote model = %q, want %q", cfg.RemoteModel, DefaultRemoteModel)
	}
}
