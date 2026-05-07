# Toob Ecosystem: Release Lifecycle & Triggers

Dieses Dokument beschreibt die Release-Kanäle, Auslöser (Triggers) und die automatischen Auswirkungen (z.B. SemVer-Vererbung), wenn in der Toob-Infrastruktur Komponenten veröffentlicht oder verändert werden.

---

## 1. Release Channels & Triggers

Das Ökosystem besteht aus vier primären Release-Säulen. Jede Säule hat ihre eigene Pipeline, aber **alle Enden laufen in der `version_index.json` zusammen**.

### A. Core SDK (`Toob-Loader`)

- **Wie wird released:** Ein Entwickler pusht einen Git-Tag mit dem Prefix `core/v*` (z.B. `core/v1.0.2`) **in das `Toob-Boot/Toob-Loader` Repository**. _(Hinweis: Alternativ kann der automatische ABI-Analyzer `oracle-semver.yml` diesen Tag pushen, wenn sich C-Code im Monorepo ändert)._
- **Pipeline:** `.github/workflows/release-core.yml`
- **Ablauf:**
  1. Packt den C-Code (`toobloader/`, `sdk/`, `common/`) in eine `.zip` Datei.
  2. Erstellt ein offizielles GitHub Release im `Toob-Loader` Repository.
  3. **Trigger:** Sendet ein asynchrones Kommando (`gh workflow run`) an die Registry, um die `version-index.yml` zu starten.
- **Auswirkung:** Die `version_index.json` wird aktualisiert und listet nun das neue Core-SDK Release auf.

### B. CLI (`Toob-CLI-Release`)

- **Wie wird released:** Ein Entwickler pusht einen Git-Tag mit dem Prefix `cli/v*` (z.B. `cli/v1.1.0`) **in das `Toob-Boot/Toob-Loader` Repository**.
- **Pipeline:** `.github/workflows/release-cli.yml`
- **Ablauf:**
  1. Cross-Kompiliert die Go-Binaries für Windows, Mac und Linux (mit UPX Komprimierung).
  2. Pusht die fertigen Binaries in das `Toob-CLI-Release` Distributions-Repository.
  3. **Trigger:** Sendet ebenfalls ein Kommando an die Registry, um die `version-index.yml` zu starten.
- **Auswirkung:** Die `version_index.json` zeigt die neue CLI-Version. Zukünftig wird dieser Schritt auch den Build des Compiler-Images auslösen.

### C. Compiler Image (DockerHub)

- **Wie wird released:** (Geplant) Wird automatisch getriggert, sobald ein neues CLI-Release erfolgreich veröffentlicht wurde.
- **Pipeline:** _(z.B. `release-compiler.yml`)_
- **Ablauf:** Baut einen Ubuntu-Container mit allen C-Abhängigkeiten (cmake, ninja, python3) **und bäckt das neueste CLI-Binary fest ein**.
- **Auswirkung:** Das Image `toob-boot/toob-compiler:vX.Y.Z` landet auf DockerHub. Die `version-index.yml` wird dies bei ihrem nächsten Durchlauf erkennen und indizieren.

### D. Registry Hardware-Manifeste (`Toob-Registry`)

- **Wie wird released:** Änderungen an den Verzeichnissen `chips/`, `arch/`, `vendor/` oder `toolchains/` werden in den `main`-Branch gemerged.
- **Pipeline:** `.github/workflows/main-release.yml` ("Registry Auto-Release")
- **Ablauf:**
  1. **SemVer Calculation:** Berechnet, wie sich die Änderungen auf die Chip-Versionen auswirken (siehe [_Hochvererbung_](dependency_versioning.md#3-semver-vererbungskette-soll-zustand)).
  2. **Aggregation:** Generiert eine neue `registry.json` und erhöht die globale `registry_version`.
  3. Committet die neuen JSON-Dateien und pusht einen neuen Git-Tag (z.B. `v1.0.8`) **in das `Toob-Boot/Toob-Registry` Repository**.
  4. **Trigger 1:** Startet automatisch (`workflow_run`) die `version-index.yml`, um die neue Registry in der Topology zu hinterlegen.
  5. **Trigger 2:** Startet automatisch (`workflow_run`) die `compatibility-matrix.yml`, welche die Hetzner-Farm anweist, alle betroffenen Hardware-Kombinationen neu zu kompilieren und zu testen.

---

## 2. Die SemVer-Vererbung (Hochvererbung)

Die Registry hat eine strikte Vererbungskette. Änderungen an Basis-Komponenten schlagen wie eine Welle nach oben bis zur globalen Registry-Version durch.

**Die Regel der Hochvererbung:**
Die höchste Schwereklasse (`MAJOR` > `MINOR` > `PATCH`) einer Unter-Komponente erzwingt zwingend denselben Versions-Bump für alle übergeordneten Komponenten, die von ihr abhängig sind.

**Beispiel einer Kettenreaktion:**

1. Ein Entwickler fixt einen Bug im Espressif Vendor-Code (`vendor/esp`). Das Vendor-Manifest erhält ein **`PATCH` (+0.0.1)**.
2. In der gleichen PR fügt er ein neues Feature für die RISC-V Architektur (`arch/riscv32`) hinzu. Das Architektur-Manifest erhält ein **`MINOR` (+0.1.0)**.
3. **Chip-Vererbung:** Der Chip `esp32c6` hängt von beidem ab. Die `semver_calc.go` vergleicht die Bumps: `MINOR` ist höher als `PATCH`. Obwohl am Chip-Code selbst kein einziges Zeichen verändert wurde, wird die Version des Chips automatisch um ein **`MINOR`** gebumped.
4. **Registry-Vererbung:** Da sich mindestens ein Chip innerhalb der Registry um ein `MINOR` verändert hat, wird auch die globale `registry_version` in der `registry.json` zwingend um ein **`MINOR`** gebumped (z.B. von `v1.0.7` auf `v1.1.0`).

Dieses System garantiert, dass ein Endnutzer, der seine Projekte auf Chip- oder Registry-Ebene versioniert, immer über Breaking Changes (`MAJOR`) oder neue Features (`MINOR`) in den unsichtbaren Tiefen der Toolchain oder Architektur informiert wird.

---

## 3. Der "Single Source of Truth" Kreislauf

Das Herzstück dieser gesamten Automatisierung ist die Datei `version_index.json`.

Egal, auf welchem der oben genannten Kanäle ein Release stattfindet – **alle Wege führen am Ende zum Index-Generator**.
Da die Pipelines aus dem Monorepo (`release-core` und `release-cli`) sowie die Registry-Pipeline sich gegenseitig antriggern, friert die `version_index.json` nach jeder kleinsten Bewegung im Ökosystem den exakten Zustand ein.

Clients, CLIs und Matrix-Worker müssen somit keine teuren API-Requests an GitHub oder DockerHub senden, sondern können sich blind auf diese eine, aggregierte Datei verlassen, um zu wissen, welche Core-SDKs, CLIs und Compiler-Images offiziell miteinander kompatibel existieren.
