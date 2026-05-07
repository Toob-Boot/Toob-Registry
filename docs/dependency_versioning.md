# Registry Dependency Versioning & SemVer Inheritance

Dieses Dokument beschreibt die vollständige Abhängigkeitslogik der Toob-Registry,
wie Versionsänderungen durch die Schichten nach oben vererbt werden,
und vergleicht den IST-Zustand mit dem SOLL-Zustand.

---

## 1. Dependency Graph (Die Schichten)

```
┌──────────────────────────────────────────────────────────────────────┐
│                          REGISTRY (v1.0.7)                          │
│  registry.json — Aggregiert ALLES. Höchste Ebene.                  │
│  Trigger: Jede Änderung in einer tieferen Schicht.                 │
│  Version: registry_version Feld                                    │
└──────────────────────────────────────────┬───────────────────────────┘
                                           │ depends on
        ┌──────────────────────────────────┼──────────────────────┐
        ▼                                  ▼                      ▼
┌───────────────┐  ┌───────────────────┐  ┌──────────────────────────┐
│  CHIP (1.1.0) │  │ COMPILER IMAGE    │  │ CORE SDK (extern, Tag)  │
│  chip_manifest│  │ (v1.2.0)          │  │ Git Tag core/v*          │
│  .json        │  │ DockerHub Tag     │  │ Toob-Loader Repo        │
└───┬───┬───┬───┘  └────────┬──────────┘  └──────────────────────────┘
    │   │   │               │
    │   │   │               │ depends on (1:1 Mapping)
    │   │   │               ▼
    │   │   │      ┌───────────────────┐
    │   │   │      │ CLI (extern, Tag) │
    │   │   │      │ GitHub Release    │
    │   │   │      │ Toob-CLI-Release  │
    ▼   ▼   ▼      └───────────────────┘
┌──────┐ ┌────────┐ ┌───────────┐
│ ARCH │ │ VENDOR │ │ TOOLCHAIN │
│1.0.0 │ │ 1.0.1  │ │13.2.0_... │
└──────┘ └────────┘ └───────────┘
   ▲         ▲           ▲
   └─────────┴───────────┘
   Unabhängig voneinander.
   Keine gegenseitigen Dependencies.
```

**Wichtig:** Die Cross-Compiler-Toolchain (z.B. `riscv32-esp-elf-gcc 13.2.0`)
ist NICHT im Compiler Image vorinstalliert. Sie wird zur Laufzeit per
`toolchain.EnsureAvailable()` aus der Registry auto-downloaded und in einem
persistenten Docker-Volume gecached. Das Compiler Image enthält nur die
System-Abhängigkeiten (cmake, ninja, python3, zcbor) und die **fest eingebackene
Toob CLI Binary**.

---

## 2. Datenmodelle (Alle Schichten im Detail)

### Schicht 0: Basis-Dependencies (unabhängig voneinander)

#### Arch
| Feld | Wert | Quelle |
|------|------|--------|
| **Pfad** | `arch/{name}/arch_manifest.json` | Dateisystem |
| **Version** | `1.0.0` | Manuell im Manifest |
| **Abhängig von** | Nichts | — |
| **Vererbt an** | Chip (über `chip.arch` Referenz) | — |

```json
// arch/riscv32/arch_manifest.json
{ "name": "riscv32", "version": "1.0.0", "description": "..." }
```
**Code-Dateien:** `arch_timer.c`, `arch_trap.c`, `include/`

#### Vendor
| Feld | Wert | Quelle |
|------|------|--------|
| **Pfad** | `vendor/{name}/vendor_manifest.json` | Dateisystem |
| **Version** | `1.0.1` | Manuell im Manifest |
| **Abhängig von** | Nichts | — |
| **Vererbt an** | Chip (über `chip.vendor` Referenz) | — |

```json
// vendor/esp/vendor_manifest.json
{ "name": "Espressif", "version": "1.0.1", "description": "..." }
```
**Code-Dateien:** `vendor_console.c`, `vendor_rwdt.c`, `vendor_spi_flash.c`, etc.

#### Toolchain
| Feld | Wert | Quelle |
|------|------|--------|
| **Pfad** | `toolchains/{name}/toolchain_manifest.json` | Dateisystem |
| **Version** | `13.2.0_20230928` | Manuell (von Espressif/GCC upstream) |
| **Abhängig von** | Nichts | — |
| **Vererbt an** | Chip (über `chip.compiler_prefix` → Toolchain-Lookup) | — |

