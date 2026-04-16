# 🍺🌈 Brew LSD

NO. NOT THE HALLUCINOGENIC DRUG, BEER IS ENOUGH.

`brew ls` but its LSDeluxe

A tool so you don't struggle with monitoring what to package to delete, because I did.

## Features

- **All packages** — searchable list of every installed formula and cask
- **Colour-coded badges** — `LEAF` (green) · `DEP` (slate) · `CASK` (blue)
- **Tabs** — filter by All / Leaves / Formulas / Casks
- **Detail view** — press Enter on any package to see exactly what it depends on and what depends on it
- **Auto-detects Homebrew** — works on Apple Silicon (`/opt/homebrew`) and Intel Macs (`/usr/local`)

## Requirements

- macOS with [Homebrew](https://brew.sh) installed
- [Go 1.21+](https://go.dev/dl/)

## Install & run

```bash
# Clone or download the repo, then:
cd brew-lsd
go run .
```

Or download or build a standalone binary yourself:

```bash
go build -o brew-lsd .
./brew-lsd

# Install system-wide:
go install .
brew-lsd
```

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Navigate list |
| `Tab` / `Shift+Tab` | Switch tabs |
| `/` | Open search |
| `Enter` | View package details |
| `Esc` | Close search / go back |
| `q` / `Ctrl+C` | Quit |
| u | Uninstall |

## Understanding the badges

| Badge | Meaning |
|-------|---------|
| 🟩 `LEAF` | Nothing depends on this formula |
| ⬜ `DEP`  | At least one other installed formula requires this package |
| 🟦 `CASK` | A macOS app installed via `brew install --cask` |

> Tip: the **Leaves** tab gives you a quick list of all packages nothing depends on. These are candidates for `brew uninstall` if you no longer use them.
