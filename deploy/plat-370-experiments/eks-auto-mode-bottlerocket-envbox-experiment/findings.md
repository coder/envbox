# EKS Auto Mode Bottlerocket Envbox findings

Experiment started: 2026-08-08

## Objective

Determine whether Envbox can run on EKS 1.36 Auto Mode Bottlerocket with its
intended privileged-outer/unprivileged-inner security model, while eliminating
the node-sysctl preparation race with a NodePool startup taint and a privileged
preparation DaemonSet.

If that baseline passes, test a shared, Envbox-managed node-local image cache
without sharing the node container runtime's data store.

## Status

**Published Envbox 0.6.7 baseline: pass. Cache POC: pass.** The
startup-taint anti-race mechanism, intended outer/inner security split,
nested developer workload, same-node restart, and lifecycle across three
independently provisioned Bottlerocket nodes have passed. The third node
included a repeated fail-closed control followed by successful release. An
opt-in, digest-keyed node-local image-cache prototype was implemented from
Envbox `main` pinned at upstream base commit
`7f301fa2bbe790ee53c7d0a3c1a76c94c11aac4b`.
Its decisive EKS proof passed: one 46-second cold miss and two
registry-blocked cache hits became usable in 32 and 36 seconds. Focused tests
after the final cache and cgroup corrections passed; the full final suite and
production hardening remain pre-PR follow-up work.

## Prior evidence

- Dogfood commit `e31c0f248f8c7212036e46db5aa8541fc39de588`
  recorded that Bottlerocket's `user.max_user_namespaces=0` prevented Sysbox
  from creating Envbox's inner user namespace.
- Dogfood's original privileged preparation DaemonSet could change the value,
  but did not prevent a workspace from scheduling first.
- The earlier native-user-namespace Auto Mode experiment demonstrated that a
  NodePool startup taint plus a privileged `control_t` preparation DaemonSet
  can order host-sysctl preparation before an ordinary workload on a fresh
  node.
- None of that evidence proves a complete Envbox `workspace_cvm` lifecycle on
  Auto Mode Bottlerocket.

## Planned decision gates

1. EKS 1.36 Auto Mode cluster and dedicated Bottlerocket NodePool.
2. Negative control: failed preparation leaves Envbox unscheduled.
3. Positive control: verified preparation removes the startup taint before
   Envbox schedules.
4. Envbox outer Sysbox lifecycle and unprivileged inner-container validation.
5. Nested Docker, BuildKit, networking, EBS persistence, and resource limits.
6. Replacement-node repetition.
7. Conditional node-local cache prerequisite and implementation tests.

## Results

### Cluster creation

**Pass (control-plane and default-capacity baseline).** On 2026-08-08:

- `eksctl 0.229.0` created `envbox-auto-bottlerocket-136` in `us-east-2`;
- EKS reported Kubernetes `1.36`, platform version `eks.9`, and status
  `ACTIVE`;
- the API server reported `v1.36.2-eks-bca9cf6`;
- the default Auto Mode `NodeClass/default` was ready;
- the default `system` NodePool had two ready nodes while
  `general-purpose` had zero;
- both observed system nodes were arm64 Bottlerocket
  `2026.8.3 (aws-k8s-1.36-standard)`, kernel `6.18.38`, with
  `containerd://2.2.5+bottlerocket`.

These arm64 system nodes are only the cluster baseline. The experiment will
request a separate amd64 `m6i.large` NodePool so that the Envbox image and
the historical Dogfood configuration are tested on the intended architecture.

### Anti-race controls

**Pass.**

- The installed `karpenter.sh/v1` NodePool CRD exposes
  `spec.template.spec.startupTaints` and describes them as temporary taints
  normally removed by a DaemonSet.
- The CRD explicitly says startup taints are ignored for provisioning, so an
  Envbox Pod need not tolerate the preparation taint in order to cause a new
  node to be provisioned.
- `spec.limits` is supported, including `limits.nodes`, allowing this
  experiment to cap the dedicated pool at one node.
