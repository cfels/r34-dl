package conf

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	APIKey        string   `json:"api_key"`
	UserID        string   `json:"user_id"`
	AgeVerified   bool     `json:"age_verified"`
	ActiveAPI     string   `json:"active_api"`
	AudioEnabled  bool     `json:"audio_enabled,omitempty"`
	FilterAI      bool     `json:"filter_ai,omitempty"`
	SearchHistory []string `json:"search_history,omitempty"`
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
