# EKS Auto Mode Bottlerocket Envbox experiment

Date prepared: 2026-08-08

## Purpose

Determine whether Envbox can run reliably on EKS Auto Mode Bottlerocket with
its intended security model:

```text
EKS Auto Mode Bottlerocket node
  `- privileged Envbox outer container
       `- unprivileged Sysbox inner container (`workspace_cvm`)
            `- developer processes, systemd, dockerd, and child containers
```

The experiment has two ordered goals:

1. establish whether an Envbox workspace is technically viable on a stock
   EKS Auto Mode Bottlerocket node when node-global namespace sysctls are
   prepared without a scheduling race;
2. only after Goal 1 passes, test the prerequisites and a prototype for an
   Envbox-managed node-local image cache.

Do not begin the cache prototype merely because the shared `hostPath` works.
The baseline Envbox lifecycle, inner-container isolation, replacement-node
behavior, and nested Docker workload must pass first.

## Existing evidence

Dogfood commit `e31c0f248f8c7212036e46db5aa8541fc39de588` records a concrete
Auto Mode failure: Bottlerocket had `user.max_user_namespaces=0`, preventing
Sysbox from creating the inner user namespace. Dogfood added a privileged
DaemonSet to change the node-global value. That DaemonSet did not provide an
ordering guarantee; its comments say that a workspace could reach a fresh
node first and fail, with retry as the practical recovery.

The current Dogfood deployment uses AL2023 managed node groups and applies
the required sysctls before kubelet starts. The retained DaemonSet is evidence
that Envbox reached the user-namespace prerequisite on Bottlerocket, but it is
not proof that a complete Coder workspace passed on Auto Mode.

The earlier experiment in
`../eks-auto-mode-bottlerocket-userns-rootful-dind-experiment` established
that, on the tested EKS Auto Mode version:

- a NodePool `startupTaint` can gate ordinary workloads while allowing a
  privileged preparation DaemonSet to run;
- a privileged `control_t` Pod can write the host's
  `/proc/sys/user/max_user_namespaces` through a narrow `hostPath`;
- the DaemonSet can verify the value, patch its own Node, and remove the
  startup taint;
- a later `hostUsers: false` Pod and Auto Mode EBS PVC then work.

This runbook reuses that anti-race mechanism but tests Envbox's bundled
Sysbox lifecycle rather than native Kubernetes user namespaces.

## Decision rules

Record every phase as **pass**, **fail**, or **blocked**. Do not weaken a
failed test silently.

### Envbox is viable only if

1. a pending Envbox Pod can cause Auto Mode to create a fresh node even
   though the NodePool has a startup taint;
2. the Envbox Pod never starts before the preparation DaemonSet verifies the
   sysctls and removes that taint;
3. the normal privileged Bottlerocket context (`control_t`) is sufficient;
4. Envbox starts `sysbox-mgr`, `sysbox-fs`, and its private outer dockerd;
5. `workspace_cvm` starts with `Privileged=false` and an active user-namespace
   mapping;
6. systemd, rootful Docker, BuildKit, networking, resource limits, and an
   EBS-backed workspace directory work inside `workspace_cvm`;
7. the preparation and Envbox startup sequence repeats on replacement nodes
   without manual node access or host/containerd modification.

Requiring the Bottlerocket `super_t` SELinux type is useful diagnostic
evidence but is not a production pass: `super_t` is allowed to modify
arbitrary host files. The inner container must never be made privileged.

### The node-local cache is viable only if

1. two separate Envbox outer Pods on the same node can share a dedicated
   cache `hostPath` without exposing it to either inner container;
2. each Pod keeps a separate private outer Docker data root;
3. a second Pod can start from a verified cache entry without downloading the
   inner image from its registry;
4. concurrent misses, interrupted writes, corruption, and node replacement
   fail safely;
5. the implementation has an enforceable size bound and eviction policy.

The cache is node-local, disposable state. Losing it when Auto Mode replaces
a node is expected.

These were the original production-readiness criteria. After the core POC
proved a registry-independent hit from an empty private outer Docker store,
the experiment owner narrowed the stopping point: criteria 1-3 are the
completed POC gate, while concurrency, corruption recovery, eviction, and
production hardening in criteria 4-5 are explicitly deferred rather than
claimed as complete.

## Safety constraints

- Use a disposable cluster and a dedicated NodePool.
- Limit the experimental NodePool to one node so a failed startup taint does
  not cause uncontrolled node provisioning.
- Use a permanent taint to keep unrelated workloads off the node.
- The preparation DaemonSet may patch only the Node named by its downward-API
  `spec.nodeName`; its ClusterRole is powerful because Kubernetes RBAC cannot
  express "only the node on which this Pod runs" for dynamic node names.
- Mount individual `/proc/sys/...` files into the preparation Pod rather than
  the host root or all of `/proc`.
- Never mount the node's `/var/lib/containerd`, `/var/lib/docker`, CRI socket,
  or containerd socket.
- Never mount the proposed cache into `workspace_cvm`.
- Do not use a real Coder agent token during the initial Envbox substrate
  test. Add the real agent only after the substrate passes.

## Prerequisites

- `aws`, `eksctl`, and `kubectl` installed and authenticated.
- AWS permissions for disposable EKS, IAM, VPC, EC2, EBS, and Auto Mode
  resources.
- Permission to create privileged Pods, `hostPath` volumes, ClusterRoles,
  ClusterRoleBindings, NodePools, and NodeClasses.
- EKS Kubernetes 1.36 and Auto Mode available in the selected region.
- Sufficient quota for one `m6i.large` node and its EBS volumes.

Run from this directory:

```bash
cd ~/sysbox/0/eks-auto-mode-bottlerocket-envbox-experiment

