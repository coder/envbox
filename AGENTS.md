# AGENTS.md

## For AI Coding Agents

This document helps AI coding agents work effectively with the envbox project. Read this first before making changes.

## ⚠️ CRITICAL: Always Start From Latest Main

**ALWAYS pull latest main and create a feature branch. NEVER push directly to main.**

### Starting Any New Task

Before starting any work, **ALWAYS** do this:

```bash
# 1. Get latest main
git checkout main
git pull origin main

# 2. Create feature branch from latest main
git checkout -b your-branch-name

# 3. Make your changes and commit them
# ... do your work ...
git add .
git commit -m "your message"

# 4. Push to YOUR branch (not main!)
git push origin your-branch-name

# 5. Create a Pull Request for review
```

**DO NOT:**
- ❌ Start work without pulling latest main first
- ❌ Run `git push origin main` - this pushes directly to main and bypasses code review
- ❌ Create branches from stale/outdated main branches

## What is envbox?

envbox enables running non-privileged containers capable of running system-level software (e.g., `dockerd`, `systemd`) in Kubernetes. It wraps [Nestybox sysbox](https://github.com/nestybox/sysbox/) to provide secure Docker-in-Docker capabilities.

**Primary use case**: [Coder](https://coder.com) workspaces, though the project is general-purpose.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│ Outer Container (Privileged)                                │
│  - Runs on the Kubernetes node                              │
│  - Starts sysbox-mgr, sysbox-fs, and dockerd                │
│  - Managed by the envbox binary (/envbox)                   │
│  - Has elevated privileges to manage namespaces             │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐ │
│  │ Inner Container (Unprivileged - MUST STAY SECURE)     │ │
│  │  - User's actual workspace/workload                    │ │
│  │  - Created via sysbox runtime                          │ │
│  │  - Runs dockerd, systemd, or other system software     │ │
│  │  - NEVER privileged - this is the security model       │ │
│  │  - Name: "workspace_cvm" (InnerContainerName)          │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### Key Architectural Points

1. **Two-tier container model**: Outer (privileged) manages inner (unprivileged)
2. **Security boundary**: The inner container must remain unprivileged - this is non-negotiable
3. **Sysbox integration**: The outer container runs sysbox components that enable the inner container to run system-level software securely
4. **User namespace mapping**: UID/GID offset of 100000 (UserNamespaceOffset constant)

## 🚨 Critical Security Rule

**NEVER make the inner container privileged.**

The entire premise of envbox is providing secure system-level capabilities without granting actual privilege. The inner container is explicitly set to `Privileged: false` (see `cli/docker.go:732`). Any change that compromises this breaks the security model.

## Project Structure

```
.
├── cmd/envbox/          # Main entry point
├── cli/                 # CLI commands (docker.go is the main orchestrator - 1000+ lines)
├── dockerutil/          # Docker API client wrappers
├── xunix/               # Linux-specific utilities (GPU, mounts, devices)
├── background/          # Process management for sysbox components
├── sysboxutil/          # Sysbox manager interaction
├── buildlog/            # Build logging utilities
├── slogkubeterminate/   # Kubernetes termination signal handling
├── integration/         # Integration tests (require VM/physical machine)
│   └── integrationtest/ # Test helper package - maintain and improve this API
├── deploy/              # Dockerfile and deployment files
├── scripts/             # Build and utility scripts
└── xhttp/xio/           # HTTP and I/O utilities
```

### Key Files

- **`cli/docker.go`**: Main orchestration logic - starts sysbox, manages inner container lifecycle
- **`cmd/envbox/main.go`**: Entry point that calls `cli.Root()`
- **`integration/docker_test.go`**: Primary integration test suite
- **`Makefile`**: Build targets and sysbox version pinning
- **`deploy/Dockerfile`**: Multi-stage build for envbox image

## Development Workflow

### Prerequisites

- Go 1.24+
- Docker installed
- **VM or physical machine** for integration tests (Docker-in-Docker won't work for testing envbox)
- Linux kernel with seccomp API level >= 5

### Build Commands

```bash
# Build the envbox binary
make build/envbox

# Run unit tests
make test

# Run integration tests (REQUIRED before PR)
CODER_TEST_INTEGRATION=1 make test-integration

# Format code (gofumpt + markdownfmt)
make fmt

# Build Docker image
make build/image/envbox

# Clean build artifacts
make clean
```

### Pre-PR Checklist

✅ Run `make fmt` to format code  
✅ Run `make test` for unit tests  
✅ **Run `CODER_TEST_INTEGRATION=1 make test-integration`** (critical!)  
✅ Verify golangci-lint passes (CI will check)  
✅ Update documentation if adding features  
✅ If changing environment variables, update README.md table  
✅ **Push to feature branch, NOT main**

## Testing Strategy

### Integration Tests are Primary

Integration tests validate actual container behavior and are **the most important validation**. Unit tests are for input/output validation, but integration tests ensure correctness.

**Why integration tests matter:**
- They test the actual outer/inner container interaction
- They validate sysbox integration
- They catch subtle namespace, mount, and device issues
- They ensure GPU passthrough works correctly

### Running Integration Tests

```bash
# Set the environment variable to enable integration tests
export CODER_TEST_INTEGRATION=1

# Run all integration tests
make test-integration

# Run specific test
go test -v -count=1 ./integration/ -run TestDocker/Dockerd
```

### Writing Integration Tests

Use the `integration/integrationtest` package helpers:

```go
import (
    "github.com/coder/envbox/integration/integrationtest"
)

func TestMyFeature(t *testing.T) {
    t.Parallel()
    if val, ok := os.LookupEnv("CODER_TEST_INTEGRATION"); !ok || val != "1" {
        t.Skip("integration tests are skipped unless CODER_TEST_INTEGRATION=1")
    }

    pool, err := dockertest.NewPool("")
    require.NoError(t, err)

    tmpdir := integrationtest.TmpDir(t)
    binds := integrationtest.DefaultBinds(t, tmpdir)

    // Run envbox
    resource := integrationtest.RunEnvbox(t, pool, &integrationtest.CreateDockerCVMConfig{
        Image:       integrationtest.DockerdImage,
        Username:    "root",
        OuterMounts: binds,
    })

    // Wait for inner container's docker daemon
    integrationtest.WaitForCVMDocker(t, pool, resource, time.Minute)

    // Your test logic here
}
```

**Integration test helpers:**
- `TmpDir(t)` - Creates temporary directory (handles cleanup)
- `MkdirAll(t, paths...)` - Creates directories safely
- `WriteFile(t, path, contents)` - Writes test files
- `RunEnvbox(t, pool, config)` - Starts envbox container
- `WaitForCVMDocker(t, pool, resource, timeout)` - Waits for inner dockerd
- `DefaultBinds(t, tmpdir)` - Creates standard volume binds

### Unit Tests

Unit tests are for:
- Input validation and parsing (e.g., mount string parsing)
- Pure functions without side effects
- Mock-based testing of Docker API calls

**Patterns:**
- Use table-driven tests
- Mock external dependencies (see `dockerutil/dockerfake/`)
- Test edge cases and error conditions

## Common Development Tasks

### Adding a New Environment Variable

1. Define constant in `cli/docker.go`:
   ```go
   EnvMyNewFeature = "CODER_MY_NEW_FEATURE"
   ```

2. Add to `dockerCmd.Flags()` section:
   ```go
   cliflag.StringVarP(cmd.Flags(), &flags.myFeature, "my-feature", "", EnvMyNewFeature, "default", "Description")
   ```

3. Use in container creation logic (around line 730+ in `cli/docker.go`)

4. **Update README.md** environment variable table

5. Add integration test to verify behavior

### Adding Mount Support

1. Parse mount in `parseMounts()` function
2. Add mount to `xunix.Mount` slice
3. Pass to inner container via `Mounts` field
4. Test with integration test

### GPU/Device Passthrough

1. Detection logic goes in `xunix/gpu.go` or `xunix/device.go`
2. Use regex patterns to identify relevant mounts/libraries
3. Pass devices via `Resources.Devices` in container config
4. Mount libraries via `Mounts`
5. Test with actual GPU hardware (integration test may need special setup)

### Fixing Bugs

1. **Reproduce**: Write integration test that fails with the bug
2. **Fix**: Make minimal changes to fix the issue
3. **Verify**: Ensure integration test now passes
4. **Check**: Run full test suite (`make test && make test-integration`)
5. **Document**: Add comments explaining non-obvious fixes

### Improving Documentation

- Update `README.md` for user-facing changes
- Add inline comments for complex logic
- Update this `AGENTS.md` if development patterns change
- Keep examples current with actual code

## Key Packages Explained

### `cli/docker.go`

The main orchestration logic (1000+ lines). Key responsibilities:
- Starts sysbox-mgr and sysbox-fs background processes
- Starts dockerd in outer container
- Waits for sysbox manager to be ready
- Pulls inner container image
- Creates and starts inner container with proper configuration
- Forwards signals to inner container
- Handles bootstrap script execution

**Important functions:**
- `dockerCmd()` - Main command logic
- `dockerdArgs()` - Generates dockerd arguments
- `parseMounts()` - Parses CODER_MOUNTS environment variable
- Inner container creation (around line 730)

### `dockerutil/`

Docker API client wrappers and utilities:
- `client.go` - Docker client creation and management
- `container.go` - Container operations
- `image.go` - Image pulling and metadata (including OS detection for GPU passthrough)
- `daemon.go` - Dockerd process management
- `registry.go` - Registry authentication and image pull secrets
- `exec.go` - Container exec operations
- `network.go` - Network configuration

**Architecture-specific files:**
- `image_linux_amd64.go` - AMD64-specific usr lib detection
- `image_linux_arm64.go` - ARM64-specific usr lib detection

### `xunix/`

Linux-specific utilities for system interactions:
- `gpu.go` - GPU detection and mount identification (regex-based)
- `device.go` - Device handling (/dev/fuse, /dev/net/tun, etc.)
- `mount.go` - Mount point handling
- `sys.go` - System information (kernel version, etc.)
- `user.go` - User/group operations
- `exec.go` - Process execution
- `fs.go` - Filesystem abstractions (can be mocked with `xunixfake/`)
- `env.go` - Environment variable utilities
- `error.go` - "No space left on device" detection

**GPU detection patterns** (see `gpu.go`):
- Mounts matching: `(?i)(nvidia|vulkan|cuda)`
- Libraries matching: `(?i)(libgl(e|sx|\.)|nvidia|vulkan|cuda)`
- Shared objects: `\.so(\.[0-9\.]+)?$`

### `background/`

Process management for long-running background processes (sysbox-mgr, sysbox-fs, dockerd):
- `process.go` - Process abstraction with stdout/stderr capture
- Handles process lifecycle and monitoring
- Logs process output for debugging

### `integration/integrationtest/`

**Important**: Maintain and improve this API to make integration tests easier to write.

Current helpers:
- `TmpDir(t)` - Temporary directory creation
- `MkdirAll(t, paths...)` - Directory creation
- `WriteFile(t, path, contents)` - File writing
- `RunEnvbox(t, pool, config)` - Envbox container startup
- `WaitForCVMDocker(t, pool, resource, timeout)` - Dockerd readiness check
- `DefaultBinds(t, tmpdir)` - Standard volume binds
- `CreateCoderToken(t)` - Coder agent token creation
- Certificate handling utilities

**Future improvements** should make common testing patterns easier.

## Configuration via Environment Variables

All configuration uses `CODER_*` prefixed environment variables. See README.md for complete list.

### Critical Variables

- `CODER_INNER_IMAGE` - Inner container image (required)
- `CODER_INNER_USERNAME` - Inner container username (required)
- `CODER_AGENT_TOKEN` - Coder agent token (required for Coder integration)
- `CODER_AGENT_URL` - Coder deployment URL
- `CODER_BOOTSTRAP_SCRIPT` - Script to run in inner container (typically starts agent)

### Common Optional Variables

- `CODER_MOUNTS` - Mount paths (format: `src:dst[:ro],src:dst[:ro]`)
- `CODER_ADD_GPU` - Enable GPU passthrough (`true`/`false`)
- `CODER_ADD_TUN` - Add TUN device (`true`/`false`)
- `CODER_ADD_FUSE` - Add FUSE device (`true`/`false`)
- `CODER_CPUS` - CPU limit for inner container
- `CODER_MEMORY` - Memory limit for inner container (bytes)
- `CODER_INNER_ENVS` - Environment variables to pass to inner container (supports wildcards)

## Dependencies and Versions

### Go Dependencies

- **Go 1.24+** required (see `go.mod`)
- **Docker API client** pinned to specific version (avoid breaking changes)
- **coder/coder v2.14.4** - Main Coder integration
- **coder/tailscale fork** - Not important to agents; version should match coder/coder's go.mod
- **sysbox 0.6.7** - Exact version with SHA pinned in Makefile

### Docker and System Dependencies

- **Docker CE 27.3.1** - Pinned in Dockerfile
- **sysbox 0.6.7** - Downloaded and verified via SHA256 in Dockerfile
- **Linux kernel** - Requires seccomp API level >= 5
- **Ubuntu 22.04 (Jammy)** - Base image

### Version Management

**Updating sysbox:**
1. Update `SYSBOX_VERSION` in Makefile
2. Update `SYSBOX_SHA` in Makefile (get from nestybox releases)
3. Update both `ARG` values in deploy/Dockerfile
4. Test thoroughly with integration tests

**Updating coder/coder dependency:**
1. Check coder/coder's go.mod for tailscale version
2. Update both dependencies in go.mod
3. Run `go mod tidy`
4. Verify integration tests pass

## Coder Integration

### How envbox integrates with Coder

1. **Template**: Coder templates define envbox containers as Kubernetes pods
2. **Agent**: Bootstrap script installs and starts Coder agent in inner container
3. **Token**: `CODER_AGENT_TOKEN` authenticates agent with Coder deployment
4. **Workspace**: Inner container becomes user's workspace environment

### Coder-Specific Considerations

- Environment variables follow Coder naming conventions
- Agent must start successfully for workspace to be usable
- GPU passthrough important for ML/AI workspaces
- Mount handling critical for persistent home directories
- Network configuration affects agent connectivity

### Example Template Usage

See [coder/coder repo examples](https://github.com/coder/coder/tree/main/examples/templates/envbox) for reference templates.

## Common Pitfalls and Gotchas

### ❌ Don't Do This

1. **Push directly to main** - Always use feature branches and PRs
2. **Make inner container privileged** - Breaks entire security model
3. **Skip integration tests** - They catch real-world issues unit tests miss
4. **Test envbox inside envbox** - Won't work; requires VM/physical machine
5. **Change sysbox version without updating SHA** - Build will fail verification
6. **Ignore "no space left on device" errors** - These have special handling (see `noSpaceDataDir`)
7. **Modify user namespace offset (100000)** - Will break existing container mappings
8. **Remove signal forwarding** - Inner container won't receive termination signals

### ✅ Do This

1. **Always use feature branches** - Create branch, push to it, then PR
2. **Run integration tests on every change** - They're the source of truth
3. **Use integrationtest helpers** - Consistent, reliable test setup
4. **Preserve backward compatibility** - Existing workspaces depend on envbox behavior
5. **Test GPU passthrough on real hardware** - Mock tests won't catch driver issues
6. **Log important events** - Helps debugging in production
7. **Handle errors gracefully** - Users should understand what went wrong
8. **Update documentation** - Keep README.md and this file current

### Known Issues to Be Aware Of

1. **Kernel compatibility**: sysbox requires seccomp API level >= 5
2. **Storage drivers**: Overlay2 doesn't work on top of overlay (use vfs fallback)
3. **Disk space**: Special handling when user PVC is full (`noSpaceDataDir`)
4. **GPU libraries**: Must mount symlinked shared objects correctly
5. **AWS EKS**: Special handling for web identity tokens
6. **Idmapped mounts**: May need disabling on some systems (`CODER_DISABLE_IDMAPPED_MOUNT`)

## What to Focus On

### High-Priority Tasks for Agents

1. **Bug fixes** - Especially:
   - Container lifecycle issues
   - Mount and volume problems
   - GPU passthrough failures
   - Signal handling bugs
   - Network configuration issues

2. **Integration tests** - Add tests for:
   - New features
   - Bug reproductions
   - Edge cases
   - Different inner images

3. **Documentation** - Improve:
   - README.md clarity
   - Code comments in complex sections
   - Integration test examples
   - This AGENTS.md file

4. **Small features** - Incremental improvements:
   - New environment variables
   - Additional mount options
   - Device passthrough enhancements
   - Better error messages

### Lower-Priority Tasks

- Large architectural changes (discuss with maintainers first)
- Performance optimizations (profile first)
- Refactoring (ensure integration tests cover affected code)

## Debugging Tips

### Viewing Logs

```bash
# Outer container logs
kubectl logs <pod-name>

# Inner container logs (from outer container)
docker logs workspace_cvm

# Sysbox manager status
cat /run/sysbox/sysmgr.sock
```

### Common Debug Points

1. **Sysbox not starting**: Check kernel version and seccomp support
2. **Inner container fails to start**: Check image pull secrets and registry auth
3. **GPU not detected**: Verify mounts, check `xunix/gpu.go` regex patterns
4. **Bootstrap script fails**: Examine script execution logs
5. **Out of space**: Check if vfs fallback is being used

### Integration Test Debugging

```bash
# Keep test containers around on failure
# Modify test to not cleanup on failure
if !t.Failed() {
    os.RemoveAll(tmpdir)
}

# Inspect running test container
docker ps | grep envbox
docker exec -it <container-id> bash

# Check inner container
docker exec -it <outer-container-id> docker ps
docker exec -it <outer-container-id> docker logs workspace_cvm
```

## Code Quality Standards

### Linting

- **golangci-lint v1.64.8** runs in CI
- **shellcheck** for shell scripts
- **gofumpt** for Go formatting (stricter than gofmt)
- **markdownfmt** for Markdown files

Run locally:
```bash
make fmt  # Format code
# golangci-lint runs automatically in CI
```

### Code Style

- Follow standard Go conventions
- Use descriptive variable names
- Add comments for non-obvious logic
- Keep functions focused and reasonably sized
- Use error wrapping with `xerrors.Errorf`
- Structured logging with `slog`

### Error Handling

```go
// ✅ Good
if err != nil {
    return xerrors.Errorf("pull inner image: %w", err)
}

// ❌ Bad
if err != nil {
    return err  // Lost context
}
```

### Logging

```go
// Use structured logging
log.Info(ctx, "starting inner container",
    slog.F("image", innerImage),
    slog.F("username", username))

// Don't use fmt.Println
```

## CI/CD Pipeline

### GitHub Actions Workflows

- **ci.yaml** - Main CI pipeline:
  - Linting (golangci-lint, shellcheck)
  - Formatting checks
  - Unit tests
  - Integration tests
  - Security scanning

- **release.yaml** - Release automation
- **latest.yaml** - Latest tag updates

### Integration Tests in CI

Integration tests run on `ubuntu-latest-8-cores` runners with proper permissions. They're the gate for merging PRs.

## Getting Help

### Resources

- **README.md** - User documentation and configuration reference
- **This AGENTS.md** - Developer/agent guidance
- **Integration tests** - Examples of correct usage patterns
- **Sysbox docs** - https://github.com/nestybox/sysbox/tree/master/docs
- **Coder docs** - https://coder.com/docs
- **Coder template example** - https://github.com/coder/coder/tree/main/examples/templates/envbox

### When Making Changes

1. Read relevant code sections first
2. Check existing tests for patterns
3. Start with small, focused changes
4. Write integration test to verify behavior
5. Run full test suite before submitting
6. Update documentation as needed
7. **Push to feature branch and create PR**

---

**Remember**: Never push to main. The security of the inner container is paramount. Integration tests are mandatory. Make incremental changes. Help maintain the `integrationtest` package API.
