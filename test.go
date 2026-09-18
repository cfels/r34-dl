package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var skippedTestDirs = map[string]bool{
	".git":         true,
	".github":      true,
	".agents":      true,
	".codex":       true,
	"assets":       true,
	"downloads":    true,
	"vendor":       true,
	"node_modules": true,
}

var blockedTestFlags = []string{"-exec", "-toolexec"}

func discoverTestPackages(root string) ([]string, error) {
	seen := map[string]bool{}
	var packages []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				if entry != nil && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			return err
		}
		if entry.IsDir() {
			if path != root && skippedTestDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := "./"
		if rel != "." {
			pkg = "./" + filepath.ToSlash(rel)
		}
		if !seen[pkg] {
			seen[pkg] = true
			packages = append(packages, pkg)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(packages)
	return packages, nil
}

func moduleRoot() string {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return ""
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return ""
	}
	return filepath.Dir(gomod)
}

func testExtraArgs(args []string) ([]string, error) {
	extra := make([]string, 0, len(args))
	for _, arg := range args {
		for _, blocked := range blockedTestFlags {
			if arg == blocked || strings.HasPrefix(arg, blocked+"=") {
				return nil, fmt.Errorf("flag %q is not allowed with --run-tests", arg)
			}
		}
		if arg == "" || strings.ContainsAny(arg, "\x00\n\r") {
			return nil, fmt.Errorf("invalid test argument %q", arg)
		}
		extra = append(extra, arg)
	}
	return extra, nil
}

func runTestPackage(dir, pkg string, extra []string) (time.Duration, error) {
	args := append([]string{"test", "-count=1"}, extra...)
	args = append(args, pkg)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	start := time.Now()
	err := cmd.Run()
	return time.Since(start), err
}

func runTestSuites(extra []string) int {
	args, err := testExtraArgs(extra)
	if err != nil {
		fmt.Fprintf(os.Stderr, "r34-dl: %v\n", err)
		return 2
	}

	dir := moduleRoot()
	if dir == "" {
		fmt.Fprintln(os.Stderr, "r34-dl: not inside a Go module, run --run-tests from the r34-dl source tree")
		return 2
	}

	packages, err := discoverTestPackages(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "r34-dl: %v\n", err)
		return 2
	}
	if len(packages) == 0 {
		fmt.Printf("no test files found in %s\n", dir)
		return 0
	}

	fmt.Printf("running %d test package(s) in %s: %s\n\n", len(packages), dir, strings.Join(packages, " "))

	failed := 0
	var elapsed time.Duration
	for _, pkg := range packages {
		fmt.Printf("== %s\n", pkg)
		took, err := runTestPackage(dir, pkg, args)
		elapsed += took
		if err != nil {
			failed++
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				fmt.Printf("-- %s failed (exit %d) in %s\n\n", pkg, exitErr.ExitCode(), took.Round(time.Millisecond))
			} else {
				fmt.Printf("-- %s could not run: %v\n\n", pkg, err)
			}
			continue
		}
		fmt.Printf("-- %s ok in %s\n\n", pkg, took.Round(time.Millisecond))
	}

	fmt.Printf("%d/%d test package(s) passed in %s\n", len(packages)-failed, len(packages), elapsed.Round(time.Millisecond))
	if failed > 0 {
		return 1
	}
	return 0
}