```json
// toolchains/riscv32-esp-elf/toolchain_manifest.json
{
    "version": "13.2.0_20230928",
    "urls": { "linux_amd64": "https://...", ... },
    "sha256": { "linux_amd64": "782fee...", ... }
}
```

---

### Schicht 1: Chip (aggregiert Schicht 0)

| Feld | Wert | Quelle |
|------|------|--------|
| **Pfad** | `chips/{name}/chip_manifest.json` | Dateisystem |
| **Version** | `1.1.0` | Manuell ODER auto-bumped durch `semver_calc.go` |
| **Abhängig von** | `arch`, `vendor`, `toolchain` | Über Felder `arch`, `vendor`, `compiler_prefix` |
| **Vererbt an** | Registry | Über `registry.json` Aggregation |

```json
// chips/esp32c6/chip_manifest.json
{
    "name": "esp32c6",
    "vendor": "esp",           // → vendor/esp/
    "arch": "riscv32",         // → arch/riscv32/
    "compiler_prefix": "riscv32-esp-elf-",  // → toolchains/riscv32-esp-elf/
    "version": "1.1.0",
    "min_core_sdk": "core/v0.0.1",
    "min_compiler": "latest"
}
```
**Code-Dateien:** `chip_config.h`, `chip_platform.c`, `startup.c`, Linker-Script, etc.

**Die `environment_hash` Logik:** SHA-256 über `chip + toolchain + vendor + arch` JSON.
Ändert sich irgendein Byte in einer der 4 Quellen, ändert sich der Hash → alle alten Matrix-Tests werden invalidiert.

---

### Schicht 2: Externe Software-Dimensionen

#### CLI (Source of Truth: GitHub Releases)

| Feld | Wert | Quelle |
|------|------|--------|
| **Wo** | GitHub Releases (`Toob-CLI-Release`) | `matrix_generator.go` → GitHub API `/releases` |
| **Versionierung** | Oracle in `oracle-semver.yml` (ABI-Diff basiert) | Automatisch bei Push auf `main` |
| **Vererbt an** | Compiler Image (1:1 Mapping) | Docker-Build-Pipeline |

#### Core SDK (Source of Truth: Git Tags)

| Feld | Wert | Quelle |
|------|------|--------|
| **Wo** | Git Tags (`core/v*` in `Toob-Loader`) | `matrix_generator.go` → GitHub API `/tags` |
| **Versionierung** | Oracle in `oracle-semver.yml` (ABI-Diff basiert) | Automatisch bei Push auf `main` |
| **Vererbt an** | Registry (indirekt über Matrix-Kombination) | — |

#### Compiler Image (Source of Truth: DockerHub Tags)

| Feld | Wert | Quelle |
|------|------|--------|
| **Wo** | DockerHub Image Tags (`toob-boot/toob-compiler`) | `matrix_generator.go` → DockerHub API `/tags` |
| **Abhängig von** | **CLI** (1:1 Mapping: Compiler Image v1.2.0 = CLI v1.2.0) | Auto-Build bei CLI Release |
| **Inhalt** | Ubuntu 26.04 + cmake + ninja + python3 + zcbor + **feste CLI Binary** | Dockerfile.compiler |
| **NICHT enthalten** | Cross-Compiler-Toolchain (wird zur Laufzeit auto-downloaded) | `toolchain.EnsureAvailable()` |

**CLI → Compiler Image SemVer-Vererbung:**
- CLI PATCH (v1.0.0 → v1.0.1) → Compiler Image PATCH (v1.0.0 → v1.0.1)
- CLI MINOR (v1.0.0 → v1.1.0) → Compiler Image MINOR (v1.0.0 → v1.1.0)
- CLI MAJOR (v1.0.0 → v2.0.0) → Compiler Image MAJOR (v1.0.0 → v2.0.0)

Das Compiler Image wird **automatisch** bei jedem CLI-Release gebaut und auf DockerHub
gepusht. Es gibt exakt ein Image pro CLI-Version — kein Flickenteppich.
Patches greifen sofort, weil jeder CLI-Bugfix automatisch ein neues Compiler-Image erzeugt.

