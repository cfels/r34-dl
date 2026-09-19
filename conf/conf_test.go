package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddBlacklist(t *testing.T) {
	cfg, added, removed := AddBlacklist(Config{}, "scat, Big Breasts ,scat")
	if len(added) != 2 || added[0] != "scat" || added[1] != "big_breasts" {
		t.Fatalf("added = %v, want [scat big_breasts]", added)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	if len(cfg.Blacklist) != 2 || cfg.Blacklist[1] != "big_breasts" {
		t.Fatalf("blacklist = %v, want [scat big_breasts]", cfg.Blacklist)
	}

	cfg, added, removed = AddBlacklist(cfg, "-scat,gu ro")
	if len(added) != 1 || added[0] != "gu_ro" {
		t.Errorf("added = %v, want [gu_ro]", added)
	}
	if len(removed) != 1 || removed[0] != "scat" {
		t.Errorf("removed = %v, want [scat]", removed)
	}
	if len(cfg.Blacklist) != 2 || cfg.Blacklist[0] != "big_breasts" || cfg.Blacklist[1] != "gu_ro" {
		t.Errorf("blacklist = %v, want [big_breasts gu_ro]", cfg.Blacklist)
	}

	cfg, added, removed = AddBlacklist(cfg, "scat\x1b]52;c;x\x07")
	if len(added) != 1 || added[0] != "scat]52;c;x" {
		t.Errorf("added = %v, want the escape stripped from the tag", added)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
	for _, tag := range cfg.Blacklist {
		if strings.ContainsRune(tag, 0x1b) || strings.ContainsRune(tag, 0x07) {
			t.Errorf("blacklist kept control runes: %q", tag)
		}
	}
}

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
