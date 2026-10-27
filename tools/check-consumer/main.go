// Command check-consumer builds the raw-radio application in an isolated
// consumer with cold Go caches, then repeats using go mod vendor.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hilli/esp32c6-radio/internal/image"
)

const module = "github.com/hilli/esp32c6-radio"

func main() {
	version := flag.String("version", "", "published commit/version to download; default tests the local module")
	flag.Parse()
	if err := check(*version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func check(version string) (result error) {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	source, err := os.ReadFile("examples/raw-radio/main.go")
	if err != nil {
		return err
	}
	target, err := os.ReadFile("targets/esp32c6-radio.json")
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "esp32c6-radio-consumer-")
	if err != nil {
		return err
	}
	env := append(os.Environ(),
		"GOMODCACHE="+filepath.Join(work, "modules"),
		"GOCACHE="+filepath.Join(work, "cache"), "GOFLAGS=", "GOWORK=off")
	defer func() {
		cmd := exec.Command("go", "clean", "-modcache")
		cmd.Dir, cmd.Env = work, env
		if output, err := cmd.CombinedOutput(); err != nil {
			result = errors.Join(result, fmt.Errorf("consumer cache cleanup: %w\n%s", err, output))
		}
		result = errors.Join(result, os.RemoveAll(work))
	}()
	for name, data := range map[string][]byte{"main.go": source, "target.json": target} {
		if err := os.WriteFile(filepath.Join(work, name), data, 0o644); err != nil {
			return err
		}
	}
	run := func(dir string, args ...string) (string, error) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir, cmd.Env = dir, env
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, output)
		}
		return string(output), nil
	}
	if _, err := run(work, "go", "mod", "init", "consumer.example/radio"); err != nil {
		return err
	}
	if version == "" {
		_, err = run(work, "go", "mod", "edit", "-require="+module+"@v0.0.0", "-replace="+module+"="+root)
	} else {
		_, err = run(work, "go", "get", module+"@"+version)
	}
	if err != nil {
		return err
	}
	if _, err := run(work, "go", "mod", "tidy"); err != nil {
		return err
	}
	moduleDir, err := run(work, "go", "list", "-m", "-f", "{{.Dir}}", module)
	if err != nil {
		return err
	}
	moduleDir = strings.TrimSpace(moduleDir)
	if moduleDir == "" {
		return errors.New("module download has no source directory")
	}
	provenance, err := run(moduleDir, "go", "run", "./tools/import-native")
	if err != nil {
		return err
	}
	fmt.Print(provenance)
	build := func(name string) error {
		for _, suffix := range []string{".elf", ".bin"} {
			if _, err := run(work, "tinygo", "build", "-target", "target.json", "-o", name+suffix, "."); err != nil {
				return err
			}
		}
		report, err := image.Check(filepath.Join(work, name+".elf"), filepath.Join(work, name+".bin"))
		if err != nil {
			return err
		}
		fmt.Printf("%s consumer: image=%d SRAM=%d heap=%d bytes\n", name, report.FlashBytes, report.StaticRAM, report.HeapBytes)
		return nil
	}
	if err := build("module"); err != nil {
		return err
	}
	if _, err := run(work, "go", "mod", "vendor"); err != nil {
		return err
	}
	vendor := filepath.Join(work, "vendor", filepath.FromSlash(module))
	entries, err := os.ReadDir(filepath.Join(moduleDir, "internal", "phy", "native"))
	if err != nil {
		return err
	}
	paths := []string{"LICENSE"}
	for _, entry := range entries {
		if entry.IsDir() {
			return errors.New("unexpected native asset directory")
		}
		paths = append(paths, filepath.Join("internal", "phy", "native", entry.Name()))
	}
	for _, path := range paths {
		original, err := os.ReadFile(filepath.Join(moduleDir, path))
		if err != nil {
			return err
		}
		copy, err := os.ReadFile(filepath.Join(vendor, path))
		if err != nil {
			return err
		}
		if !bytes.Equal(original, copy) {
			return fmt.Errorf("vendor changed %s", path)
		}
	}
	env = append(env, "GOFLAGS=-mod=vendor")
	if err := build("vendor"); err != nil {
		return err
	}
	fmt.Println("all native archives, headers, linker files and licenses survived vendoring")
	return nil
}