- Server-side dry-run accepted the namespace, dedicated NodePool, Service
  Account, node-patching RBAC, source ConfigMap, and privileged preparation
  DaemonSet without admission errors.

Admission passed, and the execution results below subsequently proved the
Bottlerocket host-sysctl access and both sides of the scheduling gate.

The admitted resources were then created successfully:

- `NodePool/envbox-auto` became ready with zero nodes;
- its effective configuration contains the permanent isolation taint and
  `experiment.coder.com/envbox-prep=required:NoSchedule` startup taint;
- it is restricted to Linux/amd64, on-demand `m6i.large`, and one node;
- `DaemonSet/envbox-node-prep` initially reported desired/current/ready
  `0/0/0`, as expected before any matching workload requested capacity.

This clean zero-node state is the starting point for the negative control.

The negative-control workload inputs were then admitted and created:

- server-side dry-run accepted the Auto Mode EBS StorageClass, PVC, and
  published Envbox 0.6.7 baseline Pod manifest;
- `PVC/envbox-workspace` initially remained `Pending`, as expected for
  `WaitForFirstConsumer`;
- the live preparation DaemonSet retained
  `EXPERIMENT_FAIL_BEFORE_UNTAINT=true`;
- `Pod/envbox-smoke` was created as the capacity trigger.

At this checkpoint, the blank `NODE_NAME=` shown by the simple environment
JSONPath was expected:
that variable uses a downward-API `valueFrom` and is populated separately in
each scheduled preparation Pod. The node-provisioning and fail-closed outcome
had not yet been recorded; the following section records the result.

#### Negative control: pass

The deliberate failure mode produced the required fail-closed result:

- pending `Pod/envbox-smoke` caused Auto Mode to provision exactly one
  `m6i.large` NodeClaim and fresh amd64 Bottlerocket node
  `i-02e14b4f48a55aba5`;
- the node was Bottlerocket 2026.8.3, kernel 6.18.38, with
  `containerd://2.2.5+bottlerocket`;
- `DaemonSet/envbox-node-prep` ran on that node while the Envbox Pod could
  not schedule;
- the prep process observed `user.max_user_namespaces=0`, changed it to
  `65535`, and read it back successfully;
- it also changed and verified `user.max_pid_namespaces=20000`,
  `user.max_mnt_namespaces=20000`,
  `fs.inotify.max_user_watches=1048576`, and
  `fs.inotify.max_user_instances=8192`;
- it then explicitly reported that the negative control was active and
  retained `experiment.coder.com/envbox-prep`;
- the node retained both the permanent isolation taint and startup taint;
- `envbox-smoke` remained `Pending` with no node, init-container statuses,
  or main-container statuses, so no Envbox code or image pull began;
- the EBS claim remained `Pending`, consistent with
  `WaitForFirstConsumer` while the consumer was still gated.

The scheduler first nominated `nodeclaim/envbox-auto-r28qb`, proving the
startup taint did not prevent capacity provisioning. It nevertheless refused
to bind the Envbox Pod after the node joined because that Pod does not
tolerate the startup taint. This is the desired anti-race behavior.

The preparation Pod intentionally remained `0/1 Ready`: its readiness marker
is withheld in negative-control mode. That is expected, not a prep failure.

#### Positive control: pass

Before releasing the gate, the preparation process was inspected in place:

- SELinux context was `system_u:system_r:control_t:s0:c0.c1023`, the normal
  privileged Bottlerocket domain; no `super_t` override was used;
- permitted, effective, and bounding capability masks were full
  (`000001ffffffffff`), as expected for this intentionally privileged
  node-preparation Pod;
- only the five selected procfs sysctl files were bind-mounted at
  `/host-sysctl/*`, all read-write.

After removing `EXPERIMENT_FAIL_BEFORE_UNTAINT`, the replacement prep Pod:

1. read and verified all five already-prepared values;
2. added `experiment.coder.com/envbox-ready=true` to its assigned node;
3. removed `experiment.coder.com/envbox-prep`;
4. became ready and continued monitoring the values.

