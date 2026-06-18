# Project Conventions

These conventions apply to this repository and can be adopted by any repository
following the same standards.

## Container Engine

- The primary container engine is **podman**
- All Makefiles and scripts must use a `CONTAINER_SUBSYS` variable (defaulting
  to `podman`) to allow overriding if desired
- Use `.containerignore` for build context exclusion (podman's native format)
- Never reference docker-specific tooling unless required for compatibility

## Base Images

- Prefer **fedora-minimal** for lightweight containers (application and CI images)
- Prefer **UBI (Universal Base Image)** when Red Hat support or certification is needed
- Always pin base image tags to a specific version — never use `:latest`
- Use known, trusted registries (registry.fedoraproject.org, registry.access.redhat.com,
  quay.io, ghcr.io, etc.)

## CI Testing

### Containerized CI

All CI checks run inside a dedicated CI container image. This guarantees that
local and remote CI environments are identical — no environment drift, no
"works on my machine" failures.

- A `test/Containerfile.ci` defines the CI container with all lint and
  validation tools pre-installed
- `make ci-all` builds the CI container and runs `make ci-checks` inside it
  serially — this is the local developer entry point
- The GHA workflow builds the same CI container image once, then fans out to
  parallel jobs that each run a single `make <target>` inside that image
- Tests that require the host container engine (e.g., building the application
  image) run directly on the GHA runner, not inside the CI container

### Local vs Remote Execution

- **Locally**: `make ci-all` runs all checks serially in a single container
  invocation for simplicity
- **Remotely (GHA)**: Each check runs as a separate parallel job using the
  same CI container image, for faster feedback
- Both paths use the same Makefile targets and the same container image,
  ensuring identical behavior

### Makefile Structure for CI

The Makefile must provide:

- `ci-build` — Build the CI container image
- `ci-all` — Build the CI container and run `ci-checks` inside it (local entry point)
- `ci-checks` — Run all checks serially (intended to run inside the CI container)
- Individual check targets (e.g., `yaml-lint`, `markdown-lint`) — each runnable
  independently inside the CI container, enabling GHA parallelism

### Required CI Checks

Every repository should include the following checks, as applicable:

| Check | Tool | Applies To |
| ----- | ---- | ---------- |
| YAML lint | yamllint | All YAML files |
| Markdown lint | markdownlint-cli2 | All Markdown files |
| Makefile lint | checkmake | Makefile |
| Containerfile check | custom script | Containerfile base image tags and registries |
| Kubernetes validation | kubeconform | Kubernetes manifests |
| Python lint | ruff | Python files |
| Shell lint | shellcheck | Shell scripts |
| Documentation check | find | Plan documents in docs/plans/ |
| Container image build | podman | Application Containerfile builds and runs |
| OCI label validation | podman inspect | Required OCI labels present on built images |

## Makefile Standards

- All targets must be declared `.PHONY`
- Must include `clean` and `test` targets
- `test` should run the full CI suite (`ci-all`)
- Use variables for configurable values (container engine, registry, image name)
- Support `.env` files for local configuration overrides

## OCI Image Labels

All container images must include these standard labels:

- `org.opencontainers.image.title`
- `org.opencontainers.image.description`
- `org.opencontainers.image.revision` (git commit SHA)
- `org.opencontainers.image.version`
- `org.opencontainers.image.source` (repository URL)

## Linting

- Fix all lint issues rather than suppressing rules, unless there is a
  documented reason to disable a specific rule
- Linter configurations live in the repository root (`.yamllint.yaml`,
  `.markdownlint.yaml`, etc.)

## Documentation

### Plan Documents

- Every change must have an associated plan document in `docs/plans/`
- Plan documents use descriptive filenames (e.g., `container-based-ci.md`),
  not numeric prefixes — PR/issue numbers are not known until after creation
- Plans must consider lessons learned from previous plans in the same directory
- Superseded plans are preserved with a clear note at the top pointing to
  the replacement plan

### Markdown

- All Markdown must pass markdownlint
- Use fenced code blocks with language identifiers
- Tables must have properly spaced separators
- Lists must be surrounded by blank lines

## YAML

- All YAML must pass yamllint
- Use 2-space indentation
- No document-start markers required
- Kubernetes env var values must be strings (quoted numbers)

## Version Control

- Work in feature branches, never directly on main
- Commits must be signed off (DCO)
- Commit messages should be concise with a descriptive body
