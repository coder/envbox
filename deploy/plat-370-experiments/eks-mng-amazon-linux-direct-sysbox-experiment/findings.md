# Direct Sysbox on EKS managed AL2023 nodes

Date: 2026-08-09

Status: **functional pass; production adoption remains conditional**

## Outcome

Direct, node-installed Sysbox successfully provided non-privileged Coder-shaped
Docker-in-Docker workspaces on an EKS 1.35 managed node group running the
AWS-managed Amazon Linux 2023 AMI.

The workspace Pods passed Kubernetes Pod Security Standards `restricted`
enforcement with:

- `hostUsers: false`;
- `privileged: false`;
- `allowPrivilegeEscalation: false`;
- all Kubernetes-requested capabilities dropped; and
- `seccompProfile.type: RuntimeDefault`.

Rootful Docker, BuildKit, Compose, nested networking, resource limits,
persistent Docker state, concurrent isolated workspaces, cross-node access,
and automatic Sysbox installation on a newly scaled node all passed.

This is not an upstream, off-the-shelf AL2023 result. It required a small
experimental fork of the Sysbox Kubernetes installer. The result establishes
technical viability, not a production support commitment.

## Tested stack

| Component | Tested value |
| --- | --- |
| EKS | 1.35.6 (`v1.35.6-eks-254016e`) |
| Node group | EKS managed node group, two `m6i.large` nodes |
| Node OS | Amazon Linux 2023.12.20260727, AWS-managed AMI |
| Kernel | 6.12.94-123.192.amzn2023.x86_64 |
| Host runtime | EKS containerd 2.2.5 |
| Sysbox | CE 0.7.1, `sysbox-runc` commit `081856cc5d17e7095f066b08d0eca6bb0b515c47` |
| Installer image | `envbox-eks-cache-experiment@sha256:fe6b53286b836b932692907921656c154922d62366287429dd5534c5347575f3` |
| Workspace image | `registry.nestybox.com/nestybox/ubuntu-focal-systemd-docker@sha256:e451ce586ea5c0224c511d4c87b9aa215173db0d1eac6473d0e4b3b5abe41ba5` |
| Workspace Docker | 20.10.17, overlay2, cgroup v2 |
| Persistent storage | EBS gp3 PVC mounted at `/var/lib/docker` |

## Installer changes required for AL2023

The fork changed the Sysbox Kubernetes installer to:

1. recognize AL2023 as a supported distribution;
2. select the generic static Sysbox binaries;
3. install dependencies with `dnf`;
4. skip Shiftfs and require a sufficiently new idmapped-mount kernel;
5. omit the Debian-specific `kernel.unprivileged_userns_clone` sysctl when it
   does not exist; and
6. configure the containerd 2.x CRI plugin table
   `io.containerd.cri.v1.runtime`, removing any stale legacy handler.

Two failures made the last two changes necessary:

- POC image 1 stopped when the AL2023 kernel lacked
  `/proc/sys/kernel/unprivileged_userns_clone`.
- POC image 2 installed Sysbox but wrote its handler only under containerd's
  legacy `io.containerd.grpc.v1.cri` table. Kubelet consequently reported the
  `sysbox-runc` handler as unknown.

POC image 3 repaired the active containerd 2.x configuration. The host kept
the EKS-provided containerd; CRI-O was not installed or activated. The
`sysbox`, `sysbox-mgr`, and `sysbox-fs` services were active, and ordinary EKS
system Pods remained healthy after the containerd restart.

## Functional results

| Test | Result |
| --- | --- |
| Sysbox workspace with systemd and rootful Docker | Pass |
| Unprivileged outer workspace Pod and unprivileged nested containers | Pass |
| Docker pull and run | Pass |
| BuildKit image build and execution | Pass |
| Nested CPU, memory, and PID limits | Pass |
| Docker bridge networking and service-name DNS | Pass |
| Compose DNS/HTTP, outbound access, and published port | Pass |
| Kubernetes ClusterIP Service to a nested published port from another node | Pass |
| Direct cross-node Pod-IP access to a nested published port | Pass |
| EBS-backed Docker state across Pod replacement | Pass; replacement ready in 13 seconds |
| Two simultaneous workspaces on one node | Pass |
| Distinct user namespaces and non-overlapping UID maps | Pass |
| Separate-PVC Docker-state isolation | Pass, including after Pod recreation |
| New MNG node automatically prepared by the installer DaemonSet | Pass |
| Fresh Sysbox workspace and nested Docker on the new node | Pass |
| Namespace-wide PSS `restricted` enforcement | Pass |