**Reproduzierbarkeit:** `toob-compiler:v1.0.0` verhält sich in 6 Monaten identisch.
Ältere Chip-Konfigurationen können mit älteren Compiler-Images gebaut werden,
selbst wenn die neueste CLI breaking changes eingeführt hat.

---

### Schicht 3: Registry (Höchste Ebene)

| Feld | Wert | Quelle |
|------|------|--------|
| **Pfad** | `registry.json` (Root) | Generiert durch `build_registry.go` |
| **Version** | `registry_version: "v1.0.7"` | Auto-bumped durch `build_registry.go` |
| **Abhängig von** | Alle Schicht 0 + Schicht 1 Manifeste | Walk über alle Verzeichnisse |
| **Historisierung** | Git Tags (`v1.0.7`) | Durch `main-release.yml` CI |

---

## 3. SemVer-Vererbungskette (SOLL-Zustand)

### Regel: Der höchste Update-Typ gewinnt, Vererbung nur nach OBEN

```
Vendor: MINOR (1.0.1 → 1.1.0)     ─┐
Arch:   PATCH (1.0.0 → 1.0.1)      ├─→ Chip: MINOR (höchster Typ gewinnt!)
Toolchain: keine Änderung          ─┘         1.1.0 → 1.2.0
                                              │
Chip: MINOR ──────────────────────────────────┼─→ Registry: MINOR
                                                   v1.0.7 → v1.1.0
```

**Vererbungsregeln:**
1. `MAJOR` (1.x.x) > `MINOR` (x.1.x) > `PATCH` (x.x.1)
2. Die Version einer Komponente bumped um **mindestens** den Typ der höchsten Dependency-Änderung
3. Eigene Änderungen werden mit den vererbten Änderungen **verglichen**, der höhere Typ gewinnt
4. Vererbung geht NUR nach oben, NIEMALS nach unten (Arch ändert sich nicht, weil sich ein Chip ändert)

### Beispiel-Szenario

```
Commit: "fix(vendor/esp): update RWDT timeout calculation"
  → vendor/esp geändert
  → Entwickler tagged als PATCH (x.x.+1)

Commit: "feat(arch/riscv32): add trap delegation support"  
  → arch/riscv32 geändert
  → Entwickler tagged als MINOR (x.+1.x)

Zusammengeführt in einem PR-Merge:
  
  1. vendor/esp:    1.0.1 → 1.0.2 (PATCH)
  2. arch/riscv32:  1.0.0 → 1.1.0 (MINOR)
  3. esp32c6:       1.1.0 → 1.2.0 (MINOR, weil arch=MINOR > vendor=PATCH)
  4. registry:      v1.0.7 → v1.1.0 (MINOR, vererbt von Chip)
```

---

## 4. IST-Zustand: Was funktioniert, was fehlt

### ✅ Was funktioniert

| Mechanismus | Script | Status |
|------------|--------|--------|
| Registry-Aggregation (Walk über alle Manifeste) | `build_registry.go` | ✅ Funktioniert |
| Registry-Version Auto-Bump (Patch) | `build_registry.go:bumpPatch()` | ✅ Funktioniert |
| Integrität (SHA256-Hashes, Pfad-Referenzen) | `build_registry.go` | ✅ Funktioniert |
| PR-Validation (Dry-Run) | `pr-validator.yml` | ✅ Funktioniert |
| Git-Tag für Registry-Versionen | `main-release.yml` | ✅ Funktioniert |
| Matrix: CLI/Core/Compiler dynamisch entdecken | `matrix_generator.go` | ✅ Funktioniert |
| Matrix: SemVer-Filter (min_core_sdk, min_compiler) | `matrix_generator.go` | ✅ Funktioniert |
| Matrix: Kartesisches Produkt (CLI × Core × Compiler) | `matrix_generator.go` | ✅ Funktioniert |
| Compiler Image: Dockerfile + toob-ci-build.sh | `Dockerfile.compiler` | ✅ Implementiert |
| Compiler Image: CLI→Image 1:1 Mapping (Architektur) | `dependency_versioning.md` | ✅ Dokumentiert |
| Compiler Image: Toolchain Auto-Download + Cache | `session.go` + Volume | ✅ Implementiert |

### ❌ Was fehlt oder kaputt ist

