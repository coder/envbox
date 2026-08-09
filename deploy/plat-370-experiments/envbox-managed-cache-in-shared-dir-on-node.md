# Envbox-managed node-local image cache

This note evaluates a **best-effort, node-local cache owned by
Envbox**. It is shared only by trusted Envbox outer Pods scheduled
onto the same node; it deliberately does not mount or modify the node
container runtime's own data store. This is a proposal: current Envbox
uses a private outer Docker daemon that pulls the inner image normally
and does not implement this shared cache.

## 1. Envbox-managed cache: feasibility and phased design

Yes, with an explicit Kubernetes-provided shared host directory.
Privilege alone is not enough: an Envbox container's normal filesystem
and mount namespace disappear with its Pod and are not visible to
another Pod. The workspace template must give every Envbox outer Pod a
mount of the same dedicated `hostPath`, for example:

```text
node: /var/lib/envbox-image-cache
  ├── Envbox Pod A: /cache
  └── Envbox Pod B: /cache
```

The outer Envbox process can then maintain a node-local OCI cache
there. A possible flow is:

1. Normalize the requested inner-image reference to an immutable manifest digest.
2. Look for a verified OCI image layout / archive at `/cache/<digest>`.
3. On a hit, import it into Envbox's private outer Docker daemon and create `workspace_cvm`.
4. On a miss, pull from the registry, atomically publish the OCI
   representation into the cache, then create the workspace.

This avoids repeated registry / WAN downloads for new Envbox Pods on
the same node. It does **not** make Docker share image snapshots
directly: every private outer Docker daemon still has to read,
decompress, unpack, and record the image in its own data root. It is
consequently a download cache, not full layer/snapshot reuse. This is
the recommended first phase because it preserves Envbox's current
`dockerd`-driven image and container lifecycle.

### What privilege contributes to the cache design

For the shared OCI cache itself, privilege is not required. A process
with a writable `/cache` volume can create directories, write blobs,
take file locks, verify digests, and import an OCI archive into its
own daemon without Linux administrative capabilities. Kubernetes, not
the container, should create and mount the dedicated host directory.

The only meaningful possible cache-specific benefit of Envbox's
privileged outer Pod is avoiding access-policy friction, especially on
Bottlerocket / EKS Auto Mode. Non-privileged Pods normally receive
different SELinux MCS labels, which can prevent two Pods from reading
and writing the same host-backed path. Privileged Pods are less likely
to encounter that particular SELinux isolation boundary. This is an
implementation/environment detail to confirm with a smoke test, not a
reason to use privilege for cache mechanics.

Envbox still needs its privileged outer container for its separate
Sysbox and nested-Docker responsibilities. The cache proposal neither
requires additional privilege nor justifies accessing host
containerd/Docker storage or sockets.

The cache needs digest-keyed immutable entries, integrity
verification, per-digest locking, atomic publication, eviction and
disk-pressure handling. It must also be treated as shared data between
trusted Envbox outer Pods: image layers can disclose source or
accidentally baked-in secrets. Never mount the host's
`/var/lib/containerd`, `/var/lib/docker`, or a container-runtime
socket. Those stores are daemon-private and exposing them becomes a
node-compromise interface.

### Future optimization: cache immutable unpacked image artifacts

On a phase-one cache hit, each Envbox private outer Docker daemon
imports the OCI image and still decompresses, extracts, and records
its own `overlay2` image layers before it creates the Sysbox workspace
container.

As a later optimization, Envbox could cache the **immutable, shareable
artifacts produced from the OCI image** as well: extracted read-only
layer directories or a prepared read-only root filesystem. Every
workspace would still have its own writable layer, but it could reuse
the node's immutable lower layers instead of repeating decompression
and extraction.

```text
shared node cache: immutable extracted image layers / rootfs
     ├── workspace A: private OverlayFS upperdir + workdir → merged rootfs
     └── workspace B: private OverlayFS upperdir + workdir → merged rootfs
                                                    │
                                                    ▼
                                               sysbox-runc
```