The permanent `experiment.coder.com/envbox-auto=true:NoSchedule` isolation
taint remained. Only after the startup taint disappeared did the scheduler
bind `envbox-smoke` to `i-02e14b4f48a55aba5`. The EBS claim then bound, its
volume-initialization container completed, and the outer Envbox container
started.

This proves the startup-taint/DaemonSet mechanism closes the original
Dogfood scheduling race on the tested Auto Mode release. It also proves the
required host sysctls can be prepared with an ordinary privileged
`control_t` DaemonSet. It does not yet prove the Envbox inner workload works.

### Envbox baseline

**Pass for the published Envbox 0.6.7 baseline image.**

- Published baseline image: `ghcr.io/coder/envbox:0.6.7`.
- Resolved outer image digest:
  `ghcr.io/coder/envbox@sha256:93b212e287abeef70a854574dbd0f08df3ad65224f0632d1d5a3f1c0dc87f897`.
- The node pulled the 206,426,841-byte outer image in approximately 3.8
  seconds and the Kubernetes container remained running.
- The 30 GiB Auto Mode EBS PVC bound successfully.

Kubernetes `Ready` here only establishes that the outer `/envbox docker`
process is alive. Sysbox services, the private outer Docker daemon,
`workspace_cvm`, inner privilege state, systemd, nested Docker, and networking
remain to be inspected before this phase can pass.

The outer Envbox log subsequently established substantially more:

- the private outer Docker 29.2.1 daemon initialized successfully with the
  `overlay2` storage driver and BuildKit;
- `sysbox-mgr` entered system-container mode and listened on
  `/run/sysbox/sysmgr.sock`;
- Sysbox reported kernel support for idmapped mounts and overlayfs on
  idmapped mounts; shiftfs was absent but not needed;
- Envbox pulled the large inner image directly through its private daemon,
  including applying an approximately 909 MB uncompressed layer;
- the pull used manifest digest
  `sha256:770f0a4fc1f2f730ad3f9096a13e86e356e2d1f9c4c5081918d23623d578d72d`;
- image metadata detected Ubuntu, `/sbin/init`, and user `coder` as
  UID/GID 1000 with home `/home/coder`;
- the temporary metadata container and final `workspace_cvm` both registered
  with Sysbox at outer UID/GID offset `100000:100000`;
- the final Docker create request explicitly contained
  `Privileged:false`, `Runtime:"sysbox-runc"`, a 4 GiB memory limit, and
  the intended persistent mounts;
- `workspace_cvm` started and Envbox emitted `Envbox startup complete!`.

Immediately afterward, `sysbox-fs` logged a failed write to
`/proc/sys/net/core/default_qdisc` because that proc node did not exist. The
workspace remained running and startup had completed, so this is currently a
non-fatal warning—not yet evidence that networking is correct. Nested bridge,
outbound, and published-port tests must decide whether it has practical
impact.

Direct inspection of the live outer and inner containers remains necessary
before marking the baseline complete; Docker's create-request log is useful
evidence but should be corroborated with `docker inspect` and functional
tests.

Live inspection corroborated the principal architecture and security claims:

- the outer process ran as host-user-namespace UID 0 with full capabilities
  under Bottlerocket `control_t`, as expected for Envbox's privileged control
  plane;
- outer `dockerd`, `sysbox-mgr`, `sysbox-fs`, and managed containerd were all
  alive; `/dev/fuse`, `/lib/modules`, `/usr/src`, and the Sysbox manager
  socket were present;
- the private outer daemon used Docker 29.2.1, overlay2, cgroupfs, and cgroup
  v2;
- `docker inspect workspace_cvm` reported `running=true`,
  `privileged=false`, `runtime=sysbox-runc`, a 4 GiB memory limit, and
  `/sbin/init` running as container root;
- the only host-configured mounts into `workspace_cvm` were
  `/var/lib/coder/containers -> /var/lib/containers`,
  `/var/lib/coder/docker -> /var/lib/docker`, and
  `/home/coder -> /home/coder`; the outer-only kernel and Sysbox paths were
  not passed through;
