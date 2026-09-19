package tests

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	configDir := ""
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		if dir, err := os.MkdirTemp("", "r34-dl-testcfg"); err == nil {
			configDir = dir
			os.Setenv("XDG_CONFIG_HOME", dir)
		}
	}
	code := m.Run()
	if configDir != "" {
		os.RemoveAll(configDir)
	}
	os.Exit(code)
}
