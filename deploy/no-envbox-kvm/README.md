# No-envbox workspaces: mounted devices + KVM (exploration)

> Status: **exploratory proof-of-concept**. Nothing here is wired into the
> envbox build or tested end to end. It exists to sketch what a workspace
> deployment that drops the envbox/sysbox wrapper in favor of a KVM-backed
> microVM (using only device requests, not a privileged outer pod) would
> look like, and to surface the gaps that still need filling.

## Why

Today envbox is a **privileged** outer pod that:

- bundles and supervises the sysbox runtime (`sysbox-mgr`, `sysbox-fs`,
  `sysbox-runc`),
- `mknod`s `/dev/net/tun` and `/dev/fuse` for the inner container from inside
  the privileged container (see `xunix/device.go`),
- bind-mounts host `/lib/modules` and `/usr/src` to satisfy sysbox's kernel
  requirements,
- runs an inner `dockerd` under `sysbox-runc` as the user's workspace.

That model has four structural problems (see the gap analysis that motivated
this branch):

1. **Cold image cache.** The inner dockerd has its own image store, isolated
   from the node's containerd cache, so every inner pull is cold.
2. **Requires `privileged`.** Blocked by Pod Security Admission, Gatekeeper,
   and most hardened clusters.
3. **Coupled to sysbox** (now community-best-effort) and a host kernel matrix
   (`/lib/modules`, `/usr/src`).
4. **Not node-OS portable.** Cannot run on immutable/SELinux-locked node OSes
   like Bottlerocket that forbid the host mounts and module loading it needs.

## The idea

Replace the "shared-kernel + sysbox" isolation model with a **microVM booted
inside an ordinary pod**. Instead of the pod being privileged and manufacturing
its own devices, the pod:

- **requests `/dev/kvm`** (and `/dev/net/tun`) as scheduled resources from a
  device plugin, rather than `mknod`ing them under `privileged`, and
- boots a lightweight VMM (Cloud Hypervisor / Firecracker) that runs a real
  guest kernel; the user's `dockerd`/`systemd` runs **inside the guest**, where
  overlayfs, DinD, and non-root images "just work" against a normal kernel.

This is not a Kubernetes RuntimeClass (customers can't install those). The VMM
runs as a normal workload process inside the pod, so no cluster-runtime install
is required. The only node-level requirement is that `/dev/kvm` exists and is
exposed to the pod.

```
 Before (envbox)                      After (no-envbox + KVM)
 ┌───────────────────────────┐        ┌───────────────────────────┐
 │ privileged outer pod      │        │ unprivileged pod          │
 │  sysbox daemons           │        │  requests devices.../kvm  │
 │  ┌──────────────────────┐ │        │  ┌──────────────────────┐ │
 │  │ inner dockerd (sysbox│ │        │  │ VMM (cloud-hypervisor│ │
 │  │ -runc), shared kernel│ │        │  │  / firecracker)      │ │
 │  │  user workspace      │ │        │  │  ┌────────────────┐  │ │
 │  └──────────────────────┘ │        │  │  │ guest kernel   │  │ │
 └───────────────────────────┘        │  │  │  dockerd/systemd│ │ │
    needs: privileged,                │  │  │  user workspace │ │ │
    /lib/modules, /usr/src,           │  │  └────────────────┘  │ │
    sysbox, host kernel matrix        │  └──────────────────────┘ │
                                      └───────────────────────────┘
                                         needs: /dev/kvm exposed
```

## Node requirements

`/dev/kvm` must exist on the node:

- **AWS:** `*.metal` instances, or `C8i` / `M8i` / `R8i` with EC2 nested
  virtualization enabled (GA 2026-02-16). Older virtualized families do not
  expose VT-x/AMD-V and cannot host KVM.
- **GCP / Azure:** nested virtualization on supported machine types.
- **Bare metal:** native.

## What's in this directory

| File | Purpose |
| --- | --- |
| `manifests/kvm-device-plugin.yaml` | DaemonSet exposing `/dev/kvm` (+ `/dev/net/tun`) as schedulable resources, pinned to KVM-capable nodes. Not privileged for workloads; the plugin itself needs host device access. |
| `manifests/workspace-pod.yaml` | A no-envbox workspace pod: unprivileged, requests the device resources, boots a microVM, mounts a persistent `/var/lib/docker` cache disk. |
| `manifests/coder-template.tf` | Terraform `kubernetes_pod` sketch for the equivalent Coder template. |
| `NOTES.md` | Open questions, the make-or-break tests, and mapping to the existing envbox code that would need to change. |

## Explicitly out of scope for this sketch

- The guest rootfs image (kernel + init + agent + dockerd) build.
- The in-pod supervisor that launches the VMM, forwards the Coder agent token,
  wires vsock/virtio-net, and streams the build log (the analog of today's
  `cli/docker.go` flow).
- GPU passthrough into the guest.

These are the real engineering; this branch only sketches the deployment
surface so we can evaluate feasibility and node/OS constraints.