This can reduce first-start CPU and disk I/O beyond an OCI-blob cache,
and permits page-cache reuse for image files. It must **not** be
implemented by mounting another Docker daemon's
`/var/lib/docker/overlay2` tree: Docker's storage metadata and mounts
are daemon-private, and independent dockerd instances have no
supported external shared-snapshot-store interface.

The standard building blocks are OverlayFS (shared read-only lower
layers plus a private copy-on-write upper layer) and an OCI runtime
bundle with a prepared root filesystem. However, keeping this entirely
Envbox-managed means Envbox would have to replace the portion of the
current private `dockerd` workflow that prepares and launches the
image. It would need to manage image extraction and verification,
per-image locking and reference counts, lower-layer garbage
collection, per-workspace OverlayFS mounts, OCI bundle/config
generation, and the lifecycle, networking, mounts, logs, and cleanup
currently delegated to Docker. It is therefore a valid second-phase
optimization, but a much larger redesign than the OCI-blob cache.

### Architectural boundary

The cleaner long-term alternative, but one outside the
Envbox-managed-cache constraint, is a direct workspace Pod image using
node-installed Sysbox and a Kubernetes `RuntimeClass`. In that design
the node's containerd runtime and snapshotter own both the image cache
and unpacked snapshots. The first phase proposed here does not require
that node-runtime integration.

## 2. EKS Auto Mode with Bottlerocket

Mostly yes, through Kubernetes configuration rather than direct node
administration.

An EKS Auto Mode administrator cannot customize the node AMI or SSH
into the managed nodes, but can create the Envbox Pod specification
and grant a narrowly scoped exception for a privileged Pod with a
dedicated writable `hostPath`. The volume should use `type:
DirectoryOrCreate`: kubelet creates the directory before the first
Envbox container starts, avoiding a first-workspace race. `type:
Directory` would instead require the directory to be pre-created and
would fail Pod startup if it is absent. All Envbox Pods scheduled onto
that node can mount the resulting directory.

### Baseline Envbox support is a prerequisite

The preceding conclusion is only about the proposed cache volume. It
does **not** establish that current Envbox is supported on EKS Auto
Mode Bottlerocket. Current Envbox requires a privileged outer Pod and
host mounts of `/lib/modules` and `/usr/src` for Sysbox; the checked
dogfood deployment deliberately schedules Envbox workspaces onto a
dedicated AL2023 managed node group, while other workloads use Auto
Mode nodes. There is no Envbox integration test or deployment
documentation in this source tree demonstrating the exact Auto Mode
Bottlerocket combination.

Envbox bundles and starts Sysbox inside its outer Pod, so this is not
identical to installing Sysbox directly on the Bottlerocket
node. However, the dogfood infrastructure identifies a concrete
baseline blocker for stock Auto Mode: Envbox / Sysbox-runc needs the
node-global `user.max_*_namespaces` limits raised at boot. Auto Mode's
locked-down NodeClass does not expose boot user data or kubelet
configuration for this. These namespace limits are not namespaced
sysctls, so Kubernetes cannot set them per workspace Pod; the dogfood
comments describe node-wide configuration as the only supported
setting mechanism. `fs.inotify.*` can be set per Pod in principle, but
the namespace limits cannot.

The earlier dogfood approach used a privileged DaemonSet to write the
sysctls at runtime. It can make the settings visible to later Pods,
but that particular manifest races with a workspace scheduled onto a
newly created node before the DaemonSet completes. That race is why
the current deployment uses the AL2023 managed node group with
boot-time configuration.

Sysbox's full `sysbox-deploy-k8s` installer uses a stronger,
race-avoidance protocol on regular Kubernetes nodes: it immediately
applies a `sysbox-runtime=not-running:NoSchedule` taint, installs and
validates the runtime, sets the node label `sysbox-runtime=running`,
and then removes the taint. Its `RuntimeClass` selects only nodes with
that `running` label, so Sysbox workloads stay pending during
installation. This demonstrates that a DaemonSet-based direct-Sysbox
deployment can handle the general Kubernetes installation race.

