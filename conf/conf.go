package conf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"moxiu/r34-dl/safe"
)

type Config struct {
	APIKey        string   `json:"api_key"`
	UserID        string   `json:"user_id"`
	AgeVerified   bool     `json:"age_verified"`
	ActiveAPI     string   `json:"active_api"`
	AudioEnabled  bool     `json:"audio_enabled,omitempty"`
	FilterAI      bool     `json:"filter_ai,omitempty"`
	Blacklist     []string `json:"blacklist,omitempty"`
	SearchHistory []string `json:"search_history,omitempty"`
}

func NormalizeTag(raw string) string {
	return safe.Tag(strings.ToLower(strings.Join(strings.Fields(raw), "_")))
}

func BlacklistTags(raw string) []string {
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		if tag := NormalizeTag(part); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func AddBlacklist(cfg Config, raw string) (Config, []string, []string) {
	wanted := BlacklistTags(raw)
	drop := make(map[string]bool, len(wanted))
	add := make([]string, 0, len(wanted))
	for _, tag := range wanted {
		if strings.HasPrefix(tag, "-") {
			if trimmed := strings.TrimLeft(tag, "-"); trimmed != "" {
				drop[trimmed] = true
			}
			continue
		}
		add = append(add, tag)
	}

	kept := make([]string, 0, len(cfg.Blacklist)+len(add))
	seen := make(map[string]bool, len(cfg.Blacklist)+len(add))
	removed := make([]string, 0, len(drop))
	for _, raw := range cfg.Blacklist {
		tag := NormalizeTag(raw)
		if tag == "" {
			continue
		}
		if drop[tag] {
			removed = append(removed, tag)
			continue
		}
		if seen[tag] {
			continue
		}
		seen[tag] = true
		kept = append(kept, tag)
	}

	added := make([]string, 0, len(add))
	for _, tag := range add {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		kept = append(kept, tag)
		added = append(added, tag)
	}

	cfg.Blacklist = kept
	return cfg, added, removed
}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(dir, "r34-dl")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(appDir, 0o700)
	return filepath.Join(appDir, "config.json"), nil
}

func Load() (Config, error) {
	p, err := path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{ActiveAPI: "safebooru"}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.ActiveAPI == "" {
		cfg.ActiveAPI = "safebooru"
	}
	return cfg, nil
}

func Save(cfg Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(p), ".config-*.tmp")
	if err != nil {
		return err
	}
	tmp := file.Name()
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(tmp)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func SaveAPIKey(userID, apiKey string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.UserID = userID
	cfg.APIKey = apiKey
	return Save(cfg)
}