export AWS_REGION="us-east-2"
export CLUSTER_NAME="envbox-auto-bottlerocket-136"
export EXPERIMENT_NS="envbox-auto"
export NODEPOOL_NAME="envbox-auto"
```

Confirm the account before creating billable resources:

```bash
aws sts get-caller-identity
aws --version
eksctl version
kubectl version --client -o yaml
```

## 1. Create the disposable EKS Auto Mode cluster

`cluster.yaml` should create an EKS 1.36 cluster with Auto Mode enabled.

```bash
eksctl create cluster -f cluster.yaml

aws eks update-kubeconfig \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME"

aws eks describe-cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME" \
  --query 'cluster.{name:name,status:status,version:version,platformVersion:platformVersion}' \
  --output yaml | tee cluster-version.yaml

kubectl version -o yaml | tee kubectl-version.yaml
kubectl get nodepools,nodeclasses -o wide \
  | tee initial-nodepools-and-nodeclasses.txt
kubectl get nodes -o wide | tee initial-nodes.txt
```

Stop if the server version is not 1.36. Record the actual node OS, kernel, and
runtime later; do not infer those solely from the product name.

Execution record (2026-08-08): **passed**. EKS reported `1.36` / `eks.9`,
and the API server reported `v1.36.2-eks-bca9cf6`. The two initial system
nodes used arm64 Bottlerocket 2026.8.3, kernel 6.18.38, and
containerd 2.2.5. They are not the experimental nodes; the dedicated
NodePool below deliberately requests amd64 `m6i.large` capacity.

## 2. Create the namespace, NodePool, and preparation resources

The dedicated NodePool must have:

- label `experiment.coder.com/envbox-auto=true`;
- permanent taint
  `experiment.coder.com/envbox-auto=true:NoSchedule`;
- startup taint
  `experiment.coder.com/envbox-prep=required:NoSchedule`;
- Linux/amd64 and `m6i.large` requirements;
- on-demand capacity;
- a one-node limit;
- enough consolidation delay to preserve the node while evidence is
  collected.

The Envbox Pod tolerates only the permanent taint. The preparation DaemonSet
tolerates both. A workspace must not tolerate the startup taint.

Validate that the installed Auto Mode CRD accepts the fields before applying:

```bash
kubectl explain nodepool.spec.template.spec.startupTaints
kubectl explain nodepool.spec.limits
kubectl apply --server-side --dry-run=server -f namespace.yaml
kubectl apply -f namespace.yaml
kubectl apply --server-side --dry-run=server -f nodepool.yaml
kubectl apply --server-side --dry-run=server -f node-prep.yaml
```

The namespace is applied between dry runs because server-side dry-run does
not persist it, while validation of the namespaced DaemonSet requires the
namespace to exist.

Execution record (2026-08-08): the installed `karpenter.sh/v1` CRD exposed
both `startupTaints` and `limits`; its field documentation confirmed that
startup taints are ignored for provisioning and are intended to be removed
by an initialization DaemonSet.

Server-side dry-run subsequently accepted `namespace.yaml`, `nodepool.yaml`,
and every object in `node-prep.yaml`. This proves admission compatibility,
not that the DaemonSet can execute or modify Bottlerocket host sysctls.

Then apply them in this order:

```bash
kubectl apply -f nodepool.yaml
kubectl apply -f node-prep.yaml

kubectl get nodepool "$NODEPOOL_NAME" -o yaml \
  > nodepool.initial.yaml
