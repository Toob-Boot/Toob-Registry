# Toob Ecosystem: Release Lifecycle & Triggers

Dieses Dokument beschreibt die Release-Kanäle, Auslöser (Triggers) und die konkreten Schritte zum Releasen jeder Komponente im Toob-Ökosystem.

---

## 1. CLI (`Toob-CLI-Release`)

Die CLI ist ein eigenständig versioniertes Go-Binary, das über GitHub Releases an Endnutzer verteilt wird. Da die CLI unabhängig vom Compiler-Container deployed wird, ist hier **Version-Skew möglich** — weshalb ein striktes SemVer-System mit automatischer Erkennung existiert.

### Automatischer Workflow (SemVer Enforcer)

Wenn Code in `cli/toob-cli/` auf `main` gepusht wird, läuft der **SemVer Enforcer** (`semver-enforcer.yml`):

1. Erkennt, welche Monorepo-Bereiche sich geändert haben (Core, CLI, Chips, etc.)
2. Für CLI-Änderungen: Vergleicht die aktuelle `ports.go` per AST-Diff mit der Version des letzten `cli/v*`-Tags
3. Bestimmt den Bump-Typ: `MAJOR` (Breaking), `MINOR` (neue Features), `PATCH` (Bugfixes)
4. Pusht automatisch einen neuen `cli/v{X.Y.Z}`-Tag

Dieser Tag-Push löst die **Release-Pipeline** aus.

### Release-Pipeline (`release-cli.yml`)

**Trigger:** Push eines `cli/v*`-Tags (automatisch vom SemVer Enforcer oder manuell).

**Ablauf:**
1. Checkout des getaggten Commits
2. `go test ./internal/ports/ -count=1` — Vertragsprüfung (assertions_test.go)
3. Cross-Compilation für 4 Targets: `windows/amd64`, `linux/amd64`, `darwin/amd64`, `darwin/arm64`
4. UPX-Kompression für Windows und Linux Binaries
5. SHA256-Checksummen generieren
6. Upload als GitHub Release in `Toob-Boot/Toob-CLI-Release`
7. Trigger der `version-index.yml` in der Registry

> **Hinweis:** Die Pipeline läuft auf dem selbstgehosteten CI-Server via `act` (nicht direkt auf GitHub Actions). Die Bedingung `if: github.event.act` stellt sicher, dass sie nur in dieser Umgebung ausgeführt wird.

### CLI Release — Manuelle Schritte

Falls du manuell releasen willst (z.B. für ein Hotfix):

```bash
# 1. Sicherstellen, dass alle Port-Tests grün sind
cd cli/toob-cli
go test ./internal/ports/ -count=1 -v

# 2. SemVer bestimmen (optional, zur Kontrolle)
go build -o /tmp/semver-tool ./internal/ports/cmd/semver
git show HEAD~1:cli/toob-cli/internal/ports/ports.go > /tmp/ports_old.go
/tmp/semver-tool /tmp/ports_old.go ./internal/ports/ports.go

# 3. Tag pushen (löst die Release-Pipeline auf dem CI-Server aus)
git tag cli/v{X.Y.Z} -m "CLI v{X.Y.Z}: ..."
git push origin cli/v{X.Y.Z}
```

### PR-Tests für die CLI

Wenn ein PR Änderungen an `cli/toob-cli/` enthält, wird die CLI automatisch auf dem CI-Server in einem **Compiler Container** getestet. Der `toob-ci-build.sh` im PR-Modus führt folgende Schritte aus:

1. `go test ./internal/ports/ -count=1` — Port-Contract Assertions
2. `git show origin/main:cli/toob-cli/internal/ports/ports.go` — Holt die Baseline
3. `go run ./internal/ports/cmd/semver` — AST-Diff gegen `main`, Erkennung von Breaking Changes
4. `toob build --native` — Kompiliert den gesamten Firmware-Stack als End-to-End-Test

Für **lokales Testen** vor dem PR reicht:

```bash
cd cli/toob-cli
go test ./internal/ports/ -count=1 -v   # Contract Assertions
go vet ./...                             # Static Analysis
go build .                               # Kompiliert die CLI
```

Das ist funktional identisch mit den Schritten 1-3 der CI-Pipeline. Der Full-Build (Schritt 4) erfordert den Compiler Container und ist lokal nur mit Docker möglich.

---

## 2. Compiler Image (Docker)

> **Status:** Noch kein öffentliches Release. Die Versionierung ist vorbereitet, die Pipeline ist ein Stub.

Das Compiler-Image enthält das CLI-Binary und alle Crosscompilation-Abhängigkeiten (cmake, ninja, GCC-Toolchains). Es wird aktuell nur lokal auf dem CI-Server per `docker-compose build` erzeugt.

### Geplanter Workflow

Die CLI-Version bestimmt die Compiler-Version: Jedes CLI-Release soll automatisch ein neues Compiler-Image triggern, das dieses CLI-Binary einbäckt.