That does not establish compatibility with stock Auto Mode
Bottlerocket. The direct installer needs broad privileged host mounts
and modifies host binaries, systemd, sysctls, and container-runtime
configuration; Auto Mode may prevent one or more of these
operations. Auto-scaling also needs a bootstrap strategy that gets the
installer onto each new node before a `sysbox-runtime=running`
workload is considered schedulable. Therefore, on stock Auto Mode,
direct Sysbox and current Envbox remain **unverified rather than
reliably supported**. Successful admission, SELinux, host-mount,
installer, and workload tests would be required. Sysbox maintainers
have also described Bottlerocket support as untested and without a
concrete support plan; Bottlerocket's immutable filesystem has blocked
the conventional node-level Sysbox installer.

The baseline test should deploy the current Envbox image using the
intended privileged security context and existing `/lib/modules`,
`/usr/src`, `/var/lib/sysbox`, Docker-state, and home-volume
mounts. It should verify `sysbox-mgr` and `sysbox-fs` start, the inner
`workspace_cvm` starts, the Coder agent connects, and an inner `docker
run` succeeds. Only then should the shared-cache mount and its
SELinux/ownership behavior be tested.

### Practical authorization model

Even in this strictest managed-node case, the design is potentially
viable. It is requested in the workspace Pod specification (for
example, a dedicated `/var/lib/envbox-image-cache` `hostPath` mounted
as `/cache`), rather than by SSHing to a node or customizing an
AMI. The Auto Mode deployment/cluster administrator must authorize the
privileged Pod and that exact host-path exception through RBAC and any
Pod Security Admission or organization policy. It is not an
unconditional capability available to arbitrary workspace users.

Auto Mode's immutable Bottlerocket OS does not by itself make the
Kubernetes `hostPath` API unavailable. Its writable persistent storage
backs `/var`, so kubelet can create a dedicated path using
`DirectoryOrCreate`. The cache remains node-local, best-effort state
and disappears when Auto Mode replaces the node. This should be
confirmed on the exact Auto Mode Bottlerocket variant with a small
privileged-Pod smoke test.

There are material constraints:

- Admission controls may prohibit `privileged` and `hostPath`. Envbox
  already requires a privileged exception, but the administrator must
  explicitly allow the additional, tightly restricted path.
- Auto Mode Bottlerocket uses SELinux. It normally applies distinct
  MCS labels to non-privileged Pods, which can block sharing
  host-backed storage across Pods. The exact Envbox Pod security
  context and cache sharing behavior need an on-cluster test; AWS
  documents matching SELinux options for Pods that deliberately share
  volumes.
- The cache is local to a particular node and disappears when Auto
  Mode replaces or terminates the node. It is appropriate only as
  best-effort cache state.
- The cache consumes node ephemeral disk outside normal Pod
  ephemeral-storage accounting, so it needs a bounded size and garbage
  collection.

In short: an EKS Auto Mode deployment can use a dedicated node-local
OCI download cache if its platform policy allows a privileged Envbox
Pod plus a narrow writable `hostPath`. It must not attempt to
bind-mount or manipulate the existing node container-runtime cache.

## 3. Operational alternatives: pull-through registry proxies

Rather than teaching Envbox to maintain and import a shared OCI-layout
cache, operators can route its existing `docker pull` requests through
a standard OCI registry pull-through proxy. This keeps OCI blob,
manifest, lock, partial-download, and garbage-collection behavior in
the registry implementation rather than Envbox code.

### Centralized proxy Deployment

Run a registry proxy as a normal Deployment, usually with durable
registry storage. Envbox's private dockerd instances use that proxy as
their registry mirror.

- **Operational burden:** lowest; it is an ordinary platform service
  with normal registry authentication, metrics, upgrades, and storage.
- **Cache scope:** cluster- or namespace-wide, rather than node-local.
- **Benefit:** avoids repeated upstream-registry downloads; cache hits
  still traverse the cluster network.
- **Trade-off:** the proxy may become a shared bottleneck and does not
  exploit a node's local disk/runtime cache.

### Node-local proxy DaemonSet

Run one pull-through registry mirror Pod on every eligible workspace
node, with local cache storage. Each Envbox Pod reaches the mirror on
its own node, normally using an explicit node-local endpoint such as a
host port or another locality-aware routing method. A normal
load-balanced Kubernetes Service is not sufficient by itself because
it may select a mirror on another node.