kubectl -n "$EXPERIMENT_NS" get daemonset envbox-node-prep -o wide
```

No experiment node is expected yet. A DaemonSet alone may have desired count
zero until a workload causes Auto Mode to provision a matching node.

Execution record (2026-08-08): **passed**. `NodePool/envbox-auto` was ready
with zero nodes and its rendered spec preserved the one-node limit, amd64
`m6i.large` requirement, permanent taint, and startup taint. The preparation
DaemonSet reported desired/current/ready `0/0/0`, establishing a clean
pre-negative-control baseline.

The prep program must write and verify these Dogfood values before removing
the startup taint:

```text
user.max_user_namespaces      = 65535
user.max_pid_namespaces       = 20000
user.max_mnt_namespaces       = 20000
fs.inotify.max_user_watches   = 1048576
fs.inotify.max_user_instances = 8192
```

It may set `kernel.unprivileged_userns_clone=1` when that proc node exists.
The hard prerequisite is a nonzero `user.max_user_namespaces`; the remaining
values reproduce Dogfood's intended workspace-node configuration.

## 3. Negative control: prove that preparation fails closed

Create the Auto Mode EBS StorageClass and PVC, and validate the workload
manifest:

```bash
kubectl apply --server-side --dry-run=server -f storage.yaml
kubectl apply --server-side --dry-run=server -f envbox-smoke.yaml
kubectl apply -f storage.yaml
kubectl -n "$EXPERIMENT_NS" get pvc envbox-workspace
```

`node-prep.yaml` deliberately starts with its failure mode enabled. Confirm
the live DaemonSet still has that setting before creating the workload:

```bash
kubectl -n "$EXPERIMENT_NS" get daemonset envbox-node-prep \
  -o jsonpath='{range .spec.template.spec.containers[?(@.name=="prep")].env[*]}{.name}={.value}{"\n"}{end}'

kubectl apply -f envbox-smoke.yaml
```

The prep process should write and read back the sysctls but refuse to publish
readiness or remove the startup taint.

Execution record (2026-08-08): both workload manifests passed server-side
dry-run. The EBS PVC began in the expected `WaitForFirstConsumer` pending
state, the live prep template had
`EXPERIMENT_FAIL_BEFORE_UNTAINT=true`, and `Pod/envbox-smoke` was created.
The capacity and scheduling outcome remained to be observed.

Watch provisioning and capture events:

```bash
kubectl get nodes,nodeclaims -o wide --watch
```

In another shell:

```bash
kubectl -n "$EXPERIMENT_NS" get pods -o wide --watch
```

After the node and prep Pod appear:

```bash
kubectl -n "$EXPERIMENT_NS" logs \
  -l app.kubernetes.io/name=envbox-node-prep \
  --prefix --tail=200 \
  | tee test1-prep-negative.log

kubectl -n "$EXPERIMENT_NS" get pod envbox-smoke -o yaml \
  > test1-envbox-pending.yaml
kubectl -n "$EXPERIMENT_NS" describe pod envbox-smoke \
  > test1-envbox-pending.describe.txt
kubectl get nodes -l experiment.coder.com/envbox-auto=true -o yaml \
  > test1-node-still-tainted.yaml
kubectl get events -A --sort-by=.lastTimestamp \
  > test1-events.txt
```

Required result:

- exactly one experiment node is created;
- the prep Pod runs on it;
- the startup taint remains;
- `envbox-smoke` remains unscheduled and its container never starts.

If Envbox schedules during this test, stop: the anti-race design is invalid.

Execution record (2026-08-08): **passed**. The pending Envbox Pod caused one
fresh amd64 `m6i.large` Bottlerocket node to be provisioned. The prep Pod
changed and verified all five requested host sysctls, including changing
`user.max_user_namespaces` from 0 to 65535, and then deliberately retained
the startup taint. The Envbox Pod remained pending with no node assignment
and no init or main container statuses. Both taints remained on the node.

## 4. Positive control: remove the gate only after verification

Disable the deliberate failure and wait for the DaemonSet replacement:

```bash
kubectl -n "$EXPERIMENT_NS" set env daemonset/envbox-node-prep \
  EXPERIMENT_FAIL_BEFORE_UNTAINT-

kubectl -n "$EXPERIMENT_NS" rollout status \
  daemonset/envbox-node-prep --timeout=10m

kubectl -n "$EXPERIMENT_NS" logs \
  -l app.kubernetes.io/name=envbox-node-prep \
  --prefix --tail=200 \
  | tee test2-prep-positive.log
```

Capture the gate transition and scheduling result:

```bash
export NODE_NAME="$(kubectl get nodes \
  -l experiment.coder.com/envbox-auto=true \
  -o jsonpath='{.items[0].metadata.name}')"

kubectl get node "$NODE_NAME" \
  -o jsonpath='{.metadata.name}{"\nlabels: "}{.metadata.labels}{"\ntaints: "}{.spec.taints}{"\n"}' \
  | tee test2-node-ready.txt

kubectl -n "$EXPERIMENT_NS" get pod envbox-smoke -o wide
kubectl -n "$EXPERIMENT_NS" describe pod envbox-smoke \
  > test2-envbox-after-gate.describe.txt
kubectl get events -A --sort-by=.lastTimestamp \
  > test2-events.txt
