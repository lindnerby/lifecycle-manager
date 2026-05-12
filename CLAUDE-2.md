# CLAUDE.md

Kyma Lifecycle Manager (KLM) is a Kubernetes operator built with kubebuilder and controller-runtime in Go.
It manages the lifecycle of Kyma modules running on *SAP BTP, Kyma Runtimes* (SKRs) including their installation, update and uninstallation.
KLM runs on a central **control plane (KCP)** Kubernetes cluster and manages remote **SKR** Kubernetes clusters.

To build KLM, run `make build`.

## KLM works with the following Custom Resources (CRs)

- **Kyma** - defines what modules shall be installed on the SKR. The user chooses a channel, not a specific version of the module. Lives on KCP and SKR.
- **ModuleTemplate** - defines the metadata of a module version. Lives on KCP and SKR.
- **ModuleReleaseMeta** - assigns module versions to channels or defines the module as mandatory and which version to install. Lives on KCP and SKR.
- **Manifest** - defines a single installation of a module on a specific SKR. Lives on KCP only.
- **Watcher** - configures a webhook to be installed on the SKR. The webhook notifies KLM of changes to resources on the SKR.

These are defined in a separate Go module in `api/v1beta2`.

## KLM uses the following controllers

- **Kyma controller** - syncs essential resources to the SKR. Creates, updates and deletes Manifest CRs. Tracks the overall status of module installations and the SKR.
- **Manifest controller** - installs, updates and uninstalls module resources on the SKR. Tracks the status of the module installation.
- **Mandatory Module Installation controller** - creates and updates Manifest CRs for mandatory modules.
- **Mandatory Module Deletion controller** - deletes Manifest CRs for mandatory modules.
- **Purge controller** - purges Custom Resource Definitions from SKRs after a certain timeout when these block the deprovisioning of the SKR.
- **Watcher controller** - installs the watcher webhook to the SKR and configures ingress on KCP.
- **Istio Gateway Secret controller** - manages the certificate rotation in the Public Key Infrastructure (PKI) inbound Watcher traffic.

These are defined in `internal/controller`.

## KLM follows the following key architectural decisions

- **ADR 001** - prefer Consumer-Defined Interfaces.
- **ADR 002** - inject Dependencies via Constructor in Composition Functions.
  - the composition root is resolved in `cmd/main.go`
- **ADR 003** - use the most specific client interface from controller-runtime and use it at the Repository layer only.
- **ADR 004** - adopt a Layered Architecture consisting of Controller -> Service -> Repository layers. No layer may reference or depend on a higher layer.
  - the layers are represented in `internal/controller`, `internal/service`, `internal/repository`
- **ADR 005** - suffix types with Controller, Service, Repository; don't suffix with Interface or Impl; let the package provide the context.

These are defined in `docs/contributor/adr`.
Only load the entire ADR if considered **relevant** for the task.
Not the entire project follows the ADRs yet, but all **new** code **MUST** follow them.

## KLM uses a unit, integration and e2e test pyramid

- **Unit tests** - are co-located with the source files as `*_test.go` and defined in a `*_test` package.
  - to run all unit tests for KLM: `gmake unittest-klm`
  - to run a specific test: `go test <path> -run <test name> -v`
- **Integration tests** - are located in `tests/integration`
  - to run all integration tests: `gmake test`
  - to run a specific suite: `KUBEBUILDER_ASSETS="$(./bin/setup-envtest use <envtest_k8s from versions.yaml> -p path)" go test <path> -v`
- **E2E tests** - are located in `tests/e2e`
  - to run a specific test `gmake -f ./tests/e2e/Makefile <test name>`
    - note that only those tests forwarding to a dedicated makefile are supported like this yet

## What behavioral guidelines to consider

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them - don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it - don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.