- from the outer process table, inner PID 1 and root-owned services appeared
  as numeric UID 100000, while non-root inner services appeared at further
  offsets such as 100103 and 100104;
- inside the workspace, UID/GID 0 mapped to outer IDs 100000-165535, PID 1
  was systemd, and root held full capabilities in that nested user namespace;
- inner Docker 23.0.1 was active and used overlay2, the systemd cgroup driver,
  and cgroup v2.

The inner SELinux label was also `control_t`. That label alone does not turn
inner root into host root: its capabilities and UID 0 are scoped by the
Sysbox-created user namespace, as demonstrated by the 0-to-100000 UID/GID
maps and outer process ownership. This is nevertheless a powerful developer
sandbox and should not be described as an ordinary least-privilege
application container.

Outstanding observations:

- systemd reported `degraded`; failed units must be enumerated to determine
  whether this is benign container-image noise or a workspace problem;
- inner Docker warned that `bridge-nf-call-iptables` and
  `bridge-nf-call-ip6tables` were disabled;
- one defunct shim from Envbox's temporary metadata container was visible;
  a single zombie is not yet a functional failure, but repeated workspace
  operations should be checked for accumulation;
- the initial multi-target `findmnt` invocation produced no output and is
  inconclusive; each persistent target should be queried separately.

Nested child creation, BuildKit, DNS/HTTP, outbound networking, published
ports, and cgroup resource controls remain the decisive functional tests.

Those functional tests then passed:

- `docker run --rm alpine:3.21 id` pulled and launched a rootful child
  container successfully;
- BuildKit executed a `RUN id` step, exported an image, and that image ran
  with the expected `envbox-build-ok` output;
- a user-defined nested bridge provided service-name DNS and HTTP between
  child containers;
- a child container reached `example.com`, proving outbound DNS and
  networking;
- an nginx child published port 18080 and a host-network child reached it on
  the workspace loopback address;
- a resource-constrained child observed `memory.max=67108864` and
  `cpu.max=50000 100000`, exactly matching 64 MiB and 0.5 CPU.

The workspace's own delegated cgroup root exposed all relevant cgroup v2
controllers (`cpuset cpu io memory hugetlb pids misc`) in
`cgroup.subtree_control`, with `memory.max=4294967296`,
`cpu.max=100000 100000`, and unlimited PIDs. This matches the requested 4 GiB
and one-CPU inner limits and explains why nested Docker could create and
limit child cgroups.

EBS-backed mount inspection showed:

- `/home/coder` on the PVC's `/home` ext4 subdirectory;
- `/var/lib/docker` on `/cache/docker` with an idmapped ext4 mount;
- `/var/lib/containers` on `/cache/containers` with an idmapped ext4 mount.

The missing `default_qdisc` proc node and disabled bridge-netfilter warnings
therefore caused no failure in the tested child-container, BuildKit, bridge
DNS/HTTP, outbound, or published-port paths. This does not prove every
possible networking feature, but it covers the normal developer-workspace
use cases targeted by the experiment.

Systemd remained `degraded` because four units failed:
`sys-kernel-debug.mount`, `sys-kernel-tracing.mount`,
`kmod-static-nodes.service`, and `systemd-sysctl.service`. Docker and the
workspace stayed functional. Their detailed failure reasons should be saved
before deciding whether the degraded state is benign container-environment
noise.

Detailed inspection classified those failures:

- debugfs and tracefs mounts were denied, which is an expected restriction
  for a sandboxed system container and is not needed for the tested developer
  workflow;
- `kmod-static-nodes.service` could not execute `/bin/kmod` because that
  binary is absent from the selected inner image, making this an image/unit
  mismatch rather than an EKS or Envbox failure;
- `systemd-sysctl.service` failed only when applying `fq_codel` to
  `net/core/default_qdisc`, matching the prior Sysbox warning. The functional
  network suite passed despite it.

The degraded systemd state is therefore non-blocking for the tested
workspace. It should still be documented because a workload requiring
debugfs, tracefs, or that particular qdisc configuration would have different
requirements.