```

Required result:

- the prep log shows every value was verified;
- the node receives a diagnostic readiness label;
- the startup taint is removed while the permanent isolation taint remains;
- only then does `envbox-smoke` receive a Node assignment and start.

This proves the ordering mechanism. It does not yet prove Envbox works.

Execution record (2026-08-08): **passed**. The prep Pod used Bottlerocket's
ordinary privileged `control_t` SELinux domain with the five narrow sysctl
mounts. Its positive replacement re-verified every value, labeled the node,
and removed only the startup taint. Envbox scheduled afterward, the EBS claim
bound, and the outer 0.6.7 container started. The permanent isolation taint
remained. Inner Envbox/Sysbox operation was not inferred from Kubernetes
container readiness and was left for the next sections.

## 5. Record the Bottlerocket host interface

Collect the actual Auto Mode node identity:

```bash
kubectl get node "$NODE_NAME" \
  -o jsonpath='{.metadata.name}{"\nOS: "}{.status.nodeInfo.osImage}{"\nkernel: "}{.status.nodeInfo.kernelVersion}{"\nruntime: "}{.status.nodeInfo.containerRuntimeVersion}{"\n"}' \
  | tee test3-node-runtime.txt

kubectl get node "$NODE_NAME" -o yaml > test3-node.yaml
kubectl get nodeclaims -o yaml > test3-nodeclaims.yaml
```

Run `host-preflight.yaml` using the same privileged security context and
`/usr/src` plus `/lib/modules` host mounts intended for Envbox. Record:

- process SELinux label and effective capabilities;
- mount information for `/sys`, `/sys/fs/cgroup`, `/usr/src`, and
  `/lib/modules`;
- presence of `/dev/fuse` and relevant kernel modules;
- all `user.max_*_namespaces` values;
- filesystem types backing the Envbox state directories.

```bash
kubectl apply -f host-preflight.yaml
kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready pod/envbox-host-preflight --timeout=10m
kubectl -n "$EXPERIMENT_NS" logs envbox-host-preflight \
  | tee test3-host-preflight.log
kubectl -n "$EXPERIMENT_NS" describe pod envbox-host-preflight \
  > test3-host-preflight.describe.txt
```

Do not treat an absent `/usr/src` as an automatic failure. Record it and, if
necessary, run one controlled Envbox variant without that mount. A failure to
access `/lib/modules`, FUSE, `/sys`, or cgroups is more likely to be decisive.

## 6. Baseline Envbox smoke test

The first manifest should reproduce the Dogfood outer image and Pod shape as
closely as possible:

- outer image `ghcr.io/coder/envbox:0.6.7`, with the resolved image digest
  recorded from `status.containerStatuses[].imageID`;
- outer container `privileged: true` and ordinary Bottlerocket `control_t`;
- `restartPolicy: Never`;
- an unprivileged inner image containing user `coder`, systemd, and dockerd;
- no real `CODER_AGENT_URL` and no real Coder token;
- no bootstrap script for the first substrate test;
- private Envbox state volumes and an EBS/ext4 PVC for workspace-like state;
- the Dogfood `/usr/src` and `/lib/modules` host mounts;
- no cache `hostPath` yet.

The manifest may set a non-secret placeholder `CODER_AGENT_TOKEN` because the
initial test does not start a Coder agent.

Follow the outer logs:

```bash
kubectl -n "$EXPERIMENT_NS" logs -f envbox-smoke \
  | tee test4-envbox-outer.log
```

If the Pod exits or stalls, preserve evidence before changing anything:

```bash
kubectl -n "$EXPERIMENT_NS" get pod envbox-smoke -o yaml \
  > test4-envbox-pod.yaml
kubectl -n "$EXPERIMENT_NS" describe pod envbox-smoke \
  > test4-envbox-pod.describe.txt
kubectl -n "$EXPERIMENT_NS" logs envbox-smoke --timestamps \
  > test4-envbox-outer.timestamps.log
kubectl get events -A --sort-by=.lastTimestamp \
  > test4-events.txt
```

If it remains running, inspect the outer and inner environments:

```bash
kubectl -n "$EXPERIMENT_NS" exec envbox-smoke -- sh -ec '
  ps auxww
  test -S /run/sysbox/sysmgr.sock
  docker info
  docker ps --no-trunc
  docker inspect workspace_cvm \
    --format "privileged={{.HostConfig.Privileged}} runtime={{.HostConfig.Runtime}}"
  docker exec workspace_cvm cat /proc/self/uid_map
  docker exec workspace_cvm cat /proc/self/gid_map
  docker exec workspace_cvm cat /proc/self/cgroup
  docker exec workspace_cvm mount
  docker exec workspace_cvm systemctl is-system-running --wait || true
  docker exec workspace_cvm docker info
  docker exec workspace_cvm docker run --rm alpine:3.21 id