| Problem | Wo | Auswirkung |
|---------|-----|------------|
| **SemVer-Typ-Vererbung fehlt** | `semver_calc.go` | Bumped immer MINOR, egal ob die Dependency ein PATCH oder MAJOR war. Kein Typ-Vergleich. |
| **Hash-Vergleich greift nie** | `semver_calc.go` | Vergleicht gegen `compatibility_matrix.json`, die leer ist (`{}`). Kein Chip wird je gebumped. |
| **Registry bumped nur PATCH** | `build_registry.go:bumpPatch()` | Egal ob eine Dependency einen MAJOR-Change hatte, die Registry-Version bekommt immer nur +0.0.1. |
| **Kein Mechanismus erkennt den Bump-Typ** | Überall | Niemand liest, ob eine Dependency MAJOR/MINOR/PATCH geändert wurde. Es wird nur geprüft, ob sich der Hash geändert hat (binär: ja/nein). |
| **Basis-Dependencies (Vendor/Arch/Toolchain) werden nie auto-gebumped** | — | Version muss immer manuell in der Manifest-Datei eingetragen werden. Kein Script erkennt Dateiänderungen automatisch. |
| **CLI/Core/Compiler nicht in registry.json** | `registry.json` | Kein lokaler Index über externe Software-Versionen. `planner.go` kann `latestCore`/`latestCompiler` nicht dynamisch auflösen. |
| **Kein Commit-Message-Parsing** | — | Conventional Commits (`feat:`, `fix:`, `BREAKING CHANGE:`) werden ignoriert. |
| **Compiler Image Auto-Publish Pipeline fehlt** | CI | Kein Workflow baut bei CLI-Release automatisch ein neues `toob-compiler:vX.Y.Z` Image und pusht es auf DockerHub. |

---

## 5. Vergleich: IST vs SOLL

### SemVer-Typ-Erkennung

| Aspekt | IST | SOLL |
|--------|-----|------|
| Woher kommt der Bump-Typ? | Hardcoded MINOR in `semver_calc.go:123` | Aus der Version-Differenz der Dependency (`1.0.0→2.0.0` = MAJOR) |
| Wer bumped Vendor/Arch/Toolchain? | Entwickler manuell | Automatisch durch Commit-Message-Konvention ODER Version-Diff |
| Wer bumped Chip? | `semver_calc.go` (aktuell kaputt) | `semver_calc.go` mit korrekter Typ-Vererbung |
| Wer bumped Registry? | `build_registry.go` (immer PATCH) | `build_registry.go` mit dem höchsten vererbten Typ |

### Vererbungskette

| Schritt | IST | SOLL |
|---------|-----|------|
| Vendor ändert sich | Manuell Version bumpen | Manuell ODER automatisch via `git diff` |
| → Chip erbt | Hash-Vergleich (kaputt, da Matrix leer) | Version-Diff der alten vs. neuen Dependency-Version |
| → Registry erbt | Immer `bumpPatch()` | Höchsten Typ aus allen geänderten Chips übernehmen |
| → Matrix invalidiert | Ja (via `environment_hash`) | Ja (via `environment_hash`) — ✅ funktioniert bereits |

### Bump-Typ-Vererbung

| Szenario | IST | SOLL |
|----------|-----|------|
| Vendor PATCH + Arch MINOR | Chip = MINOR (hardcoded) | Chip = MINOR (max(PATCH, MINOR)) |
| Vendor MAJOR | Chip = MINOR (hardcoded) | Chip = MAJOR (vererbt) |
| Nur Chip-eigener Code PATCH | Chip = MINOR (hardcoded) | Chip = PATCH (eigener Typ) |
| Chip MINOR + Toolchain MAJOR | Registry = PATCH (hardcoded) | Registry = MAJOR (max(MINOR, MAJOR)) |

---

## 6. Die Pipeline-Kette (Wie es auslösen SOLL)