- **Operational burden:** moderate. It needs per-node cache
  capacity/eviction, local reachability, readiness/fallback handling,
  and monitoring; no new Envbox cache implementation.
- **Cache scope:** one cache per node. The first pull is remote; later
  workspaces on that node pull from local/LAN storage.
- **EKS Auto Mode / Bottlerocket:** viable. AWS recommends DaemonSets
  when Auto Mode users need host-level software because AMI
  customization is unavailable. A registry mirror itself can be an
  ordinary non-privileged Pod; Auto Mode supports `hostNetwork` and
  `hostPort` if used for node-local reachability.
- **Durability:** `emptyDir` preserves cache only while the DaemonSet
  Pod survives. A dedicated local `hostPath` can survive mirror-Pod
  recreation, but then the host-path and SELinux policy concerns
  belong to this single platform-owned DaemonSet. Node replacement
  still discards cache state.
- **Startup race:** when Auto Mode adds a fresh node for a workspace,
  the workspace can race the mirror's readiness. Envbox needs
  retry/wait behavior or an upstream-registry fallback.

### Implementation burden versus user operations

The proxy alternatives and the Envbox-managed cache have different
kinds of cost:

- **Envbox-managed node cache:** highest *Envbox development and
  maintenance* burden. Envbox would need bespoke OCI-cache, locking,
  import, eviction, integrity, and multi-tenant isolation
  behavior. Once that is implemented well, however, it can be the
  easiest *day-two user experience*: users enable one Envbox/template
  option (and the platform grants one narrow `hostPath` policy
  exception), while Envbox transparently populates and uses the
  cache. There is no separate registry service, endpoint, credential,
  node-local routing, or cache component for them to configure and
  observe.
- **Centralized proxy Deployment:** lower implementation burden
  because it relies on a standard registry, but the operator must
  deploy, secure, upgrade, size, monitor, and configure every Envbox
  daemon to use that service.
- **Node-local proxy DaemonSet:** also relies on a standard registry,
  but has more platform configuration than a Deployment: local
  endpoint/routing, cache storage per node, DaemonSet readiness, and
  fallback handling.

Therefore, if the question is "what is easiest for a Coder / Envbox
user after the feature exists?", the Envbox-managed cache may well be
the simplest. If the question is "what can be delivered and maintained
with the least new Envbox-specific engineering?", a standard
pull-through registry proxy is simpler. A node-local mirror DaemonSet
is the standard choice when repeated same-node pull latency matters
and that added platform configuration is acceptable.

## Sources

- Kubernetes: [Volumes and `hostPath`](https://kubernetes.io/docs/concepts/storage/volumes/)
- Kubernetes: [Privileged containers](https://kubernetes.io/docs/concepts/security/linux-kernel-security-constraints/)
- AWS: [EKS Auto Mode data-plane security](https://docs.aws.amazon.com/whitepapers/latest/security-overview-amazon-eks-auto-mode/eks-auto-mode-data-plane.html)
- AWS: [EKS Auto Mode volume sharing / SELinux](https://docs.aws.amazon.com/eks/latest/userguide/auto-troubleshoot.html)
- AWS: [EKS Auto Mode and DaemonSets](https://docs.aws.amazon.com/eks/latest/best-practices/automode.html)
- AWS: [EKS Auto Mode networking support](https://docs.aws.amazon.com/eks/latest/userguide/auto-networking.html)
- Bottlerocket: [Security guidance and filesystem layout](https://github.com/bottlerocket-os/bottlerocket/blob/develop/SECURITY_GUIDANCE.md)
- Sysbox: [Bottlerocket support discussion](https://github.com/nestybox/sysbox/discussions/487)
- Sysbox: [installer DaemonSet and RuntimeClass manifest](/home/geo/sysbox/sysbox-k8s-manifests/sysbox-install.yaml)
- Docker: [storage drivers and shared read-only image layers](https://docs.docker.com/engine/storage/drivers/)
- Docker: [`overlay2` image and container-layer layout](https://docs.docker.com/engine/storage/drivers/overlayfs-driver/)
- OCI: [runtime bundles and root filesystem configuration](https://github.com/opencontainers/runtime-spec)