'
```

Then run an inner BuildKit and networking test:

```bash
kubectl -n "$EXPERIMENT_NS" exec envbox-smoke -- sh -ec '
  docker exec workspace_cvm sh -ec "
    mkdir -p /tmp/envbox-build
    printf \"FROM alpine:3.21\\nRUN id\\nCMD [\\\"sh\\\", \\\"-c\\\", \\\"echo envbox-build-ok\\\"]\\n\" \
      > /tmp/envbox-build/Dockerfile
    docker build -t envbox-inner-smoke:1 /tmp/envbox-build
    docker run --rm envbox-inner-smoke:1
    docker run -d --name envbox-httpd -p 18080:80 nginx:1.27-alpine
    wget -qO- http://127.0.0.1:18080/ | grep -q \"Welcome to nginx\"
    docker rm -f envbox-httpd
  "
'
```

Required assertions:

- outer processes and sockets remain healthy;
- `workspace_cvm` uses `sysbox-runc` and reports `Privileged=false`;
- its UID/GID maps show a non-host-root user namespace;
- inner systemd and dockerd function;
- image pull, child-container creation, BuildKit, bridge networking, published
  ports, and cleanup work;
- PVC-backed files remain readable after recreating the Envbox Pod;
- the inner container has no access to outer-only host paths.

Execution note (2026-08-08): the 0.6.7 outer log reached
`Envbox startup complete!`. It showed a private overlay2 Docker daemon,
healthy Sysbox manager registration, kernel idmapped-mount support, and a
final `workspace_cvm` create request with `Privileged:false` and
`Runtime:"sysbox-runc"`. Sysbox registered the container with UID/GID offset
100000. A later non-fatal `sysbox-fs` error reported that
`/proc/sys/net/core/default_qdisc` did not exist; the networking tests below
must establish whether this has observable impact.

Live inspection then confirmed the intended split: the outer ran as
host-user-namespace root with full capabilities under `control_t`, while
`workspace_cvm` was running with `Privileged=false`, runtime `sysbox-runc`,
and inner UID/GID 0 mapped to outer 100000. Inner PID 1 was systemd and inner
Docker was active with overlay2 and cgroup v2. Only the three intended
persistent mounts entered the workspace. Systemd was degraded and Docker
reported disabled bridge-netfilter sysctls, so neither is considered benign
until the failed-unit and functional networking checks are recorded.

Functional execution record (2026-08-08): **passed**. Rootful child creation,
BuildKit, nested bridge DNS/HTTP, outbound access, an nginx published port,
and child cgroup limits all worked. The parent workspace exposed the full
delegated cgroup v2 controller set at its private root with the requested
4 GiB and one-CPU limits; a child observed the exact requested 64 MiB and
0.5-CPU limits. The EBS-backed Docker and containers paths were idmapped ext4
mounts. Four failed systemd units remained to classify, and PVC persistence
across Envbox Pod recreation remained untested.

Persistence execution record (2026-08-08): **passed**. The four degraded
systemd units were classified as denied debug/trace mounts, a missing
`/bin/kmod` in the chosen image, and the already-observed qdisc sysctl error;
none affected the tested workflow. A developer-owned home marker and nested
Docker build image survived Envbox Pod deletion/recreation on the same EBS
PVC, and the persisted image ran. The complete warm restart took about 50
seconds; Envbox still entered its pull path, but the cached-layer check through
startup completion took roughly two seconds. This is per-workspace PVC cache
behavior, not node-local sharing.

## 7. Diagnostic ladder for a baseline failure

Change one variable at a time and preserve every failed manifest and log.

1. **Admission or mount setup:** inspect Pod events for privileged or
   `hostPath` rejection.
2. **Missing host paths:** retry without `/usr/src`; do not omit
   `/lib/modules` without establishing whether Sysbox can operate without it.
3. **User namespaces:** verify the host-visible sysctl values and Sysbox's
   exact error.
4. **SELinux:** capture `/proc/self/attr/current` and any node/debug logs that
   expose AVC denials.
5. **FUSE or kernel support:** inspect `sysbox-fs`, `/dev/fuse`, and module
   availability.
6. **Cgroups:** capture `/proc/self/cgroup`, cgroup mountinfo, controller
   files, ownership, and the Envbox cgroup-wrapper error.
7. **Idmapped mounts or storage:** compare an `emptyDir` state variant with
   the EBS/ext4 PVC variant.
8. **Diagnostic `super_t`:** only after capturing the normal `control_t`
   failure, try a separate manifest with `seLinuxOptions.type: super_t`.

If Envbox needs host-containerd changes, a writable host root, or `super_t` to
operate, record the baseline as not production-viable on stock Auto Mode even
if a diagnostic workaround starts it.

## 8. Replacement-node and race regression

After a complete pass, repeat the lifecycle on at least three fresh nodes.
For each iteration:

1. save all evidence from the current node;
2. delete the Envbox Pod;
3. remove the experiment NodeClaim or otherwise let Auto Mode replace the
   node;
4. wait until the old node is gone;
5. recreate `envbox-smoke` so it triggers new capacity;
6. verify the new node begins with the startup taint;
7. verify only the prep DaemonSet runs before untainting;
8. verify Envbox passes again.

Do not delete a NodeClaim while the Envbox Pod or its PVC is still using that
node. Record exact NodeClaim names before deleting any resource.

Also repeat the negative-control failure once on a fresh node. An unavailable
prep image or failed sysctl write must leave the workspace pending, not start
it on an unprepared node.

First replacement execution record (2026-08-08): **passed**. Deleting
NodeClaim `envbox-auto-r28qb` removed node `i-02e14b4f48a55aba5` and returned
the pool to zero. Recreating the workload provisioned node
`i-03ee3275dadaa4788` through NodeClaim `envbox-auto-2rpsf`. A two-second
timeline captured the node with the startup taint and no Pod assignment,
then the startup taint removed with no assignment, and finally Envbox bound
to the node while only the permanent isolation taint remained. The prep Pod
started from `user.max_user_namespaces=0` and verified all five target
sysctls. The complete fresh-node workspace recovery took about 76 seconds.
The EBS-backed home marker and nested Docker image survived and the image ran
successfully.

The original node plus this replacement account for two fresh nodes. For the
third node, restore `EXPERIMENT_FAIL_BEFORE_UNTAINT=true` before replacement,
prove the new workspace remains unscheduled, preserve the evidence, then
remove the flag and verify the same node completes normally. This combines
the final positive repetition with the repeated fail-closed control.

Third-node negative-control execution record (2026-08-08): **passed**.
Deleting NodeClaim `envbox-auto-2rpsf` removed the second node and returned
the pool to zero. The pending workload then provisioned node
`i-05d37f1a80c65f2ec` through NodeClaim `envbox-auto-s54mw`. From
`19:34:01Z` until the prep result at `19:34:20Z`, polling showed both taints
present and no workspace assignment. The prep Pod began with
`user.max_user_namespaces=0`, verified all five target values, and
intentionally retained the startup taint. The workspace remained Pending
with no node or container statuses. The positive release on this same node
was then tested.

Third-node positive execution record (2026-08-08): **passed**. After removing
the failure flag, polling continued to show the startup taint and no Envbox
assignment through `19:36:02Z`. At `19:36:06Z`, the startup taint was absent
and the workspace had been assigned; the permanent isolation taint remained.
The replacement prep Pod verified every value, labeled the node ready, and
removed the gate. The workspace and inner Docker were usable 45 seconds from
the start of release. The EBS home marker and nested image survived this
second node replacement, and the image again ran successfully.

Section 8 is **passed for Envbox 0.6.7**. The original capacity plus two
explicit replacements cover three fresh Bottlerocket nodes. Two deliberate
negative controls failed closed, and all positive releases produced working
workspaces without manual node access, host-containerd changes, or `super_t`.

## 9. Conditional cache prerequisite test

Proceed only after Sections 6 and 8 pass.

Add a dedicated volume to the outer Envbox Pod only:

```yaml
volumes:
  - name: envbox-image-cache
    hostPath:
      path: /var/lib/envbox-image-cache
      type: DirectoryOrCreate
