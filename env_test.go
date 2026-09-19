package main

import (
	"errors"
	"strings"
	"testing"
)

func TestCLIOutputStripsRemoteEscapes(t *testing.T) {
	payload := "\x1b]52;c;aGFja2Vk\x07\x1b[2J"
	line := failureLine(12, errors.New("ffmpeg: "+payload+"boom"))
	if strings.Contains(line, "\x1b]52;c;") || strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x07") {
		t.Errorf("bulk failure line leaked an escape sequence: %q", line)
	}
	if !strings.Contains(line, "failed #12") || !strings.Contains(line, "boom") {
		t.Errorf("failure line lost its content: %q", line)
	}

	leaky := failureLine(13, errors.New(`Get "https://api.rule34.xxx/index.php?api_key=SUPERSECRET123&user_id=777": timeout`))
	if strings.Contains(leaky, "SUPERSECRET123") || strings.Contains(leaky, "user_id=777") {
		t.Errorf("failure line leaked credentials: %q", leaky)
	}
}

func TestSanitizedTestEnvDropsToolFlags(t *testing.T) {
	t.Setenv("GOFLAGS", "-toolexec=/tmp/evil")
	t.Setenv("GOEXPERIMENT", "someexp")

	env := sanitizedTestEnv()
	var sawGoEnv, sawToolchain bool
	for _, entry := range env {
		if strings.HasPrefix(entry, "GOFLAGS=") {
			t.Errorf("GOFLAGS survived: %q", entry)
		}
		if strings.HasPrefix(entry, "GOEXPERIMENT=") {
			t.Errorf("GOEXPERIMENT survived: %q", entry)
		}
		if entry == "GOENV=off" {
			sawGoEnv = true
		}
		if entry == "GOTOOLCHAIN=local" {
			sawToolchain = true
		}
	}
	if !sawGoEnv {
		t.Error("GOENV=off should disable the user's go env file during --run-tests")
	}
	if !sawToolchain {
		t.Error("GOTOOLCHAIN=local should pin the toolchain during --run-tests")
	}
	found := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			found = true
		}
	}
	if !found {
		t.Error("PATH should be preserved for the test run")
	}
}