#### Same-node Pod recreation and EBS persistence: pass

Before recreation, user `coder` wrote
`2026-08-08T19:05:30Z envbox-persistence-marker` under `/home/coder`, and the
nested build image had digest
`sha256:8f9e33dce0e5f45315b2c199415680375d184e1c5abfad90642a690e3df21a31`.

After deleting and recreating `Pod/envbox-smoke` against the same PVC:

- `workspace_cvm` and its inner Docker daemon became usable after about 50
  seconds, including Kubernetes Pod startup;
- the marker remained readable as user `coder`;
- the same nested image digest remained in the inner Docker store;
- running that persisted image again produced `envbox-build-ok`;
- the PVC remained bound and the replacement Pod stayed on the prepared
  node.

Envbox still logged its unconditional `pulling image` path on the warm start,
but went from that log line to `Envbox startup complete!` in about two
seconds. The PVC-backed private outer Docker store avoided re-downloading and
re-extracting the already-present inner layers. This is the existing
per-workspace cache and illustrates the original limitation: another Envbox
workspace with a separate PVC/private daemon would not share it.

At this point the Envbox 0.6.7 substrate and normal nested-development path
work on the prepared Auto Mode Bottlerocket node, including same-node Pod
recreation and EBS persistence. The first fresh-node replacement below also
passed; one final replacement remains before declaring the repetition gate
complete.

### Replacement nodes

#### First replacement: pass

The first experiment node and NodeClaim were explicitly recorded as
`i-02e14b4f48a55aba5` and `envbox-auto-r28qb`. After deleting the Envbox Pod,
deleting that exact NodeClaim, and waiting for the old Node to disappear, the
NodePool returned to zero nodes. Recreating `envbox-smoke` provisioned a
distinct node and claim:

- node `i-03ee3275dadaa4788`;
- NodeClaim `envbox-auto-2rpsf`;
- Bottlerocket 2026.8.3 for EKS 1.36;
- kernel 6.18.38;
- `containerd://2.2.5+bottlerocket`.

The two-second polling timeline captured the anti-race sequence directly:

1. at `2026-08-08T19:13:03Z`, the new node existed with both the permanent
   isolation taint and `envbox-prep=required:NoSchedule`, while the Envbox Pod
   had no assigned node;
2. at `19:13:07Z`, the prep taint was gone but Envbox still had no assigned
   node;
3. at `19:13:11Z`, Envbox was assigned to the new node, which retained only
   the permanent isolation taint.

The new prep Pod independently observed the stock Bottlerocket values,
including `user.max_user_namespaces=0`, wrote and verified all five target
values, labeled the node ready, and only then removed the startup taint. Its
later repeated verification lines are the DaemonSet's ongoing monitoring,
not repeated untaint operations.

The Pod, `workspace_cvm`, and inner Docker became usable 76 seconds after
recreating the Kubernetes Pod. The existing EBS PVC attached to the new
node; both the developer marker and nested Docker image survived the node
replacement. The image retained digest
`sha256:8f9e33dce0e5f45315b2c199415680375d184e1c5abfad90642a690e3df21a31`
and again produced `envbox-build-ok`.

This is stronger than a same-node restart: it proves fresh Bottlerocket
capacity begins unprepared, the startup taint orders preparation correctly,
and the EBS-backed workspace state remains usable after Auto Mode replaces
the node.

#### Third fresh node, repeated negative control: pass

Before replacing the second node, the workspace Pod was deleted and
`EXPERIMENT_FAIL_BEFORE_UNTAINT=true` was restored on the prep DaemonSet.
Deleting NodeClaim `envbox-auto-2rpsf` removed node
`i-03ee3275dadaa4788` and again returned the pool to zero. Recreating the
workspace provisioned:

- node `i-05d37f1a80c65f2ec`;
- NodeClaim `envbox-auto-s54mw`.

The polling record from `19:34:01Z` through `19:34:20Z` continuously showed
the new node with both the permanent and startup taints and showed no Envbox
Pod assignment. The prep Pod independently began with the stock values,
including `user.max_user_namespaces=0`, successfully wrote and verified all
five target values, then explicitly retained the startup taint because the
negative control was active.

