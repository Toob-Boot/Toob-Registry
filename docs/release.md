# Toob Release Pipeline

All components live in the `Toob-Boot/Toob-Loader` monorepo. Version tagging is fully automated by the **SemVer Enforcer** (`semver-enforcer.yml`), which runs on GitHub Actions on every push to `main`.

## Architecture

```
Push to main
     │
     ▼
SemVer Enforcer (GitHub Actions)
     │  AST-diff (CLI), ABI-diff (Core), Manifest-diff (Compiler)
     │
     ├── cli/vX.Y.Z tag ──► CI Server (act) ──► release-cli.yml
     │                              │
     │                              ▼
     │                        Draft Release on Toob-CLI-Release
     │                              │
     │                         [Manual Publish]
     │                              │
     │                              ▼
     │                     cli-release-notify.yml (GitHub Actions)
     │                              │
     │                              ▼
     │                     sync-cli-release.yml (GitHub Actions)
     │                     updates compiler_manifest.json
     │                              │
     │                              ▼
     │                     Enforcer runs again → compiler/vX.Y.Z tag
     │
     ├── compiler/vX.Y.Z tag ──► CI Server (act) ──► release-compiler.yml
     │                                                      │
     │                                                      ▼
     │                                               Docker Hub push
     │
     └── core/vX.Y.Z tag ──► CI Server (act) ──► release-core.yml
```

**Key constraint:** Tags pushed by GitHub Actions do not trigger webhooks. The CI server must be notified manually or via `repository_dispatch` for compiler and core releases.

---

## 1. CLI

**Source:** `cli/toob-cli/` in the monorepo.  
**Distribution:** GitHub Releases on `Toob-Boot/Toob-CLI-Release`.

### Automatic Flow

1. Push to `main` with changes in `cli/toob-cli/`
2. Enforcer compares `ports.go` AST against last `cli/v*` tag
3. Determines bump: MAJOR (breaking), MINOR (new exports), PATCH (internal)
4. Pushes `cli/vX.Y.Z` tag
5. CI server runs `release-cli.yml` via `act`:
   - Cross-compiles for `windows/amd64`, `linux/amd64`, `darwin/amd64`, `darwin/arm64`
   - UPX compression, SHA256 checksums, Minisign signatures
   - Creates **draft** release on `Toob-CLI-Release`
6. **Manual step:** Publish the draft on GitHub
7. Publishing triggers `cli-release-notify.yml` → dispatches `cli_published` to monorepo
8. `sync-cli-release.yml` updates `compiler_manifest.json` with new CLI version
9. This commit triggers the Enforcer again → new `compiler/v*` tag

### Manual Release

```bash
git tag cli/vX.Y.Z
git push origin cli/vX.Y.Z
```

---

## 2. Compiler Image

**Source:** `compiler/compiler_manifest.json` + `cli/.pipeline-repo/Dockerfile.compiler`.  
**Distribution:** `mannomannx/toob-compiler` on Docker Hub.

### Manifest Fields

| Field | Bump Level |
|-------|-----------|
| `protocol_version` | MAJOR |
| `base_image`, `system_packages` | MINOR |
| Everything else | PATCH |

### Automatic Flow

1. Enforcer detects changes in `compiler/*`, `Dockerfile.compiler`, or `toob-ci-build.sh`
2. Compares manifest against last `compiler/v*` tag, determines bump
3. Writes new `compiler_version`, commits `[skip ci]`, pushes tag
4. CI server runs `release-compiler.yml` via `act`:
   - Reads CLI version, protocol version from manifest
   - Builds Docker image with pinned CLI binary
   - Pushes to Docker Hub as `vX.Y.Z` + `latest`
   - Updates `compiler_version` in manifest, commits `[skip ci]`
   - Triggers `version-index.yml` in Registry

### Manual Release

```bash
# Verify manifest has correct CLI version (must be a published release)
jq '.cli.version, .cli.source.ref' compiler/compiler_manifest.json

git tag compiler/vX.Y.Z
git push origin compiler/vX.Y.Z
```

> **Important:** The CLI version in the manifest must point to a **published** GitHub Release. Draft releases don't have downloadable binaries.

---

## 3. Core SDK

**Source:** `toobloader/`, `sdk/`, `common/` in the monorepo.  
**Distribution:** GitHub Releases on `Toob-Boot/Toob-Loader`.

1. Enforcer compiles old and new C code, runs `abidiff`
2. Pushes `core/vX.Y.Z` tag
3. CI server runs `release-core.yml` via `act`

---

## 4. Registry

**Source:** `chips/`, `arch/`, `vendor/`, `toolchains/` in `Toob-Registry`.  
**Distribution:** Git tags on `Toob-Boot/Toob-Registry`.

1. Merge to `main` with hardware manifest changes
2. `main-release.yml` calculates SemVer inheritance chain
3. Bumps `registry_version`, pushes tag (e.g. `v1.0.8`)
4. Triggers `version-index.yml` and `compatibility-matrix.yml`

### SemVer Inheritance

Changes cascade upward. The highest bump in any dependency determines the parent's bump:

```
vendor/esp (PATCH) + arch/riscv32 (MINOR)
  → chip/esp32c6 inherits MINOR
    → registry_version inherits MINOR
```

---

## 5. Infrastructure

### CI Server

- **Host:** Hetzner VPS at `ci.the-toob.com`
- **Stack:** Docker Compose (`toob-ci` daemon + Caddy reverse proxy)
- **Execution:** GitHub workflows run via [nektos/act](https://github.com/nektos/act) inside `toob-release-runner` containers

### Webhook Configuration (GitHub → CI Server)

| Setting | Value |
|---------|-------|
| Payload URL | `https://ci.the-toob.com/hooks/release` |
| Content type | `application/json` |
| Secret | Same as `WEBHOOK_SECRET` in `docker-compose.yml` |
| Events | Pushes |

### Known Limitation

Tags pushed by GitHub Actions workflows (e.g. by the Enforcer) do **not** trigger GitHub webhooks. Compiler and Core releases therefore require either:
- A manual webhook simulation from the CI server
- A `repository_dispatch` from the Enforcer to the CI server

---

## 6. Version Index

`version_index.json` is the single aggregated view of all ecosystem versions. It is regenerated by `version-index.yml` after every release and pulls data from:
- GitHub API (CLI releases, Core releases, Registry tags)
- Docker Hub API (Compiler image tags)
- Local `registry.json` (hardware manifest versions)