```

Mount it as `/cache` in the outer container. Do not include `/cache` in
`CODER_MOUNTS` or any inner-container mount list.

Use `cache-share-probe-a.yaml` and `cache-share-probe-b.yaml` to run two
sequential privileged outer Pods on the same node:

1. Pod A records its SELinux context, creates a cache marker using a temporary
   file plus atomic rename, reads it, and exits;
2. Pod B records its context, reads the same marker, replaces it atomically,
   and exits;
3. recreate Pod A and verify the replacement value;
4. replace the node and verify the old marker is absent.

Capture host-path mountinfo, numeric ownership, and SELinux labels where
visible. Confirm from `docker inspect workspace_cvm` and an inner `findmnt`
that `/cache` is not exposed to the workspace.

This phase proves only that Bottlerocket can host the shared directory. It
does not prove image-download avoidance.

Same-node sharing execution record (2026-08-08): **passed**. Both probe Pods
ran under `control_t` and mounted the same XFS host path read-write. Probe A
atomically published `probe-a-v1`; probe B read it and atomically replaced it
with `probe-b-v1`; a deleted and recreated probe A then read B's value. The
marker remained mode `0600`, owner `0:0`. Outer-only workspace isolation and
node-replacement disposal are the remaining prerequisite checks.

Outer-only isolation execution record (2026-08-08): **passed with a startup
flake recorded**. An Envbox variant mounted the hostPath only in the outer
container. Its first start failed because `sysbox-fs` never created its Unix
socket even though `sysbox-mgr` became ready. Recreating the identical Pod
succeeded in 15 seconds. The outer process then read `probe-b-v1`, while
`workspace_cvm` remained `Privileged=false`, had only its three intended
mounts, and had neither a `/cache` mount nor access to the marker. Preserve
the failure as reliability evidence; do not attribute it to the cache mount
without a repeatable comparison.

Node-replacement disposal execution record (2026-08-08): **passed**. The
node containing `probe-b-v1` was removed and the pool returned to zero. Probe
A then provisioned a distinct node and mounted an empty cache directory on a
different XFS device identity. It initialized `probe-a-v1`; the old marker
did not follow the workload. Section 9's hostPath prerequisite is complete.

## 10. Conditional Envbox cache prototype

Unmodified Envbox at the starting main commit always called its private Docker
daemon's image-pull path and did not consume the shared directory. A
meaningful cache test therefore required a small Envbox prototype.

For the first prototype:

- require a digest-pinned `CODER_INNER_IMAGE`;
- make the feature opt-in with a cache directory such as `/cache`;
- use one lock per immutable image digest;
- on a miss, perform the existing pull, inspect the resolved image, export it
  with Docker `ImageSave`, write integrity metadata, fsync, and atomically
  publish the archive;
- on a hit, verify the archive and metadata, import it with `ImageLoad`, and
  skip `ImagePull`;
- delete or quarantine corrupt/incomplete entries and fall back to a normal
  pull;
- emit explicit hit/miss/corruption, byte-count, and duration logs;
- never cache registry credentials or mount the cache into the inner
  container.

Use separate private `/var/lib/docker` directories for every Envbox Pod. The
shared directory is an image-archive cache, not a shared Docker data root.

Core POC test:

1. start Pod A on an empty node cache and confirm one cache miss;
2. delete Pod A while keeping the node;
3. start Pod B with an empty private Docker root and the same shared cache;
4. prevent Pod B from reaching the image registry;
5. confirm Pod B reports a verified cache hit and starts `workspace_cvm`;
6. confirm no Docker pull event occurred;

Deferred production-hardening tests:

7. run simultaneous cold starts for the same digest and verify exactly one
   complete entry is published;
8. truncate an entry and verify safe detection plus recovery;
9. enforce a deliberately small cache limit and verify deterministic
   eviction without deleting in-use entries.

Mutable tags and remote digest resolution are a later phase. The first test
uses immutable references so cache correctness is not mixed with tag-refresh
semantics.

Implementation record, 2026-08-08: prototype work began directly in the
`~/envbox` jj working copy from Envbox `main` pinned at upstream base commit
`7f301fa2bbe790ee53c7d0a3c1a76c94c11aac4b`, at the operator's request. The
prototype bookmark is `george/inner-image-cache`. Working-copy commit IDs
evolved during prototyping; immutable ECR image digests identify the tested
modified builds. The opt-in variable is
`CODER_INNER_IMAGE_CACHE_DIR`. An earlier revision passed the complete unit
suite. After the final cache and cgroup corrections, focused
`./dockerutil/... ./cli` tests and `git diff --check` passed; the complete
final suite remains to be rerun before a PR. Docker save/load strips canonical
digest references; the prototype
therefore archives a deterministic private tag, verifies the original
repository digest before publication, records the pulled config digest in
checksummed metadata, and verifies the loaded config ID before creating the
inner container from that ID. `make build/image/envbox` then produced local
image ID
`sha256:0b62238c8e53394982138af512f82d0ffb6f978538a3092adcb129432285183b`
(204,846,758 bytes). Temporary immutable private ECR repository
`849808308023.dkr.ecr.us-east-2.amazonaws.com/envbox-eks-cache-experiment`
was created for publication. It is experiment-owned and must be deleted with
`aws ecr delete-repository --force` during teardown. Immutable tag `cache-v1`
was pushed successfully at manifest digest
`sha256:0b62238c8e53394982138af512f82d0ffb6f978538a3092adcb129432285183b`.
Its first EKS cold start reached Ready in 54 seconds, but pre-hit review found
that its fake Docker client masked Docker save/load dropping the source
`RepoDigest`; no hit claim was made. The implementation now archives a
deterministic private tag, validates the loaded config ID, and creates the
inner container from that ID. The cache format was bumped to v2, focused tests
passed, and immutable ECR tag `cache-v2` was pushed at digest
`sha256:92f53d2cc637979dfac855ad7d413f44a9f799c1dfd1a3d4242ae5e046d4f861`.
The first v2 EKS run exited before cache initialization because current main's
`unshare --cgroup` wrapper failed to unmount `/sys/fs/cgroup` with EBUSY. The
Pod's transient Ready condition was not an Envbox-ready signal. This required
diagnosing and correcting the current-main prerequisite before repeating the
fresh EKS miss/blocked-registry-hit test; eviction remained deferred at that
checkpoint.

The follow-up mount probe established that `unshare --cgroup` changed the
visible cgroup2 mount root from `/` to `/../../../..`; ordinary unmount failed,
whereas lazy detach followed immediately by a replacement cgroup2 mount
succeeded and produced root `/`. The wrapper now uses `umount -l`; focused
tests passed, and corrected immutable ECR tag `cache-v3` was pushed at digest
`sha256:f48bb6494552adeaeb4f0ffa0c8aa01b74ba93665a5554efc8f000ff4b3fdf62`.

Execution record: the v3 cold miss passed on node `i-016a4eb521adb61f1`,
publishing a checksummed 1,145,394,688-byte archive and becoming usable in 46
seconds. The decisive hit then used new empty outer Docker/container data
subdirectories and mapped the source registry to loopback. The registry probe
failed, while Envbox reported a cache hit in 14.324 seconds and became usable
in 32 seconds. The restored image had the deterministic cache tag, no source
`RepoDigests`, and the verified config ID; `workspace_cvm` remained
unprivileged and its inner Docker was healthy. The core cold-miss and
registry-independent-hit gate therefore passed.

POC stopping point: the fresh outer data subpaths were confirmed in the live
Pod and the cache remained absent from `workspace_cvm`. At the experiment
owner's direction, stop cache feature work here. Do not run the prepared
archive-corruption mutation. Concurrency, corruption recovery, eviction, and
production hardening remain explicit follow-up work rather than POC gates.

Optional repeat record: a second blocked-registry cache hit with another empty
outer Docker store completed cache work in 14.116 seconds and became usable in
36 seconds. The first hit was 14.324 seconds / 32 seconds. Treat the 0.208
second cache-phase change as noise; there was no meaningful additional
prewarm speedup. The two hit runs average 34 seconds versus the 46-second cold
miss.

## 11. Evidence and findings

Keep `findings.md` current after every phase. For each test record:

- UTC time;
- cluster, Kubernetes, platform, NodePool, NodeClass, OS, kernel, and runtime
  versions;
- exact outer and inner image references plus resolved digests;
- Pod security context and SELinux process type;
- Node taints and labels before and after preparation;
- sysctl values before, after, and after node replacement;
- commands, exit statuses, logs, events, and observed timings;
- whether the result is pass, fail, or blocked;
- every diagnostic deviation from the production candidate.

Do not store kubeconfigs, Coder tokens, registry credentials, AWS account
identifiers, signed URLs, or Kubernetes Secret contents in this directory.
Run a secret scan before publishing it.

## 12. Cleanup

Delete workload resources and PVCs before deleting the cluster so EBS cleanup
can complete while the control plane still exists:

```bash
export AUTO_VOLUME_IDS="$(
  kubectl get pv \
    -o jsonpath='{range .items[*]}{.spec.claimRef.namespace}{"\t"}{.spec.csi.volumeHandle}{"\n"}{end}' |
  awk -v ns="$EXPERIMENT_NS" '$1 == ns && $2 != "" { print $2 }'
)"

