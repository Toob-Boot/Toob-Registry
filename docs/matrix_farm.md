# Toob-CI Orchestrator & Compatibility Matrix Farm

Dieses Dokument beschreibt die vollständige Architektur, Logik und den Lebenszyklus der Toob-CI Infrastruktur. Es erklärt, wie die autonome Matrix-Farm (auf dem dedizierten Server) arbeitet, wie Testaufträge verteilt werden und wie die Ergebnisse konfliktfrei und ausfallsicher in die Registry (GitHub) zurückfließen.

---

## 1. Architektur-Übersicht

Die Test-Infrastruktur ist strikt in zwei Domänen unterteilt:
1. **GitHub Repository (`Toob-Registry`):** Dient als reine Datenbank ("Single Source of Truth"). Hier liegen die Manifeste, die `version_index.json` und die öffentliche `compatibility_matrix.json` (der Ledger).
2. **Hetzner Server (`Toob-CI Daemon`):** Die Compute-Instanz. Ein permanenter Go-Daemon (`toob-ci`), der Webhooks empfängt, die Matrix berechnet, Docker-Container startet und am Ende Ergebnisse als Commits an GitHub zurückschickt.

---

## 2. Der Matrix Generator (Die Queue)

Bevor getestet werden kann, muss die Farm wissen, **was** getestet werden muss. Diese Logik lebt in `scripts/matrix_generator.go`.

### A. Das Kartesische Produkt
Der Generator berechnet alle mathematisch möglichen Kombinationen aus dem gesamten Ökosystem:
`Chips × CLI-Versionen × Core-SDK-Versionen × Compiler-Images`

### B. Der Environment-Hash (Fingerprint)
Um zu wissen, ob sich die Hardware tiefgreifend geändert hat, berechnet der Generator für jeden Chip einen kryptografischen SHA-256 Hash.
Dieser Hash speist sich aus dem Inhalt von:
- `chip_manifest.json`
- `vendor_manifest.json` (des jeweiligen Vendors)
- `arch_manifest.json` (der jeweiligen Architektur)
- `toolchain_manifest.json` (der genutzten Toolchain)
Ändert sich auch nur ein Byte in einem dieser Manifeste, ändert sich der Fingerprint.

### C. Der Filter
Der Generator liest die bestehende `compatibility_matrix.json` (den öffentlichen Ledger). Alle Kombinationen, die dort bereits mit exakt demselben Fingerprint als `VERIFIED` markiert sind, werden aus der Liste gestrichen. Die verbleibende Liste ist die **Queue** der ungetesteten Kombinationen.

---

## 3. Toob-CI Daemon (Der Orchestrator)

Der Daemon läuft kontinuierlich im Hintergrund und managed die Test-Ressourcen.

### A. Der Matrix Poller (`matrix.go`)
Ein Hintergrund-Thread wacht jede Stunde auf und ruft den *Matrix Generator* auf. Die hunderten von ungetesteten Kombinationen werden dann nach dem benötigten **Compiler-Image** gruppiert (Compiler-Keeper Logik, um unnötige Docker-Pulls zu vermeiden) und als `WorkPackages` an den Planner geschickt.

### B. Der Planner & Slots (`planner.go`)
Der Planner ist das Herzstück der Parallelisierung.
- Er sortiert die Queue streng nach **Priorität** (Pull-Requests haben Vorrang vor Releases, und Releases haben Vorrang vor Matrix-Tests).
- Er verteilt die Pakete an eine konfigurierbare Anzahl von **Slots** (Worker-Threads).
- Ein Slot nimmt ein Paket und startet eine **CompilerSession**. Er bootet hierfür isolierte Docker-Container auf dem Host-System und injiziert über Volumes den Code.

