// Command check-shared-consumer verifies a LOCAL module zip and vendoring using
// a file-only proxy. It never publishes, contacts a network, or opens a device.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hilli/esp32c6-radio/internal/image"
)

const modulePath = "github.com/hilli/esp32c6-radio"
const localVersion = "v0.0.0-shared.0"

func run(dir string, env []string, args ...string) ([]byte, error) {
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	c.Env = env
	p, err := c.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, p)
	}
	return p, nil
}
func write(path string, p []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, p, 0600)
}
func escaped(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func main() {
	if err := check(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func check() (result error) {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "c6-shared-local-consumer-")
	if err != nil {
		return err
	}
	defer func() {
		c := exec.Command("go", "clean", "-modcache")
		c.Dir = work
		c.Env = append(os.Environ(), "GOMODCACHE="+filepath.Join(work, "modules"), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
		if p, err := c.CombinedOutput(); err != nil {
			result = errors.Join(result, fmt.Errorf("private consumer-cache cleanup: %w\n%s", err, p))
		}
		result = errors.Join(result, os.RemoveAll(work))
	}()
	env := append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "GOWORK=off")
	cache, err := run(root, env, "go", "env", "GOMODCACHE")
	if err != nil {
		return err
	}
	p, err := run(root, env, "go", "list", "-m", "-json", "all")
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(p))
	proxy := filepath.Join(work, "proxy")
	for decoder.More() {
		var mod struct {
			Path, Version string
			Main          bool
		}
		if err := decoder.Decode(&mod); err != nil {
			return err
		}
		if mod.Main {
			continue
		}
		base := filepath.Join(strings.TrimSpace(string(cache)), "cache", "download", filepath.FromSlash(escaped(mod.Path)), "@v")
		for _, suffix := range []string{".mod", ".info", ".zip"} {
			p, err := os.ReadFile(filepath.Join(base, escaped(mod.Version)+suffix))
			if err != nil {
				return fmt.Errorf("offline dependency archive missing; no network fallback: %w", err)
			}
			if err := write(filepath.Join(proxy, filepath.FromSlash(escaped(mod.Path)), "@v", escaped(mod.Version)+suffix), p); err != nil {
				return err
			}
		}
	}
	files, err := run(root, env, "git", "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	names := strings.Split(string(files), "\x00")
	sort.Strings(names)
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, name := range names {
		if name == "" {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular module source %s", name)
		}
		p, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return err
		}
		w, err := writer.Create(modulePath + "@" + localVersion + "/" + filepath.ToSlash(name))
		if err != nil {
			return err
		}
		if _, err := w.Write(p); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	moduleProxy := filepath.Join(proxy, filepath.FromSlash(modulePath), "@v")
	if err := write(filepath.Join(moduleProxy, localVersion+".zip"), archive.Bytes()); err != nil {
		return err
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		return err
	}
	if err := write(filepath.Join(moduleProxy, localVersion+".mod"), mod); err != nil {
		return err
	}
	if err := write(filepath.Join(moduleProxy, localVersion+".info"), []byte(`{"Version":"`+localVersion+`","Time":"2026-10-27T00:00:00Z"}`)); err != nil {
		return err
	}
	consumer := filepath.Join(work, "consumer")
	for from, to := range map[string]string{
		"examples/shared-v2-link/main.go":                    "main.go",
		"targets/esp32c6-shared-v2-proof.json":               "target.json",
		"internal/phy/native/c6-shared-v2-lab1.profile.json": "profile.json",
	} {
		p, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := write(filepath.Join(consumer, to), p); err != nil {
			return err
		}
	}
	env = append(env, "GOPROXY=file://"+filepath.ToSlash(proxy), "GOMODCACHE="+filepath.Join(work, "modules"), "GOCACHE="+filepath.Join(work, "build-cache"), "GOFLAGS=")
	for _, args := range [][]string{
		{"go", "mod", "init", "consumer.example/c6shared"},
		{"go", "mod", "edit", "-require=" + modulePath + "@" + localVersion},
		{"go", "mod", "tidy"},
	} {
		if _, err := run(consumer, env, args...); err != nil {
			return err
		}
	}
	build := func(label string) error {
		for _, suffix := range []string{".elf", ".bin"} {
			if _, err := run(consumer, env, "tinygo", "build", "-target", "target.json", "-o", label+suffix, "."); err != nil {
				return err
			}
		}
		if err := image.CheckSharedV2Manifest(filepath.Join(consumer, "profile.json")); err != nil {
			return err
		}
		r, err := image.CheckSharedV2(filepath.Join(consumer, label+".elf"), filepath.Join(consumer, label+".bin"))
		if err != nil {
			return err
		}
		fmt.Printf("LOCAL %s image=%d static SRAM=%d heap address space=%d; physical NOT_RUN\n", label, r.FlashBytes, r.StaticRAM, r.HeapBytes)
		return nil
	}
	if err := build("module-zip"); err != nil {
		return err
	}
	if _, err := run(consumer, env, "go", "mod", "vendor"); err != nil {
		return err
	}
	vendor := filepath.Join(consumer, "vendor", filepath.FromSlash(modulePath))
	for _, path := range []string{
		"settings/flash_bridge.h", "settings/flash_bridge_esp32c6.c",
		"internal/phy/native/esp32c6-shared-v2-lab1.ld", "internal/phy/native/c6-shared-v2-lab1.profile.json",
		"internal/phy/native/LICENSE", "internal/phy/native/LICENSE-TINYGO", "LICENSE",
	} {
		source, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			return err
		}
		copy, err := os.ReadFile(filepath.Join(vendor, path))
		if err != nil {
			return err
		}
		if !bytes.Equal(source, copy) {
			return fmt.Errorf("vendor changed profile asset %s", path)
		}
	}
	env = append(env, "GOFLAGS=-mod=vendor")
	if err := build("vendor"); err != nil {
		return err
	}
	fmt.Println("Local module zip and vendor preserve profile assets/licenses; no network/publication/device operation.")
	return nil
}