The initial nested-network retry was needed because the selected Alpine image
does not contain `httpd`, not because networking failed. The initial Compose
published-port check similarly used an absent host `wget`; an Alpine
host-network child verified the port successfully.

## Security interpretation

The hardened workspace manifests are accepted under actual Kubernetes
`restricted` enforcement and show `NoNewPrivs: 1` plus active runtime-default
seccomp filtering. They request no capabilities and are not privileged.

Inside the workspace, however, Sysbox deliberately supplies the full system-
container capability set **inside the Pod's private user namespace**. The
workspace also receives a private writable cgroup hierarchy and was observed
under SELinux type `unconfined_service_t`. These privileges are namespaced and
do not equal host-root capabilities, but they remain part of the Sysbox trust
model and require a focused security review before production use.

This security shape is materially different from the native-userns DinD
prototype:

- direct Sysbox satisfies Kubernetes `restricted` Pod admission and mediates
  system-container interfaces through trusted node services;
- native userns DinD directly requested namespaced `SYS_ADMIN` and `NET_ADMIN`,
  unmasked `/proc`, unconfined seccomp, writable cgroups, and privilege
  escalation in its Pod specification; and
- direct Sysbox instead requires a privileged node installer, host runtime
  configuration, and trusted Sysbox services on every selected node.

Neither experiment alone proves one boundary categorically safer. They move
the trust and attack surface to different layers.

## Customer fit

This approach is a credible option when a customer:

- permits a dedicated, mutable EKS managed node group;
- permits a reviewed privileged installer or a pre-baked approved AMI;
- rejects privileged workspace Pods;
- can use Kubernetes/containerd versions that support Pod user namespaces;
  and
- accepts carrying the AL2023 installer patch until equivalent upstream
  support exists.

It does not cover EKS Auto Mode or Bottlerocket because those managed,
immutable nodes cannot be modified this way. It also does not replace Envbox
for older EKS versions without usable Pod user namespaces: Envbox creates its
own user namespace and private runtime inside a privileged outer Pod.

## Remaining production validation

The following were not established by this POC:

- Coder agent startup and a complete Coder workspace lifecycle;
- representative devcontainer and Testcontainers workloads;
- node reboot, replacement, AL2023 AMI upgrade, and MNG rolling update;
- installer upgrade, rollback, uninstall, and partial-failure recovery;
- high-density scheduling, exhaustion, and noisy-neighbor behavior;
- image-cache and startup performance relative to Envbox and native userns;
- a security review of Sysbox's namespaced capabilities, writable cgroups,
  SELinux posture, host mounts, installer privilege, and daemon attack surface;
- an immutable custom-AMI deployment that avoids the live privileged installer;
  and
- upstream maintenance and support ownership for the AL2023 patch.

## Decision

The experiment changes direct Sysbox from a speculative Ubuntu-only option to
a **validated AL2023 managed-node-group POC**. It expands the solution matrix
for customers that prohibit privileged workspace Pods but permit trusted node
runtime modification.

Recommend retaining it as a production-candidate architecture, subject to the
remaining lifecycle, Coder-integration, and security work. Do not recommend it
for Auto Mode/Bottlerocket or as an upstream-supported AL2023 deployment yet.

## Evidence

- [Runbook](runbook.md)
- [Final installer log](installer-poc3.log)
- [Host runtime evidence](host-runtime-evidence-continuation.txt)
- [Docker child smoke test](docker-child-smoke.txt)
- [BuildKit test](docker-buildkit.txt)
- [Limits and networking](docker-limits-and-network.txt)
- [Compose test](docker-compose-smoke.txt)
- [Compose published-port test](docker-compose-published-port.txt)
- [Persistence after replacement](persistence-after-pod-replacement.txt)
- [Restricted-profile smoke test](restricted-profile-smoke.txt)
- [Restricted-profile BuildKit test](restricted-profile-buildkit.txt)
- [Restricted-profile Compose test](restricted-profile-compose.txt)
- [Restricted persistent workspace](restricted-persistent-workspace.txt)
- [New-node smoke test](new-node-sysbox-smoke.txt)
- [Two-node installer state](nodes-two-node-sysbox.txt)