At that checkpoint `envbox-smoke` was still `Pending`, had no assigned node,
and had no init-container or main-container status. This repeats the
fail-closed result on independently provisioned capacity: verified sysctl
writes alone cannot release a workspace when the prep process withholds its
success transition.

#### Third fresh node, positive release: pass

Removing `EXPERIMENT_FAIL_BEFORE_UNTAINT` replaced the prep Pod. The release
timeline showed the startup taint continuously present with no Envbox node
assignment from `19:35:46Z` through `19:36:02Z`. At `19:36:06Z`, Envbox was
assigned to `i-05d37f1a80c65f2ec` and the node retained only the permanent
isolation taint.

The positive prep Pod re-read all five values left by the negative-control
Pod, verified them, added `experiment.coder.com/envbox-ready=true`, removed
the startup taint, and became ready. Envbox, `workspace_cvm`, and inner Docker
were usable 45 seconds after beginning the release. The PVC remained bound,
the original developer marker was readable, and the persisted nested image
still had digest
`sha256:8f9e33dce0e5f45315b2c199415680375d184e1c5abfad90642a690e3df21a31`
and produced `envbox-build-ok`.

The original node and two explicit replacements provide three fresh-node
observations. On every fresh node, Bottlerocket began with
`user.max_user_namespaces=0`; preparation succeeded without `super_t`; and
Envbox did not bind while the startup taint existed. Both deliberate
negative controls withheld scheduling, and every positive release allowed a
working unprivileged Sysbox inner workspace. The replacement-node and race
regression gate is therefore complete for Envbox 0.6.7.

Overall conclusion for the published Envbox 0.6.7 baseline: Envbox's
privileged outer / unprivileged inner architecture is viable on the tested EKS
1.36 Auto Mode Bottlerocket release when the dedicated NodePool uses this
startup-taint preparation protocol. This conclusion is scoped to the tested
versions and developer workflow; it does not remove the outer Pod's privileged
security implications or the documented nonblocking systemd limitations.

The baseline viability conclusion is specifically for the published Envbox
0.6.7 image and its resolved digest recorded above. The cache POC separately
used modified, main-derived images published to a temporary ECR repository.

### Node-local image cache

#### Shared hostPath prerequisite: same-node sharing passes

At this prerequisite checkpoint, `/var/lib/envbox-image-cache` was only a
dedicated node-local directory. It did not share Docker/containerd data roots
and did not yet contain an image cache implementation.

Server-side admission accepted both privileged probe manifests. On node
`i-05d37f1a80c65f2ec`, probe A mounted the path at `/cache` and reported:

- SELinux `system_u:system_r:control_t:s0:c0.c1023`;
- XFS source `/dev/nvme1n1p1` with mount root
  `/var/lib/envbox-image-cache`;
- read-write, `seclabel` mount options;
- initial directory ownership `0:0` and mode `0755`.

Probe A atomically published `probe-a-v1`. A distinct probe B Pod on the same
node read that exact value and atomically replaced it with `probe-b-v1`.
After deleting and recreating probe A, the new Pod read `probe-b-v1` rather
than initializing a new marker. All marker checks passed; the published file
was `0:0`, mode `0600`, and 11 bytes.

This proves `DirectoryOrCreate` plus the privileged Envbox-equivalent
`control_t` context permits same-node, cross-Pod sharing and atomic
replacement on this Bottlerocket release.

#### Outer-only cache visibility: pass, with one startup flake

A separate Envbox manifest added only the dedicated hostPath at outer path
`/cache`; it did not add `/cache` to `CODER_MOUNTS`. Server admission accepted
the manifest as a new Pod.

The first Pod attempt failed during Envbox startup. `sysbox-fs` logged its
initial banner and FUSE directory but never logged readiness or created
`/run/sysbox/sysfs.sock`; `sysbox-mgr` did become ready. The warm private
Docker daemon then tried to start Envbox's temporary image-metadata container
and failed because the Sysbox FS socket was absent. Envbox exited with status
1. The complete failure log was preserved.

