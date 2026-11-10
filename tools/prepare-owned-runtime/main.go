// Command prepare-owned-runtime prepares a private pinned TinyGo source overlay.
// It never installs tools, builds firmware, or modifies the supplied runtime.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type change struct {
	Original *string `json:"original_sha256"`
	Overlay  string  `json:"overlay_sha256"`
}

type manifest struct {
	TinyGo  string            `json:"tinygo"`
	Patch   string            `json:"patch_sha256"`
	Sources map[string]string `json:"sources"`
	Changes map[string]change `json:"source_changes"`
}

func main() {
	source := flag.String("source", "", "existing TinyGo 0.44.0 root (read-only)")
	output := flag.String("output", "", "new private root, under an existing parent directory")
	pin := flag.String("manifest", "internal/nplevent/owned/runtime/pinned.json", "pinned source manifest")
	flag.Parse()
	if err := prepare(*source, *output, *pin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("prepared private TinyGo source; no compiler or device operation performed")
}

func digest(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func check(path, expected string) error {
	actual, err := digest(path)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("%s: source checksum %s, expected %s", path, actual, expected)
	}
	return nil
}

func relativeSource(name string) bool {
	clean := filepath.Clean(filepath.FromSlash(name))
	return !filepath.IsAbs(clean) && strings.HasPrefix(filepath.ToSlash(clean), "src/") &&
		!strings.Contains(filepath.ToSlash(clean), "../")
}

func prepare(source, output, pinPath string) error {
	if source == "" || output == "" {
		return errors.New("-source and -output are required")
	}
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(output))
	if err != nil {
		return fmt.Errorf("output parent must already exist: %w", err)
	}
	output = filepath.Join(parent, filepath.Base(output))
	rel, err := filepath.Rel(source, output)
	if err != nil {
		return err
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return errors.New("output must be outside the supplied TinyGo root")
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return errors.New("output already exists; refusing to overwrite it")
	}
	data, err := os.ReadFile(pinPath)
	if err != nil {
		return err
	}
	var pin manifest
	if err := json.Unmarshal(data, &pin); err != nil {
		return err
	}
	if pin.TinyGo != "0.44.0" || len(pin.Sources) == 0 || len(pin.Changes) == 0 {
		return errors.New("unsupported or empty pinned runtime manifest")
	}
	for name, expected := range pin.Sources {
		if !relativeSource(name) {
			return fmt.Errorf("invalid source path %q", name)
		}
		if err := check(filepath.Join(source, filepath.FromSlash(name)), expected); err != nil {
			return err
		}
	}
	for name, changed := range pin.Changes {
		if !relativeSource(name) {
			return fmt.Errorf("invalid overlay path %q", name)
		}
		path := filepath.Join(source, filepath.FromSlash(name))
		if changed.Original != nil {
			if err := check(path, *changed.Original); err != nil {
				return err
			}
		} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("new overlay path %s must not exist in stock sources", name)
		}
	}
	patch, err := filepath.Abs(filepath.Join(filepath.Dir(pinPath), "tinygo-0.44-owned.patch"))
	if err != nil {
		return err
	}
	if err := check(patch, pin.Patch); err != nil {
		return err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git is required to apply the local source patch: %w", err)
	}
	for _, name := range []string{"src", "bin", "lib", "targets"} {
		if info, err := os.Stat(filepath.Join(source, name)); err != nil || !info.IsDir() {
			return fmt.Errorf("TinyGo root lacks directory %s", name)
		}
	}
	if err := os.Mkdir(output, 0o755); err != nil {
		return err
	}
	if err := copySources(source, output); err != nil {
		return fmt.Errorf("private copy left at %s: %w", output, err)
	}
	for _, name := range []string{"bin", "lib", "targets"} {
		if err := os.Symlink(filepath.Join(source, name), filepath.Join(output, name)); err != nil {
			return fmt.Errorf("private copy left at %s: %w", output, err)
		}
	}
	for _, checkOnly := range []bool{true, false} {
		args := []string{"apply"}
		if checkOnly {
			args = append(args, "--check")
		}
		args = append(args, patch)
		cmd := exec.Command("git", args...)
		cmd.Dir = output
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "GIT_") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "GIT_CEILING_DIRECTORIES="+parent,
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if data, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("private copy left at %s: git apply: %w: %s", output, err, data)
		}
	}
	for name, changed := range pin.Changes {
		if err := check(filepath.Join(output, filepath.FromSlash(name)), changed.Overlay); err != nil {
			return err
		}
	}
	return nil
}

// Only the modified runtime/task packages are copied. Everything else is used
// read-only through stock source links; patch destinations are never symlinks.
func copySources(source, output string) error {
	entries, err := os.ReadDir(filepath.Join(source, "src"))
	if err != nil {
		return err
	}
	if err := os.Mkdir(filepath.Join(output, "src"), 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		from := filepath.Join(source, "src", entry.Name())
		to := filepath.Join(output, "src", entry.Name())
		switch entry.Name() {
		case "runtime":
			if err := copyTree(from, to); err != nil {
				return err
			}
		case "internal":
			if err := os.Mkdir(to, 0o755); err != nil {
				return err
			}
			children, err := os.ReadDir(from)
			if err != nil {
				return err
			}
			for _, child := range children {
				src, dst := filepath.Join(from, child.Name()), filepath.Join(to, child.Name())
				if child.Name() == "task" {
					err = copyTree(src, dst)
				} else {
					err = os.Symlink(src, dst)
				}
				if err != nil {
					return err
				}
			}
		default:
			if err := os.Symlink(from, to); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyTree(source, output string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(output, rel)
		if entry.IsDir() {
			return os.Mkdir(destination, 0o755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		info, err := in.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported source entry %s", path)
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		return errors.Join(copyErr, out.Close())
	})
}
