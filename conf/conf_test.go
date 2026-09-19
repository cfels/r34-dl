package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveUsesPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	resolved, err := os.UserConfigDir()
	if err != nil || (resolved != dir && !strings.HasPrefix(resolved, dir+string(os.PathSeparator))) {
		t.Skipf("os.UserConfigDir does not honour XDG_CONFIG_HOME here (%q)", resolved)
	}

	if err := Save(Config{UserID: "42", APIKey: "secret", ActiveAPI: "rule34"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	appDir := filepath.Join(dir, "r34-dl")
	configPath := filepath.Join(appDir, "config.json")

	file, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := file.Mode().Perm(); perm != 0o600 {
		t.Errorf("config permissions = %o, want 600", perm)
	}
	if folder, err := os.Stat(appDir); err == nil {
		if perm := folder.Mode().Perm(); perm != 0o700 {
			t.Errorf("config dir permissions = %o, want 700", perm)
		}
	}

	if err := os.Chmod(configPath, 0o644); err != nil {
		t.Fatalf("loosen permissions: %v", err)
	}
	if err := Save(Config{UserID: "42", APIKey: "secret", ActiveAPI: "rule34"}); err != nil {
		t.Fatalf("Save after loosening: %v", err)
	}
	file, err = os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := file.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions after rewrite = %o, want 600", perm)
	}
}