Deleting and recreating the Pod from the identical manifest succeeded: the
Pod was running and `workspace_cvm` existed after 15 seconds. Because the
same cache mount worked on retry and the failure occurred at the existing
Sysbox FS readiness boundary, this is not evidence that Bottlerocket rejected
or could not mount the cache. It is still an observed Envbox startup flake
that a production design must tolerate or eliminate; the Kubernetes Pod has
no readiness probe and briefly reported Ready during the failed attempt.

On the successful retry:

- the outer process ran under `control_t`, mounted
  `/var/lib/envbox-image-cache` at `/cache`, and read `probe-b-v1`;
- `docker inspect workspace_cvm` still reported `Privileged=false`;
- its only mounts were the private/PVC-backed Docker, containers, and home
  paths;
- `/cache` was absent from the Docker mount configuration and inner
  mountinfo;
- the inner workspace could not see `/cache/share-probe`.

The shared directory is therefore available to the trusted outer Envbox
control plane without exposing it to the developer-controlled inner
workspace.

#### Node-replacement disposal: pass

Node `i-05d37f1a80c65f2ec`, which contained marker `probe-b-v1`, was removed
through its exact NodeClaim after all consuming Pods were deleted. The
NodePool returned to zero. Recreating probe A provisioned distinct node
`i-080cabd57d1e52a75`; its cache mount used a different XFS device identity
(`259:10` rather than `259:2`) and began as an empty directory. Probe A took
the initialization branch and published a fresh `probe-a-v1` instead of
observing `probe-b-v1`.

The hostPath storage prerequisite is therefore complete: it is shareable
across trusted outer Pods on one node, hidden from the inner workspace,
persistent across Pod recreation, and discarded with node replacement. This
is the intended best-effort node-local lifecycle.

Envbox 0.6.7 itself is not an image cache. The prototype was developed in a
jj working copy starting from Envbox `main` at upstream base commit
`7f301fa2bbe790ee53c7d0a3c1a76c94c11aac4b`. The prototype bookmark is
`george/inner-image-cache`. Working-copy commit IDs evolved during
prototyping, so the immutable ECR image digests below, rather than a transient
local commit ID, identify the tested modified builds.
`CODER_INNER_IMAGE_CACHE_DIR` opts into a versioned Docker-archive
cache and requires a sha256-digest-pinned `CODER_INNER_IMAGE`. The prototype
uses per-digest file locks, archive checksums, config-digest verification,
atomic publication, corruption recovery, and explicit result logs. Docker
save/load does not preserve canonical repository digests, so cache archives
use a deterministic private tag and a verified loaded config ID is passed to
the inner-container creation path. The cache directory is still outer-only.

Focused unit tests passed for miss-to-hit behavior, mutable-reference
rejection, truncated and incomplete entry recovery, and two concurrent cold
starts producing one pull/save plus one load. An earlier revision passed the
complete repository unit suite with `go test ./... -count=1`. After the final
cache and cgroup corrections, `go test ./dockerutil/... ./cli -count=1` and
`git diff --check` passed; the complete final suite remains to be rerun before
a PR. The prototype container image built successfully with
`make build/image/envbox`; its initial local image ID was
`sha256:0b62238c8e53394982138af512f82d0ffb6f978538a3092adcb129432285183b`
and its reported size is 204,846,758 bytes. Temporary immutable private ECR
repository
`849808308023.dkr.ecr.us-east-2.amazonaws.com/envbox-eks-cache-experiment`
was created for the prototype and must be force-deleted during experiment
teardown. The prototype was pushed as immutable tag `cache-v1`, resolving to
manifest digest
`sha256:0b62238c8e53394982138af512f82d0ffb6f978538a3092adcb129432285183b`
(ECR reported 204,844,274 image bytes). Its first EKS cold start reached Ready
in 54 seconds on node `i-016a4eb521adb61f1`, but review before the hit test
found that its mock incorrectly assumed Docker save/load preserved source
`RepoDigests`. The hit test was intentionally not run against that build.