### C. Speicher-Sicherheit (Sibling Containers)
Der Daemon führt die Tests nicht in sich selbst aus. Über den *Docker-Socket* spawnt er die Test-Umgebungen als Geschwister-Container (Sibling Containers) neben sich selbst. Sollte ein C-Build Amok laufen und Gigabytes an RAM fressen, wird der Test-Container vom Host-OS gekillt, ohne den Orchestrator (Toob-CI) mit in den Tod zu reißen.

---

## 4. Inbox, Outbox & Ledger (Ergebnis-Verarbeitung)

Das größte Risiko in automatisierten CI-Systemen sind **Race Conditions** (wenn zwei Prozesse gleichzeitig eine Datei schreiben) und das **Aufblähen (Bloating)** von JSON-Dateien mit Fehler-Logs. Der Orchestrator löst dies elegant:

### A. Die Inbox (Lokale Fragmentierung)
Nach jedem einzelnen Matrix-Test (egal in welchem Slot) schreibt der Worker eine kleine, temporäre JSON-Datei (z.B. `result_esp32c6_v1.0.0_...json`) in den Ordner `/repo-registry`. Dies ist die **Inbox**. Das Schreiben dieser kleinen Dateien passiert völlig lock-free und blitzschnell.

### B. Batching
Der Planner hat einen internen Mutex-gesicherten Zähler (`p.matrixBatches`). Er wartet, bis genau **20 Ergebnisse** in der Inbox liegen. Erst dann friert er den Status kurz ein und löst den Ledger-Commit aus (`commitMatrixLedger()`).

### C. Der Ledger Aggregator (`update_ledger.go`)
Dieses Skript sammelt die 20 Fragmente aus der Inbox ein.
- **VERIFIED:** Ist der Test erfolgreich, wird er in die große, öffentliche `compatibility_matrix.json` aufgenommen.
- **FAILED:** Ist der Test fehlgeschlagen, landet er **nicht** in der öffentlichen Matrix. Stattdessen wird er in die **Outbox** gepackt.

### D. Die Outbox (`internal_state.json`)
Die Outbox ist eine unsichtbare Datei, die den Zustand von kaputten oder problematischen Chips trackt.
- **Normaler Fehler:** Code baut nicht -> Status `FAILED`.
- **Infrastruktur-Fehler:** GitHub war down, Docker-Image wurde nicht gefunden -> Status `INFRA_ERROR`.
Die Outbox merkt sich, wie oft ein Infrastruktur-Fehler passiert ist (`RetryCount`). Tritt er zweimal in Folge auf, eskaliert das System den Fehler zu `FATAL_INFRA_ERROR`. Der Chip wird dann komplett ignoriert, bis ein Mensch eingreift. Dies verhindert Endlos-Schleifen in der Farm.

### E. Sicherer Git-Push
Nachdem das Aggregator-Skript fertig ist, committet der Worker die Ergebnisse via Git. Bevor er die neuen Daten zu GitHub pusht, führt er zwingend einen `git pull --rebase` aus. 
Falls in der Zwischenzeit ein Entwickler einen Code-PR auf GitHub gemerged hat, wird der Ledger-Commit sauber und konfliktfrei oben auf die Historie gelegt. Erst danach erfolgt der Push.

---

## 5. Ausfallsicherheit und Resilienz

- **Verbindungsabbruch zu GitHub:** Wenn die Hetzner-Server im genauen Moment des `git push` die Verbindung verlieren, stürzt nichts ab. Die Änderungen an der `compatibility_matrix.json` wurden lokal bereits als Git-Commit gesichert. Sobald eine Stunde später der nächste Batch anläuft, wird Git automatisch den hängengebliebenen Commit von vorhin mit auf den Server schieben. Nichts geht verloren.
- **Graceful Shutdown:** Erhält der Daemon ein Stop-Signal (z.B. durch ein Server-Neustart), unterbricht er nicht sofort hart. Er fängt den Befehl ab (`SIGTERM`), schließt die Queue und wartet geduldig, bis die aktuell laufenden Docker-Compiler-Sessions sauber beendet und ihre Ergebnisse gespeichert sind, bevor er sich abschaltet.