```
Developer pushed PR-Merge nach main
        │
        ▼
┌─ main-release.yml ─────────────────────────────────────┐
│                                                         │
│  1. semver_calc.go                                      │
│     a) Für jedes geänderte Verzeichnis (git diff):      │
│        - vendor/esp/* geändert? → Lese alte + neue       │
│          vendor_manifest.json Version → berechne Typ    │
│        - arch/riscv32/* geändert? → ebenso              │
│        - toolchains/* geändert? → ebenso                │
│        - chips/esp32c6/* geändert? → eigener Typ        │
│                                                         │
│     b) Für jeden Chip:                                  │
│        - Sammle Typen aller seiner Dependencies          │
│        - max(eigener_typ, dependency_typen) = finaler   │
│        - Bumpe chip_manifest.json Version               │
│                                                         │
│  2. build_registry.go                                   │
│     - Walk alle Manifeste → aggregiere in registry.json │
│     - max(alle_chip_typen) → bumpe registry_version     │
│                                                         │
│  3. Git Commit + Tag                                    │
│     - registry.json + alle gebumpten Manifeste committen│
│     - Git Tag = neue registry_version                   │
│                                                         │
└─────────────────────────────────────────────────────────┘
        │
        ▼
Toob-CI Daemon (Hetzner-Server)
        │
        ▼
Matrix-Farm: Neue registry_version entdeckt →
  Alle ungetesteten Kombinationen in die Queue
```

---

## 7. Offene Design-Entscheidungen

### A. Wie erkennt `semver_calc.go` den Bump-Typ?

**Option 1: Version-Diff (empfohlen)**
Vergleiche die alte Version (aus dem vorherigen Commit) mit der neuen Version im aktuellen Commit.
- `1.0.0` → `1.0.1` = PATCH
- `1.0.0` → `1.1.0` = MINOR
- `1.0.0` → `2.0.0` = MAJOR

Vorteil: Der Entwickler entscheidet beim Manifest-Edit bewusst den Typ.
Nachteil: Erfordert, dass der Entwickler die Version manuell bumped.

**Option 2: Conventional Commit Parsing**
Parse die Commit-Message: `fix:` = PATCH, `feat:` = MINOR, `BREAKING CHANGE:` = MAJOR.
Vorteil: Vollautomatisch.
Nachteil: Commit-Messages können falsch sein. Weniger Kontrolle.

**Option 3: Hybrid (Version-Diff + Fallback auf Commit-Message)**
Wenn der Entwickler die Version im Manifest manuell gebumped hat → nutze den Diff.
Wenn nicht → parse die Commit-Message als Fallback.

### B. CLI/Core/Compiler im Registry-Index?

Sollen externe Versionen in `registry.json` unter einem `ecosystem` Block gelistet werden?
- Pro: Lokale Übersicht, Offline-Nutzung, Registry-Version-Bump bei neuem Release
- Contra: Registry-Version bumped auch ohne Hardware-Änderung (rein informativ)

---

## 8. Historien-Architektur (Source of Truth)

Das System muss wissen, woher es seine Versionen für Vergleiche und Indexierung bezieht. Wir nutzen eine **Zero-State Architektur**. Das bedeutet, die Pipeline hat keine eigene Datenbank, sondern zieht sich die Wahrheit immer dynamisch aus den dezentralen, unveränderlichen Quellen:

### Interne Komponenten (Hardware / Manifeste)
Die Quelle der Wahrheit ist die **Git-Historie dieses Repositories (`Toob-Registry`)**.
WICHTIG: Obwohl wir nur für die gesamte Registry offizielle Git-Tags (wie `v1.0.7`) anlegen, besitzt jede einzelne interne Komponente (Vendor, Arch, Toolchain, Chip) ihre eigene, unabhängige Versionierung!

**Wie funktioniert das ohne Git-Tags für Vendors/Chips?**
- Jede Komponente hat eine eigene `*_manifest.json` (z.B. `vendor_manifest.json`), in der ein `"version"` Feld steht (z.B. `"1.0.1"`).
- Diese Manifeste *sind* die Source of Truth für die einzelnen Schichten.
- Wenn die Pipeline prüft, ob sich z.B. der `esp` Vendor geändert hat, sucht sie nicht nach einem Git-Tag namens `esp-v1.0.1`. Stattdessen führt sie `git show BEFORE_SHA:vendor/esp/vendor_manifest.json` aus. Sie zieht sich also den exakten Dateiinhalt des Manifests, wie er im vorherigen Commit aussah.
- Dann vergleicht sie das `"version"` Feld aus diesem alten Manifest mit dem `"version"` Feld der aktuellen lokalen Datei.
- **Der Human-Error-Fix:** Wenn sich Code-Dateien im Ordner geändert haben (via `git diff --name-only`), aber das `"version"` Feld im Manifest identisch ist (der Entwickler hat das Update vergessen), liest das System die Commit-Messages (`git log`) und **überschreibt** das Manifest lokal mit der neuen Version, bevor es weiterarbeitet.