The implementation was corrected to archive a deterministic private tag,
record and validate the content-addressed image config ID, and use that ID for
metadata and workspace-container creation after a hit. The cache format was
bumped to `envbox-image-cache-v2`, focused tests passed, and corrected immutable
tag `cache-v2` was pushed at manifest digest
`sha256:92f53d2cc637979dfac855ad7d413f44a9f799c1dfd1a3d4242ae5e046d4f861`
(ECR reported 204,846,494 image bytes). The first v2 EKS attempt did not reach
cache initialization: current main's `unshare --cgroup` dockerd wrapper exited
while unmounting `/sys/fs/cgroup` with `target is busy`. Kubernetes briefly
reported Ready because the Pod has no startup/readiness probe, then the Pod
became Failed. This was a current-main cgroup-remount prerequisite failure,
not a cache result. It established that a mount-layout probe and compatibility
correction were required before the fresh cache miss and registry-blocked hit
could be repeated; eviction was not implemented in this first prototype.

The cgroup probe showed that before `unshare --cgroup`, the injected cgroup2
mount was rooted at `/`; afterward, cgroup membership was `/` but mountinfo
reported root `/../../../..`. Ordinary unmount returned EBUSY, and stacking a
second mount directly returned "already mounted". A lazy detach succeeded,
and the immediate replacement cgroup2 mount was rooted at `/`. The wrapper was
changed narrowly from `umount` to `umount -l`, focused tests passed, and
immutable ECR tag `cache-v3` was pushed at digest
`sha256:f48bb6494552adeaeb4f0ffa0c8aa01b74ba93665a5554efc8f000ff4b3fdf62`
(ECR reported 204,846,323 image bytes).

The v3 EKS cache proof passed on node `i-016a4eb521adb61f1`. The cold miss
became usable in 46 seconds and reported `status=miss`, 1,145,394,688 archive
bytes, and 27.338 seconds of cache work. Metadata recorded config digest
`sha256:f18945f7fc26b451c3cc8a997c1b1856bcff36f54361c8a61d0c2c37b412cb0e`;
the archive's observed SHA-256 exactly matched metadata.

For the hit, the replacement Pod stayed on the same node but used new empty
PVC subdirectories for outer `/var/lib/docker` and `/var/lib/containers`.
`us-docker.pkg.dev` was mapped to `127.0.0.1`, and a direct registry request
failed as intended. Envbox became usable in 32 seconds and reported
`status=hit`, the same 1,145,394,688 bytes, and 14.324 seconds of cache work,
with no pull progress. The loaded image had only the deterministic private
cache tag, no `RepoDigests`, and the expected config ID. `workspace_cvm` was
running from that ID with `privileged=false`; its inner Docker 23.0.1 used
overlay2 and cgroup v2. This decisively demonstrates registry-independent
node-local cache restore into an empty outer Docker store.

The live hit Pod also confirmed that outer Docker used the deliberately fresh
PVC subpaths `envbox-cache-hit/docker` and `envbox-cache-hit/containers`, while
`workspace_cvm` received only `/var/lib/docker`, `/var/lib/containers`, and
`/home/coder`; the node cache remained absent inside the workspace. The
experiment owner accepted this as sufficient POC evidence and stopped further
cache development. Corruption recovery, simultaneous cold starts, eviction,
and additional lifecycle hardening are intentionally deferred. The proposed
archive-truncation test was not executed, so the valid node-cache entry remains
intact.

One controlled warm-hit repeat was run at the operator's request. It stayed on
the same node, kept the source registry blocked, and used a second new empty
outer data pair (`envbox-cache-hit-2/docker` and `containers`). Cache work was
14.116 seconds versus 14.324 seconds on the first hit, an immaterial 0.208
second difference. End-to-end usability was 36 seconds versus 32 seconds,
showing ordinary startup variance rather than an additional prewarm benefit.
Across the two cache-backed samples the mean was 34 seconds, 12 seconds (about
26%) below the single 46-second cold miss; the best observed saving was 14
seconds (about 30%).