**Trigger:** `compiler/v*`-Tag (geplant: automatisch nach erfolgreichem CLI-Release).

**Pipeline:** `release-compiler.yml`

**Geplanter Ablauf:**
1. CLI-Binary aus dem zugehörigen `Toob-CLI-Release` herunterladen
2. `Dockerfile.compiler` bauen (liegt in `cli/.pipeline-repo/Dockerfile.compiler`)
3. Docker-Image mit Labels taggen (`toob.cli_version`)
4. Push zu DockerHub als `toob-boot/toob-compiler:v{X.Y.Z}`
5. Trigger der `version-index.yml`

**Was noch fehlt:**
- Die Pipeline referenziert ein Dockerfile im Root statt in `cli/.pipeline-repo/`
- Es gibt keinen automatischen Trigger von `release-cli.yml` zu `release-compiler.yml`
- DockerHub Push ist noch nicht implementiert

### Manuelles Tagging (für Nachvollziehbarkeit)

Auch ohne funktionierende Pipeline kann der Tag gesetzt werden, um den Stand zu markieren:

```bash
git tag compiler/v{X.Y.Z} -m "Compiler v{X.Y.Z}: aligned with CLI v{X.Y.Z}"
git push origin compiler/v{X.Y.Z}
```

---

## 3. Core SDK (`Toob-Loader`)

- **Trigger:** Push auf `main` mit Änderungen in `toobloader/`, `sdk/`, `common/` löst den SemVer Enforcer aus.
- **Automatik:** Der Enforcer kompiliert den alten und neuen C-Code, vergleicht die ABI-Kompatibilität via `abidiff`, und pusht automatisch einen `core/v*`-Tag.
- **Pipeline:** `release-core.yml` packt den C-Code in eine `.zip` und erstellt ein GitHub Release im Monorepo.
- **Downstream:** Triggert die `version-index.yml` in der Registry.

---

## 4. Registry Hardware-Manifeste (`Toob-Registry`)

- **Trigger:** Merge auf `main` mit Änderungen in `chips/`, `arch/`, `vendor/` oder `toolchains/`.
- **Pipeline:** `main-release.yml` (Registry Auto-Release).
- **Ablauf:**
  1. SemVer-Vererbungskette berechnen (siehe [Dependency Versioning](dependency_versioning.md))
  2. Neue `registry.json` generieren und `registry_version` bumpen
  3. Git-Tag pushen (z.B. `v1.0.8`)
  4. `version-index.yml` triggern (Topology-Update)
  5. `compatibility-matrix.yml` triggern (Matrix-Farm re-test)

---

## 5. Die SemVer-Vererbung (Hochvererbung)

Die Registry hat eine strikte Vererbungskette. Änderungen an Basis-Komponenten schlagen wie eine Welle nach oben bis zur globalen Registry-Version durch.

**Die Regel der Hochvererbung:**
Die höchste Schwereklasse (`MAJOR` > `MINOR` > `PATCH`) einer Unter-Komponente erzwingt zwingend denselben Versions-Bump für alle übergeordneten Komponenten, die von ihr abhängig sind.

**Beispiel einer Kettenreaktion:**

1. Ein Entwickler fixt einen Bug im Espressif Vendor-Code (`vendor/esp`). Das Vendor-Manifest erhält ein **`PATCH` (+0.0.1)**.
2. In der gleichen PR fügt er ein neues Feature für die RISC-V Architektur (`arch/riscv32`) hinzu. Das Architektur-Manifest erhält ein **`MINOR` (+0.1.0)**.
3. **Chip-Vererbung:** Der Chip `esp32c6` hängt von beidem ab. Die `semver_calc.go` vergleicht die Bumps: `MINOR` ist höher als `PATCH`. Obwohl am Chip-Code selbst kein einziges Zeichen verändert wurde, wird die Version des Chips automatisch um ein **`MINOR`** gebumped.
4. **Registry-Vererbung:** Da sich mindestens ein Chip innerhalb der Registry um ein `MINOR` verändert hat, wird auch die globale `registry_version` in der `registry.json` zwingend um ein **`MINOR`** gebumped (z.B. von `v1.0.7` auf `v1.1.0`).

---

## 6. Der "Single Source of Truth" Kreislauf

Das Herzstück dieser gesamten Automatisierung ist die Datei `version_index.json`.

Egal, auf welchem der oben genannten Kanäle ein Release stattfindet — **alle Wege führen am Ende zum Index-Generator**.
Da die Pipelines aus dem Monorepo (`release-core` und `release-cli`) sowie die Registry-Pipeline sich gegenseitig antriggern, friert die `version_index.json` nach jeder kleinsten Bewegung im Ökosystem den exakten Zustand ein.

Clients, CLIs und Matrix-Worker müssen somit keine teuren API-Requests an GitHub oder DockerHub senden, sondern können sich blind auf diese eine, aggregierte Datei verlassen, um zu wissen, welche Core-SDKs, CLIs und Compiler-Images offiziell miteinander kompatibel existieren.
