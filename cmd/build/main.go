package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const project = "melodix"

func applicationTargets() []string {
	return []string{"discord", "cli"}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	started := time.Now()
	err := run(ctx, os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", strings.TrimPrefix(err.Error(), "main: "))
		os.Exit(1)
	}
	fmt.Printf("Done (%s).\n", time.Since(started).Round(time.Millisecond))
}

func help() {
	targets := applicationTargets()
	fmt.Printf(`%s development helper
Usage: build.cmd [command] [args]  |  bash build.sh [command] [args]

  build                  Build all packages and root executables (default)
  run [target] [-- args]  Build one application and start it; args pass through
  dist                   Build a native release folder, ZIP and SHA-256 checksum
  test [flags]           Run tests, forwarding Go test flags
  vet [flags]            Run go vet
  check                  Vet, build, optional lint, race tests and project checks
  vuln                   Scan dependencies with installed govulncheck
  fmt [fix]              List formatting changes, or apply them
  readme                 Regenerate the README (where supported)
  clean                  Remove generated root executables only
  help                   Show this help

Targets: %s (default: %s)
Dist: dist/%s-%s-%s/ and its .zip and .zip.sha256 files.
Packaging uses this checkout. It does not tag, commit, push or publish a release.
`, project, strings.Join(targets, ", "), targets[0], project, runtime.GOOS, runtime.GOARCH)
}

func run(ctx context.Context, args []string) error {
	targets := applicationTargets()
	mode := "build"
	if len(args) > 0 {
		mode = args[0]
		args = args[1:]
	}
	if mode == "help" || mode == "--help" || mode == "-h" {
		help()
		return nil
	}
	if !slices.Contains([]string{"build", "run", "dist", "test", "vet", "check", "fmt", "readme", "clean", "vuln"}, mode) {
		return fmt.Errorf("main: unknown command %q; use build.cmd help or bash build.sh help", mode)
	}
	if slices.Contains([]string{"build", "dist", "check", "readme", "clean"}, mode) && len(args) != 0 {
		return fmt.Errorf("main: %s takes no arguments", mode)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s | %s/%s\nDirectory: %s\n", project, mode, runtime.GOOS, runtime.GOARCH, root)

	switch mode {
	case "build":
		err = command(ctx, nil, "go", "build", "./...")
		if err != nil {
			return err
		}
		for _, target := range targets {
			err = buildBinary(ctx, target, executable(target), nil)
			if err != nil {
				return err
			}
		}
	case "run":
		target, appArgs, parseErr := runArguments(args)
		if parseErr != nil {
			return parseErr
		}
		binary := executable(target)
		err = buildBinary(ctx, target, binary, nil)
		if err != nil {
			return err
		}
		fmt.Println("Starting application; Ctrl+C stops it.")
		return command(ctx, nil, filepath.Join(root, binary), appArgs...)
	case "dist":
		return distribution(ctx, root)
	case "test":
		testArgs := append([]string{"./..."}, args...)
		return tests(ctx, testArgs...)
	case "vet":
		vetArgs := append([]string{"vet", "./..."}, args...)
		return command(ctx, nil, "go", vetArgs...)
	case "check":
		return check(ctx)
	case "fmt":
		flag := "-l"
		if len(args) == 1 && args[0] == "fix" {
			flag = "-w"
		} else if len(args) != 0 {
			return fmt.Errorf("main: usage: build fmt [fix]")
		}
		fmt.Println("Formatting is explicit; a listing does not modify files.")
		return command(ctx, nil, "gofmt", flag, "cmd", "internal", "pkg/music")
	case "vuln":
		_, err = exec.LookPath("govulncheck")
		if err != nil {
			return fmt.Errorf("main: govulncheck is missing; install with go install golang.org/x/vuln/cmd/govulncheck@latest")
		}
		return command(ctx, nil, "govulncheck", "./...")
	case "readme":
		return command(ctx, nil, "go", "run", "./cmd/readme")
	case "clean":
		for _, target := range targets {
			for _, suffix := range []string{"", ".exe"} {
				name := binaryName(target) + suffix
				err = os.Remove(name)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				fmt.Println("Removed, if present:", name)
			}
		}
	}
	return nil
}

func runArguments(args []string) (string, []string, error) {
	targets := applicationTargets()
	target := targets[0]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = args[0]
		args = args[1:]
	}
	if !slices.Contains(targets, target) {
		return "", nil, fmt.Errorf("main: unknown target %q; choose %s", target, strings.Join(targets, ", "))
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	return target, args, nil
}

func binaryName(target string) string {
	return project + "-" + target
}

func executable(target string) string {
	name := binaryName(target)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func buildBinary(ctx context.Context, target, output string, env []string) error {
	fmt.Println("Building:", output)
	return command(ctx, env, "go", "build", "-trimpath", "-o", output, "./cmd/"+target)
}

func command(ctx context.Context, env []string, name string, args ...string) error {
	display := slices.Clone(args)
	for i, arg := range display {
		if strings.ContainsAny(arg, " \t\"") {
			display[i] = strconv.Quote(arg)
		}
	}
	fmt.Println(">", name, strings.Join(display, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if env != nil {
		cmd.Env = env
	}
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("main: %s: %w", name, err)
	}
	return nil
}

func tests(ctx context.Context, args ...string) error {
	return command(ctx, nil, "go", append([]string{"test"}, args...)...)
}

func check(ctx context.Context) error {
	fmt.Println("[1/4] Vet")
	err := command(ctx, nil, "go", "vet", "./...")
	if err != nil {
		return err
	}
	fmt.Println("[2/4] Build all packages")
	err = command(ctx, nil, "go", "build", "./...")
	if err != nil {
		return err
	}
	fmt.Println("[3/4] Lint")
	_, err = exec.LookPath("golangci-lint")
	if err == nil {
		err = command(ctx, nil, "golangci-lint", "run", "--allow-serial-runners", "./...")
		if err != nil {
			return err
		}
	} else {
		fmt.Println("SKIP: golangci-lint is not installed; CI still runs it.")
	}
	fmt.Println("[4/4] Race tests, including convention checks (requires a C compiler)")
	err = tests(ctx, "-race", "./...", "-count=1", "-timeout=300s")
	if err != nil {
		return err
	}
	fmt.Println("Project check: release documentation packaging")
	err = command(ctx, nil, "node", "--test", "scripts/archive-docs.test.mjs")
	if err != nil {
		return err
	}

	return nil
}
