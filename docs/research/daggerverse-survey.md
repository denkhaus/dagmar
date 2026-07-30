# Daggerverse Module Survey for Dagmar

**Research Date:** 2026-07-30
**Dagger Version:** v0.21.8 (dagmar target)
**Issue:** [#6](https://github.com/denkhaus/dagmar/issues/6)

## Summary

Dagmar requires capabilities for: git clone/commit, GitHub PRs/issues, Go build & test, LLM calls inside Dagger sandbox, container/image ops, HTTP, and Kubernetes. This survey identifies existing Daggerverse modules that can be reused versus those that need to be built.

**Key Findings:**
- **Git operations:** Native Dagger API (`dag.git()`) provides full git clone/commit - no module dependency needed
- **GitHub API:** No mature PR/issue module exists - **BUILD REQUIRED**
- **Go build & test:** `sagikazarmark/daggerverse/go` (v0.9.0, MIT) is production-ready - **DEPEND**
- **LLM/AI:** `dagger/modules/evaluator` (v0.19.x, Apache-2.0) - **DEPEND/ADAPT**
- **Container/image ops:** Native Dagger API provides full capabilities - no module needed
- **HTTP:** Native `dag.http()` API - no module needed
- **Kubernetes:** `marcosnils/daggerverse/k3s` (v0.11.1), `matipan/daggerverse/kubectl` (v0.0.4), `sagikazarmark/daggerverse/helm` (v0.15.0) - **DEPEND**

## Catalogue Table

| Capability | Module | Source | Maturity | License | v0.21.8 Compatible | Recommendation |
|------------|--------|--------|----------|---------|-------------------|----------------|
| **Git Clone/Commit** | Native API | `dag.git()` | Built-in | Apache-2.0 | Yes | **USE NATIVE** |
| **GitHub PRs/Issues** | None | N/A | N/A | N/A | N/A | **BUILD** |
| **Go Build & Test** | `sagikazarmark/daggerverse/go` | [github.com/sagikazarmark/daggerverse/go](https://github.com/sagikazarmark/daggerverse/go) | Production (v0.9.0, Jun 2026) | MIT | Likely (v0.9.0) | **DEPEND** |
| **Go Build & Test (Alt)** | `dagger/modules/go` | [github.com/dagger/dagger/modules/go](https://github.com/dagger/dagger/modules/go) | Official (v0.19.5) | Apache-2.0 | Yes | **CONSIDER** |
| **LLM/AI Sandbox** | `dagger/modules/evaluator` | [github.com/dagger/dagger/modules/evaluator](https://github.com/dagger/dagger/modules/evaluator) | Official (v0.19.10) | Apache-2.0 | Yes | **ADAPT** |
| **Container/Image Ops** | Native API | `dag.container()` | Built-in | Apache-2.0 | Yes | **USE NATIVE** |
| **HTTP Requests** | Native API | `dag.http()` | Built-in | Apache-2.0 | Yes | **USE NATIVE** |
| **Kubernetes (k3s)** | `marcosnils/daggerverse/k3s` | [github.com/marcosnils/daggerverse/k3s](https://github.com/marcosnils/daggerverse/k3s) | Production (v0.11.1, Jul 2026) | MIT | Likely | **DEPEND** |
| **Kubernetes (kubectl)** | `matipan/daggerverse/kubectl` | [github.com/matipan/daggerverse/kubectl](https://github.com/matipan/daggerverse/kubectl) | Early (v0.0.4) | MIT | Unknown | **EVALUATE** |
| **Kubernetes (Helm)** | `sagikazarmark/daggerverse/helm` | [github.com/sagikazarmark/daggerverse/helm](https://github.com/sagikazarmark/daggerverse/helm) | Production (v0.15.0) | MIT | Likely | **DEPEND** |
| **GitHub Actions Mgmt** | `dagger/modules/gha` | [github.com/dagger/dagger/modules/gha](https://github.com/dagger/dagger/modules/gha) | Official | Apache-2.0 | Yes | **LIMITED USE** |
| **Git Release** | `dagger/modules/git-releaser` | [github.com/dagger/dagger/modules/git-releaser](https://github.com/dagger/dagger/modules/git-releaser) | Official | Apache-2.0 | Yes | **LIMITED USE** |

## Detailed Findings by Capability

### 1. Git Clone/Commit
**Status: USE NATIVE API**

Dagger provides built-in git operations via `dag.git()`:
- Clone public/private repositories
- SSH authentication support
- Branch/tag checkout
- Tree access for container mounting

**Source:** [Dagger Cookbook - Clone a remote Git repository](https://docs.dagger.io/cookbook/filesystems/clone-a-remote-git-repository-into-a-container)

```go
repoDir := dag.Git("https://github.com/user/repo").Ref("main").Tree()
```

**Recommendation:** Do NOT depend on a module. Use native `dag.git()` API.

### 2. GitHub PRs/Issues
**Status: BUILD REQUIRED**

No mature Dagger module exists for GitHub PR/issue operations. The official `gha` module only manages GitHub Actions configurations, not PRs/issues.

**Search results from daggerverse.dev:**
- No GitHub API modules found
- `gha` module: Actions config only, not PR/issue CRUD

**Recommendation:** BUILD dagmar's own GitHub module wrapping GitHub REST API v3 (via `dag.http()` or gh CLI in container).

### 3. Go Build & Test
**Status: DEPEND ON EXISTING MODULE**

Two strong candidates:

**Primary: `sagikazarmark/daggerverse/go`**
- Latest: v0.9.0 (2026-06-18)
- License: MIT
- Featured on daggerverse.dev
- Active development
- Description: "Go programming language module"

**Alternative: `dagger/modules/go`** (Official)
- Latest: v0.19.5 (2025-11-11)
- License: Apache-2.0
- Official Dagger module
- Better version alignment with Dagger v0.21.8

**Recommendation:** DEPEND on `sagikazarmark/daggerverse/go` for community activity, or `dagger/modules/go` for official support and license compatibility with dagmar (Apache-2.0).

### 4. LLM/AI in Dagger Sandbox
**Status: ADAPT OFFICIAL MODULE**

**`dagger/modules/evaluator`**
- Latest: v0.19.10 (2026-01-14)
- License: Apache-2.0
- Featured on daggerverse.dev
- Purpose: "Evaluating and improving LLM performance across multiple models"

This module provides patterns for running LLM calls inside Dagger containers. Dagmar can adapt this for its AI agent needs.

**Recommendation:** DEPEND on `dagger/modules/evaluator` as a reference/adaptation for LLM execution patterns.

### 5. Container/Image Operations
**Status: USE NATIVE API**

Dagger's core `dag.container()` provides:
- Image builds (`from()`, `withExec()`)
- Publishing (`publish()`)
- Multi-stage builds
- Registry operations

**Source:** [Dagger Cookbook - Builds](https://docs.dagger.io/cookbook/builds)

**Recommendation:** Do NOT depend on a module. Use native `dag.container()` API.

### 6. HTTP Requests
**Status: USE NATIVE API**

Dagger provides `dag.http()` for fetching files over HTTP/HTTPS.

**Source:** [Dagger Cookbook - Request a file over HTTP/HTTPS](https://docs.dagger.io/cookbook/filesystems/request-a-file-over-httphttps-and-save-it-in-a-container)

```go
file := dag.HTTP("https://example.com/file")
```

**Recommendation:** Do NOT depend on a module. Use native `dag.http()` API.

### 7. Kubernetes
**Status: DEPEND ON EXISTING MODULES**

Three relevant modules found:

**`marcosnils/daggerverse/k3s` (Recommended)**
- Latest: v0.11.1 (2026-07-30)
- License: MIT
- Featured on daggerverse.dev
- Description: "Runs a k3s server accessible locally and in pipelines"
- Active development

**`matipan/daggerverse/kubectl`**
- Latest: v0.0.4 (2025-05-06)
- License: MIT
- Early maturity
- Description: "kubectl with many authentication methods"

**`sagikazarmark/daggerverse/helm`**
- Latest: v0.15.0 (2026-01-13)
- License: MIT
- Featured on daggerverse.dev
- Description: "Package manager for Kubernetes"

**Recommendation:** DEPEND on `marcosnils/daggerverse/k3s` for full K8s sandbox, `sagikazarmark/daggerverse/helm` for Helm operations.

## Recommendations by Capability

| Capability | Action | Module/Approach |
|------------|--------|-----------------|
| Git clone/commit | Use native API | `dag.git()` |
| GitHub PRs/issues | **BUILD** | Create new `github` module |
| Go build & test | Depend | `sagikazarmark/daggerverse/go@v0.9.0` (MIT) or `dagger/modules/go@v0.19.5` (Apache-2.0) |
| LLM/AI calls | Adapt/Depend | `dagger/modules/evaluator@v0.19.10` |
| Container/image ops | Use native API | `dag.container()`, `.publish()` |
| HTTP requests | Use native API | `dag.http()` |
| Kubernetes (k3s) | Depend | `marcosnils/daggerverse/k3s@v0.11.1` (MIT) |
| Kubernetes (kubectl) | Evaluate | `matipan/daggerverse/kubectl@v0.0.4` |
| Kubernetes (Helm) | Depend | `sagikazarmark/daggerverse/helm@v0.15.0` (MIT) |

## License Compatibility Notes

- **Dagger core:** Apache-2.0
- **dagmar:** Should adopt Apache-2.0 for compatibility
- **MIT licenses:** Permissive, compatible with Apache-2.0
- **All identified modules:** MIT or Apache-2.0 - all compatible

## Open Questions

1. **Go module choice:** `sagikazarmark/daggerverse/go` (MIT, more active) vs `dagger/modules/go` (Apache-2.0, official)?
2. **kubectl module maturity:** `v0.0.4` is very early - evaluate before depending
3. **GitHub API scope:** Should dagmar's GitHub module cover more than PRs/issues (comments, reactions, etc.)?
4. **LLM provider support:** Which providers does dagmar need? The evaluator module supports multiple - extend or create new?

## Source Links

- [Daggerverse Directory](https://daggerverse.dev/)
- [Dagger Cookbook](https://docs.dagger.io/cookbook/)
- [Dagger Documentation](https://docs.dagger.io/)
- [sagikazarmark/daggerverse](https://github.com/sagikazarmark/daggerverse)
- [marcosnils/daggerverse](https://github.com/marcosnils/daggerverse)
- [dagger/dagger (official modules)](https://github.com/dagger/dagger/tree/main/modules)

---

**Related:** [Issue #6](https://github.com/denkhaus/dagmar/issues/6) | [Wayfinder Ticket #6](https://github.com/denkhaus/dagmar/issues/1)