kubectl delete namespace "$EXPERIMENT_NS" \
  --ignore-not-found=true --wait=true --timeout=20m

if [ -n "$AUTO_VOLUME_IDS" ]; then
  aws ec2 wait volume-deleted \
    --region "$AWS_REGION" \
    --volume-ids $AUTO_VOLUME_IDS
fi

kubectl delete nodepool "$NODEPOOL_NAME" --ignore-not-found=true

eksctl delete cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME"

aws eks list-clusters --region "$AWS_REGION"
```

If cluster deletion fails, inspect CloudFormation before retrying. Do not
manually delete unrelated VPC, IAM, or EBS resources.

## References

- `~/dogfood/clusters/dogfood-workspaces/coder/workspaces-namespace/daemonset-sysbox-node-prep.yaml`
- `~/dogfood/terraform/workspace-clusters.tf`
- `~/dogfood/templates/coder-eks/main.tf`
- `~/sysbox/0/eks-auto-mode-envbox-daemonset-anti-race.md`
- `~/sysbox/0/envbox-managed-cache-in-shared-dir-on-node.md`
- `~/sysbox/sysbox-pkgr/k8s/scripts/sysbox-deploy-k8s.sh`
- `~/sysbox/sysbox-pkgr/k8s/manifests/runtime-class/sysbox-runtimeclass.yaml`
- <https://karpenter.sh/v1.0/concepts/nodepools/>
- <https://docs.aws.amazon.com/eks/latest/best-practices/automode.html>
- <https://docs.aws.amazon.com/eks/latest/userguide/auto-troubleshoot.html>
- <https://github.com/bottlerocket-os/bottlerocket/blob/develop/SECURITY_GUIDANCE.md>
