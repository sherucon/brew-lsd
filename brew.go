package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Package represents an installed Homebrew formula or cask.
type Package struct {
	Name       string
	Desc       string
	Version    string
	IsLeaf     bool     // true if nothing depends on this package
	IsCask     bool     // true if this is a cask (not a formula)
	Deps       []string // runtime dependencies (that are installed)
	RequiredBy []string // packages that depend on this one
}

// findBrew auto-detects the brew binary on Intel and Apple Silicon Macs,
// and also on Linux (Linuxbrew). It also checks PATH first.
func findBrew() (string, error) {
	if p, err := exec.LookPath("brew"); err == nil {
		return p, nil
	}
	for _, candidate := range []string{
		"/opt/homebrew/bin/brew",              // Apple Silicon macOS
		"/usr/local/bin/brew",                 // Intel macOS
		"/home/linuxbrew/.linuxbrew/bin/brew", // Linux
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("Homebrew not found.\nInstall it from https://brew.sh or make sure it's in your PATH")
}

func runBrew(brewPath string, args ...string) ([]byte, error) {
	out, err := exec.Command(brewPath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("`brew %s` failed: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// uninstallPackage runs `brew uninstall [--cask] <name>` and returns any error.
// stderr from brew is included in the error message so the user sees exactly
// what brew said (e.g. "requires: ffmpeg").
func uninstallPackage(name string, isCask bool) error {
	brewPath, err := findBrew()
	if err != nil {
		return err
	}
	args := []string{"uninstall"}
	if isCask {
		args = append(args, "--cask")
	}
	args = append(args, name)
	cmd := exec.Command(brewPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
}


// loadPackages fetches all brew data and returns a fully consistent dep graph.
func loadPackages() ([]Package, error) {
	brewPath, err := findBrew()
	if err != nil {
		return nil, err
	}

	// ── Single source of truth: brew info --json=v2 --installed ───────────────
	//
	// We use the `runtime_dependencies` field inside the `installed` array.
	// This is recorded at bottle-install time and reflects what is ACTUALLY
	// linked on disk — it is exactly what `brew leaves` uses internally.
	//
	// Previously we cross-referenced `brew leaves` (runtime_dependencies) with
	// `brew deps --installed --formula` (formula definitions). Those two sources
	// can disagree for optional/recommended deps, keg-only packages, and
	// bottles built against a different dep set — causing the DEP badge and
	// RequiredBy count to contradict each other.
	//
	// Using runtime_dependencies for BOTH the dep graph AND the leaf calculation
	// guarantees they are always consistent.

	raw, err := runBrew(brewPath, "info", "--json=v2", "--installed")
	if err != nil {
		return nil, err
	}

	var info struct {
		Formulae []struct {
			Name      string `json:"name"`
			Desc      string `json:"desc"`
			Installed []struct {
				Version            string `json:"version"`
				RuntimeDependencies []struct {
					FullName string `json:"full_name"`
				} `json:"runtime_dependencies"`
			} `json:"installed"`
		} `json:"formulae"`
		Casks []struct {
			Token     string  `json:"token"`
			Desc      string  `json:"desc"`
			Installed *string `json:"installed"` // null when not installed
			DependsOn struct {
				Formula []string `json:"formula"`
			} `json:"depends_on"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("failed to parse brew info: %w", err)
	}

	// ── Build the installed formula set ──────────────────────────────────────
	installedFormulas := map[string]bool{}
	for _, f := range info.Formulae {
		if len(f.Installed) > 0 {
			installedFormulas[f.Name] = true
		}
	}

	// ── Build forward dep map from runtime_dependencies ──────────────────────
	// runtime_dependencies[i].full_name may include the tap prefix
	// (e.g. "homebrew/core/openssl@3"). Strip to the short name so it matches
	// the formula `name` field.
	forwardDeps := map[string][]string{}
	for _, f := range info.Formulae {
		if len(f.Installed) == 0 {
			continue
		}
		var deps []string
		for _, rd := range f.Installed[0].RuntimeDependencies {
			name := shortName(rd.FullName)
			if installedFormulas[name] {
				deps = append(deps, name)
			}
		}
		sort.Strings(deps)
		forwardDeps[f.Name] = deps
	}

	// ── Build reverse dep map ─────────────────────────────────────────────────
	reverseDeps := map[string][]string{}
	for pkg, deps := range forwardDeps {
		for _, dep := range deps {
			reverseDeps[dep] = append(reverseDeps[dep], pkg)
		}
	}
	for k := range reverseDeps {
		sort.Strings(reverseDeps[k])
	}

	// ── Assemble Package slice ────────────────────────────────────────────────
	var packages []Package

	for _, f := range info.Formulae {
		if len(f.Installed) == 0 {
			continue
		}
		deps := forwardDeps[f.Name]
		if deps == nil {
			deps = []string{}
		}
		rdeps := reverseDeps[f.Name]
		if rdeps == nil {
			rdeps = []string{}
		}
		packages = append(packages, Package{
			Name:       f.Name,
			Desc:       f.Desc,
			Version:    f.Installed[0].Version,
			IsLeaf:     len(rdeps) == 0, // derived from the same graph — always consistent
			IsCask:     false,
			Deps:       deps,
			RequiredBy: rdeps,
		})
	}

	for _, c := range info.Casks {
		if c.Installed == nil {
			continue
		}
		var deps []string
		for _, d := range c.DependsOn.Formula {
			if installedFormulas[d] {
				deps = append(deps, d)
			}
		}
		sort.Strings(deps)
		packages = append(packages, Package{
			Name:       c.Token,
			Desc:       c.Desc,
			Version:    *c.Installed,
			IsLeaf:     true, // casks are always top-level installs
			IsCask:     true,
			Deps:       deps,
			RequiredBy: []string{},
		})
	}

	// Sort: formulas first, then casks; alphabetically within each group.
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].IsCask != packages[j].IsCask {
			return !packages[i].IsCask
		}
		return packages[i].Name < packages[j].Name
	})

	return packages, nil
}

// shortName strips a tap prefix from a formula full_name.
// e.g. "homebrew/core/openssl@3" → "openssl@3", "openssl@3" → "openssl@3"
func shortName(fullName string) string {
	parts := strings.Split(fullName, "/")
	return parts[len(parts)-1]
}
