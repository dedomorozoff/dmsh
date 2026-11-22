package config

import (
	"encoding/json"
	"fmt"
)

// versionedConfig reads only the schema marker. The version must come from
// the raw JSON: unmarshalling into Default() would hide a missing marker
// because the default already carries the current version.
type versionedConfig struct {
	ConfigVersion int `json:"config_version"`
}

// migrate brings an on-disk config up to CurrentConfigVersion. A config from
// the future is an error rather than a silent downgrade.
func migrate(cfg *Config, data []byte) error {
	var v versionedConfig
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("read config version: %w", err)
	}
	switch {
	case v.ConfigVersion > CurrentConfigVersion:
		return fmt.Errorf("config version %d is newer than supported version %d", v.ConfigVersion, CurrentConfigVersion)
	case v.ConfigVersion == CurrentConfigVersion:
		return nil
	case v.ConfigVersion < 0:
		return fmt.Errorf("invalid config version %d", v.ConfigVersion)
	}

	if err := migrateV0ToV1(cfg); err != nil {
		return err
	}
	cfg.ConfigVersion = CurrentConfigVersion
	return nil
}

// migrateV0ToV1 moves the old single proxy_url field into the flat proxy
// fields. Other v0 fields are already merged from Default() by Load; an
// explicit dry_run=false from an old config is a user choice and is kept.
func migrateV0ToV1(cfg *Config) error {
	proxy := cfg.Proxy()
	if err := proxy.Validate(); err != nil {
		return fmt.Errorf("legacy proxy settings: %w", err)
	}
	cfg.SetProxy(proxy)
	return nil
}
