# Development Guidelines and Contribution Standards: `PICC-PC-Haproxy-Spoa`

This document defines the architectural standards, development workflows, coding conventions, and security requirements for contributors to **`PICC-PC-Haproxy-Spoa`**.

---

## Table of Contents

1. [Architecture & Cloud-Native Principles](#1-architecture--cloud-native-principles)
2. [Development Environment Setup](#2-development-environment-setup)
3. [Repository Structure & Code Navigation](#3-repository-structure--code-navigation)
4. [Coding Standards & Best Practices](#4-coding-standards--best-practices)
   - [SPOE Binary Frame Protocol Safety](#spoe-binary-frame-protocol-safety)
   - [Externalized 12-Factor Configuration](#externalized-12-factor-configuration)
   - [Hardened HTTP Client & Upstream Resiliency](#hardened-http-client--upstream-resiliency)
   - [Zero-Trust Fail-Safe Authorization](#zero-trust-fail-safe-authorization)
   - [Graceful Shutdown & Signal Handling](#graceful-shutdown--signal-handling)
   - [Logging & Sensitive Data Handling](#logging--sensitive-data-handling)
5. [Security, Code Quality & Compliance Tooling](#5-security-code-quality--compliance-tooling)
   - [SAST: Go Vet & Govulncheck](#sast-go-vet--govulncheck)
   - [SBOM: CycloneDX Aggregate Generation](#sbom-cyclonedx-aggregate-generation)
   - [Unprivileged Container Compliance](#unprivileged-container-compliance)
6. [Git Workflow & Branching Strategy](#6-git-workflow--branching-strategy)
   - [Branch Naming Conventions](#branch-naming-conventions)
   - [Conventional Commits](#conventional-commits)
7. [Pull Request (PR) Checklist](#7-pull-request-pr-checklist)
8. [Release Lifecycle & Versioning](#8-release-lifecycle--versioning)

---

## 1. Architecture & Cloud-Native Principles

`PICC-PC-Haproxy-Spoa` acts as a high-performance **Stream Processing Offload Agent (SPOA)** for HAProxy. It offloads dynamic access control and URL authorization decisions from the HAProxy data path to a dedicated Go service communicating via the binary SPOE protocol (over TCP port `9000`).

Adhering to Cloud Native Computing Foundation (CNCF) design principles:

1. **Config via Environment (12-Factor)**: All network ports, timeouts, upstream URLs, and bypass rules are externalized to environment variables with documented fallback defaults. Hardcoding IP addresses, company hostnames, or credentials in source code is strictly prohibited.
2. **Zero-Trust Fail-Safe Execution**: In the event of network partitioning, upstream auth timeouts, unescapable query strings, or missing session headers, the agent fails closed (`allow=false`). The server must never crash or panic on malformed input.
3. **Resilient Connection Pooling**: Outbound HTTP communication with the upstream authorization service uses explicit timeouts and keep-alive connection pooling to prevent socket exhaustion under heavy load.
4. **Dual-Plane Architecture**: The agent isolates the high-throughput binary SPOE engine (port `9000`) from the HTTP diagnostic plane (port `8080`), providing dedicated Kubernetes `/healthz`, `/readyz`, and `/livez` probes.
5. **Least Privilege Runtime**: Containers execute under an unprivileged UID/GID (`10001:10001`) with read-only root filesystem compatibility.

---

## 2. Development Environment Setup

### Required Tools
- **Go 1.26+** (tested with Go 1.26+ and Go 1.27+).
- **Docker** and **Docker Compose** for containerized testing.
- **HAProxy 2.8+** (optional, for end-to-end SPOE integration tests).
- **Git** configured with LF line endings (`core.autocrlf = input`).

### Quick Setup

```bash
# Clone the repository
git clone https://github.com/Nubo-Native-Platform/PICC-PC-Haproxy-Spoa.git
cd PICC-PC-Haproxy-Spoa

# Copy environment template
cp .env.example .env

# Verify dependencies
go mod download
go mod verify

# Run automated tests
go test -v ./...
```

---

## 3. Repository Structure & Code Navigation

```
PICC-PC-Haproxy-Spoa/
├── .github/
│   └── workflows/
│       └── ci-cd.yml             # GitHub Actions automated test, security scan & build
├── .env.example                  # Environment configuration template
├── .gitattributes                # Line ending and language normalization
├── .gitignore                    # Git ignore rules for Go and containers
├── Dockerfile                    # Multi-stage CNCF-compliant unprivileged Dockerfile
├── docker-compose.yml            # Local orchestration definition
├── go.mod                        # Go module definition
├── go.sum                        # Go module cryptographic checksums
├── spoa.go                       # Core SPOA agent implementation & HTTP probe server
├── spoa_test.go                  # Unit tests for parser, auth client & health endpoints
├── CODE_OF_CONDUCT.md            # Contributor Covenant Code of Conduct
├── CONTRIBUTING.md               # Contribution workflow and DCO sign-off guidelines
├── DEVELOPMENT_GUIDELINES.md     # Engineering, architectural, and security guidelines
├── USER_MANUAL_AND_DEPLOYMENT_GUIDE.md # Production deployment, HAProxy & K8s manual
├── LICENSE                       # Apache License 2.0
├── MAINTAINERS.md                # Project maintainers and contact points
├── README.md                     # Project overview, architecture & quick start
└── SECURITY.md                   # Security vulnerability reporting policy
```

---

## 4. Coding Standards & Best Practices

### SPOE Binary Frame Protocol Safety
- All arguments extracted from `spoe.Message` must be defensively converted to string values with nil checks.
- Parameter extraction should be consolidated into a single pass (`extractMessageArgs`) to avoid iterator cursor invalidation.
- Never invoke `panic()` in message handlers. Return graceful error logs and emit a deny action (`spoe.ActionSetVar{Name: "allow", Value: false}`).

### Externalized 12-Factor Configuration
- Read configuration through `LoadConfig()`.
- Provide sensible defaults for local development (`:9000` for SPOA, `:8080` for health probe).
- Ensure port variables support both `:PORT` and bare `PORT` inputs.

### Hardened HTTP Client & Upstream Resiliency
- Do NOT use `http.DefaultClient` or package-level `http.Post()`.
- Use a configured `http.Client` with `Transport` connection pooling:
  - `MaxIdleConns: 100`
  - `MaxIdleConnsPerHost: 50`
  - `IdleConnTimeout: 90s`
- Always bind an explicit context with timeout (`context.WithTimeout`) to outbound HTTP requests.

### Graceful Shutdown & Signal Handling
- Capture `os.Interrupt` and `syscall.SIGTERM`.
- Allow in-flight SPOE requests up to 5 seconds to drain before closing listeners.

### Logging & Sensitive Data Handling
- Do not log full authorization tokens, passwords, or PII in cleartext.
- Restrict verbose request/response payloads to `LOG_LEVEL=DEBUG`.

---

## 5. Security, Code Quality & Compliance Tooling

### SAST: Go Vet & Govulncheck
All pull requests must pass static analysis and vulnerability scanning:
```bash
# Go Vet
go vet ./...

# Govulncheck
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

### SBOM: CycloneDX Aggregate Generation
Supply chain integrity is enforced by generating CycloneDX Software Bill of Materials:
```bash
go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest
cyclonedx-gomod app -json -output bom.json .
```

---

## 6. Git Workflow & Branching Strategy

### Conventional Commits
All commit messages must follow the [Conventional Commits v1.0.0](https://www.conventionalcommits.org/) standard:
```
<type>(<scope>): <short summary>

[optional body]
```
- `feat`: A new feature or capability.
- `fix`: A bug fix.
- `docs`: Documentation updates.
- `refactor`: Code changes that neither fix a bug nor add a feature.
- `test`: Adding or refactoring tests.
- `ci`: Changes to CI/CD workflows and automation.

---

## 7. Pull Request (PR) Checklist

Before submitting a Pull Request:
- [ ] Code builds cleanly without warnings: `go build -v .`
- [ ] All tests pass: `go test -v ./...`
- [ ] Code passes static analysis: `go vet ./...`
- [ ] No hardcoded passwords, tokens, internal cluster URLs, or proprietary IPs exist.
- [ ] Git commit message adheres to Conventional Commits format.
- [ ] Commits are signed off (`git commit -s`) per Developer Certificate of Origin (DCO).

---

## 8. Release Lifecycle & Versioning

Releases follow [Semantic Versioning (SemVer 2.0.0)](https://semver.org/): `v<MAJOR>.<MINOR>.<PATCH>`.
Release tags trigger the GitHub Actions build pipeline to publish container images to GitHub Container Registry (`ghcr.io`).