Die Historie der einzelnen Hardware-Teile lebt also ausschließlich **im Text der JSON-Dateien über die verschiedenen Git-Commits hinweg**, nicht in Git-Tags. Bei Force-Pushes fällt das System auf den letzten offiziellen Registry-Tag (`git describe --tags`) zurück, um den Zustand aller Manifeste von diesem Zeitpunkt zu holen.

### Externe Komponenten (Software / Ecosystem)
Das Toob-Ökosystem ist in vier spezialisierte Repositories aufgeteilt:
- **[Toob-Loader](https://github.com/Toob-Boot/Toob-Loader)**: Das Haupt-Monorepo für den Core SDK und den Quellcode der CLI.
- **[Toob-Registry](https://github.com/Toob-Boot/Toob-Registry)**: Enthält die Hardware-Manifeste und die zentrale `version_topology.json`. Es ist lokal als Git-Submodule eingebunden.
- **[Toob-CLI-Pipeline](https://github.com/Toob-Boot/Toob-CLI-Pipeline)**: Die Pipeline-Logik (z.B. Docker-Umgebungen) für die CLI-Operationen.
- **[Toob-CLI-Release](https://github.com/Toob-Boot/Toob-CLI-Release)**: Das reine Distributions-Repository für die veröffentlichten CLI-Binaries.

Die Quelle der Wahrheit für dieses externe Software-Ökosystem sind die **öffentlichen APIs der jeweiligen Artifact-Stores**. Diese werden durch `generate_topology.go` abgefragt:
1. **Toob CLI:** Wird per Pagination von der GitHub Releases API (`Toob-Boot/Toob-CLI-Release/releases`) abgefragt. Offizielle Versionen müssen zwingend den Prefix `cli/` tragen (z.B. `cli/v1.0.1`).
2. **Core SDK:** Wird per Pagination von der GitHub Tags API (`Toob-Boot/Toob-Loader/tags`) abgefragt. Offizielle Versionen müssen zwingend den Prefix `core/v*` tragen.
3. **Compiler:** Wird direkt von der DockerHub Tags API (`hub.docker.com/v2/.../toob-compiler/tags`) abgefragt. Jeder gebaute Docker-Container repräsentiert eine offizielle Toolchain-Umgebung.

**Die Aggregation (`version_topology.json`):**
Da wir nicht wollen, dass das System andauernd das API-Ratelimit von GitHub sprengt, läuft im Hintergrund die GitHub Action `version-topology.yml`. Sie bündelt alle diese dezentralen Repositories und Releases und generiert eine statische `version_topology.json`. In dieser Datei wird der exakte, verifizierte Stand des gesamten Ökosystems ("Official" und "Main-Branch") eingefroren.

**Die `registry.json` selbst:**
Auch die `registry.json` wird exakt nach diesem Prinzip gehandhabt! Sie liegt im Root des Repositories und enthält ganz oben zwei wichtige Felder:
1. `"registry_version": "v1.0.7"`: Das ist die aggregierte SemVer-Version der *Daten*. Sie wird vom `build_registry.go` Script automatisch hochgezählt (vererbt), wenn sich Chips ändern. Die Historie dieser Version lebt ebenfalls in den Git-Commits dieser Datei. Das Git-Tag (z.B. `v1.0.7`), das am Ende von der CI erstellt wird, ist lediglich ein bequemer Pointer auf diesen Commit, aber die echte Source of Truth ist das JSON-Feld.
2. `"format_version": 1`: Das ist die Schema-Version der Datei. Falls wir jemals die Struktur der Registry ändern (z.B. wenn wir `"chips"` in `"hardware_chips"` umbenennen), setzen wir die `format_version` manuell auf `2`. So weiß die CLI beim Download sofort, ob sie mit dem JSON-Schema noch kompatibel ist, unabhängig davon, wie oft sich die `registry_version` (die reinen Daten) geändert hat.

Die `registry.json` fungiert ab diesem Moment als die **Single Pane of Glass** (die einzige Quelle der Wahrheit) für das gesamte Toob-Ökosystem.
