# Findings: EKS Auto Mode/Bottlerocket native user namespaces and rootful DinD

Date: 2026-08-06

## Question tested

Can a Coder workspace run rootful Docker and BuildKit in a native Kubernetes
user-namespace Pod on EKS 1.36 Auto Mode, without Envbox or Sysbox and without
a host-privileged workspace Pod?

The managed-node-group control experiment succeeded after adding a custom
containerd RuntimeClass with `cgroup_writable = true` and preparing a valid
domain-cgroup topology. This experiment tested whether an AWS-managed Auto
Mode node could support the same design.

## Environment

- AWS account: `849808308023`.
- Region: `us-east-2`.
- Cluster: `userns-rootful-dind-auto-136`.
- EKS control plane: Kubernetes `1.36`, platform `eks.9`.
- Auto Mode built-in NodePools:
  - `system`: two nodes;
  - `general-purpose`: initially zero nodes.
- Auto Mode NodeClass: AWS-managed `default`.
- Baseline experiment NodePool: `userns-dind-auto`.
- Prepared-node experiment NodePool: `userns-dind-auto-prepped`.
- Requested experiment instance: on-demand `m6i.large`, Linux/amd64.
- Baseline experiment node: `i-06b14ce01a7c2dbcd`.
- Prepared experiment node: `i-0c1011a1ef58147da`.
- Node OS family observed on the cluster:
  `Bottlerocket (EKS Auto, Standard) 2026.8.3
  (aws-k8s-1.36-standard)`.
- Container runtime observed on the initial Auto Mode system nodes and
  confirmed on the prepared experiment node:
  `containerd://2.2.5+bottlerocket`.

Both experiment NodePools used the unmodified AWS-managed `default`
NodeClass. Selecting `m6i.large` controlled architecture and experiment
isolation; it did not modify Bottlerocket or containerd.

## Provisioning results

### Auto Mode cluster and baseline NodePool: pass

The Auto Mode cluster became `ACTIVE`. The custom NodePool validated
successfully and reported `Ready=True`. It initially had zero nodes, as
expected for demand-driven Auto Mode provisioning.

Submitting the first matching Pod caused Auto Mode to create node
`i-06b14ce01a7c2dbcd`, nominate it, and schedule the Pod there.

### Baseline Auto Mode EBS provisioning: partial pass

The experiment created an explicit StorageClass:

```text
name: auto-gp3
provisioner: ebs.csi.eks.amazonaws.com
volumeBindingMode: WaitForFirstConsumer
filesystem: ext4
```

The `workspace-data` PVC initially remained `Pending`, as expected for WFFC.
After the Pod was scheduled, EBS provisioning and attachment succeeded:

```text
SuccessfulAttachVolume: AttachVolume.Attach succeeded for volume
pvc-bb224fe9-905a-4d37-9275-0747adb81a34
```

The baseline probe did not reach container creation, so at this point it had
not proved that the EBS volume could be mounted into a user-namespaced
container. The prepared-node continuation later completed that test.

## Baseline native user-namespace probe: fail

The first workload requested:

```yaml
spec:
  hostUsers: false
```

The Pod scheduled and its EBS volume attached, but containerd repeatedly
failed to create the Pod sandbox:

```text
FailedCreatePodSandBox: failed to create network namespace for sandbox:
failed to start noop process for unshare: fork/exec /proc/self/exe:
no space left on device
```

The Pod remained in `ContainerCreating`; no workload container process was
started.

## Node diagnostic

To distinguish literal storage exhaustion from a namespace limit, a second
diagnostic Pod used the host user and network namespaces. It scheduled on the
same dedicated node and reported:

```text
/proc/self/uid_map:
         0          0 4294967295

/proc/sys/user/max_cgroup_namespaces=31073
/proc/sys/user/max_ipc_namespaces=31073
/proc/sys/user/max_mnt_namespaces=31073
/proc/sys/user/max_net_namespaces=31073
/proc/sys/user/max_pid_namespaces=31073
/proc/sys/user/max_time_namespaces=31073
/proc/sys/user/max_user_namespaces=0
/proc/sys/user/max_uts_namespaces=31073
```

Only user-namespace creation was disabled. Writable filesystem capacity was
healthy:

```text
overlay: 79.9G total, 78.4G available, 2% used
overlay inodes: 41,941,220 available, 0% used
```

The read-only Bottlerocket `/dev/root` image appeared full, which is normal
for an immutable image and was unrelated to the failure. The writable node
filesystem had ample blocks and inodes.

## Interpretation

Linux returns `ENOSPC` from `clone(2)` or `unshare(2)` when creating a user
namespace would exceed `/proc/sys/user/max_user_namespaces`. On this Auto Mode
Bottlerocket node the limit was explicitly zero, so the runtime could not
honor `hostUsers: false`.

The error occurred while containerd was constructing the Pod sandbox. Its
wording referred to network-namespace setup because that was the sandbox
operation using the helper process, but the decisive ancestor limit was user
namespace creation.

This failure occurred before:

1. mounting the EBS volume into a user-namespaced container;
2. starting dockerd;
3. testing writable cgroup delegation;
4. testing Docker storage, BuildKit, bridge networking, or nested resource
   limits.

It is therefore incorrect to classify this as an EBS, Docker, cgroup, or
literal disk-capacity failure.

## Interim conclusion after the baseline probe

The native-user-namespace rootful-DinD design is **not viable on the tested
stock EKS 1.36 Auto Mode/Bottlerocket configuration** without node
preparation.

The immediate blocker is:

```text
user.max_user_namespaces = 0
```

The documented Auto Mode NodeClass interface used by the experiment does not
expose this host sysctl or a way to register the MNG control experiment's
`cgroup_writable` runtime handler. The continuation below tests a privileged
DaemonSet as a separate node-preparation mechanism and then tests writable
cgroup delegation independently.

## Startup-tainted node-preparation continuation

The cluster's Karpenter `NodePool` CRD supports `startupTaints`. A second
NodePool, `userns-dind-auto-prepped`, was created with:

```text
experiment.coder.com/userns-dind-auto=true:NoSchedule
experiment.coder.com/userns-prep=required:NoSchedule (startup taint)
```

The pending workspace caused Auto Mode to provision node
`i-0c1011a1ef58147da`. Karpenter ignored the startup taint for provisioning,
but the scheduler kept the workspace unscheduled because it did not tolerate
that taint. A tolerating node-preparation DaemonSet scheduled first.

### Non-privileged host-sysctl attempts: fail

Kubernetes rejected `hostUsers: true` combined with
`procMount: Unmasked`, so the DaemonSet instead mounted exactly
`/proc/sys/user/max_user_namespaces` from the host at
`/host-sysctl/max_user_namespaces`.

All of the following variants set `privileged: false` and failed to write the
sysctl with `permission denied`:

1. host UID 0, default Auto Mode SELinux label, all capabilities dropped;
2. Bottlerocket `super_t` with the automatically assigned MCS categories;
3. `super_t:s0`, with the MCS categories removed;
4. `super_t:s0` plus effective and bounding `CAP_SYS_ADMIN`.

The last variant verified:

```text
SELinux: system_u:system_r:super_t:s0
CapEff:  0000000000200000
mount:   proc ... rw
file:    -rw-r--r-- 0:0
```

Thus neither host root, an exact writable hostPath, Bottlerocket's broad
SELinux type, nor `CAP_SYS_ADMIN` was sufficient on this Auto Mode node.

### Privileged control and startup ordering: pass

Changing the same diagnostic container to `privileged: true` allowed it to
set:

```text
user.max_user_namespaces=31073
```

The actual Go preparation program then verified the value, added
`experiment.coder.com/userns-ready=true`, and removed only the startup taint.
The permanent experiment isolation taint remained.

Workspace events showed repeated `FailedScheduling` due to untolerated
taints for approximately 20 minutes while the preparation variants were being
debugged. Only after the preparation program removed the startup taint did
the scheduler bind the workspace to the node. This validates the ordering
property; the 20-minute duration was experimental troubleshooting time, not
an expected bootstrap latency.

### Native user namespace and EBS after preparation: pass

After preparation, the previously blocked `hostUsers: false` workspace
started with:

```text
UID map: 0 -> 2788229120, length 65536
GID map: 0 -> 2788229120, length 65536
```

It mounted the existing Auto Mode EBS/ext4 PVC and successfully wrote and
read `/workspace/probe-prepped.txt`. This proves that the volume works in the
user-namespaced Pod; the experiment did not directly inspect the mount to
prove which idmapped-mount implementation details the runtime used.

The updated conclusion is therefore:

- Karpenter startup taints can reliably order node preparation before Coder
  workspaces on EKS Auto Mode;
- among the tested DaemonSet security contexts, only `privileged: true`
  modified `user.max_user_namespaces`;
- native Kubernetes user namespaces and an Auto Mode EBS workspace volume
  worked after that preparation;
- the default Auto Mode runtime's writable cgroup delegation was tested next.

### Default Auto Mode cgroup delegation: fail

The prepped `hostUsers: false` Pod started successfully with all capabilities
inside its user namespace, unmasked proc, and unconfined seccomp. Its cgroup
namespace root was a domain cgroup, but containerd mounted cgroup v2
read-only:

```text
cgroup2 (ro,seclabel,nosuid,nodev,noexec,relatime,nsdelegate,...)
/sys/fs/cgroup owner: 65534:65534
mkdir: Read-only file system
cgroup-writable=no
```

Available controllers were visible, but no writable subtree was delegated.
This is the same functional boundary that the MNG experiment solved with a
custom containerd handler configured with `cgroup_writable = true`.

Therefore the default non-privileged Auto Mode runtime cannot support the
proven rootful-DinD design.

### User-namespaced privileged cgroup control: fail

A final control retained `hostUsers: false` but set the workspace container
to `privileged: true`. The container had every capability in its Pod user
namespace and ran as `control_t`, but it still received no delegated cgroup:

```text
UID map: 0 -> 2000093184, length 65536
cgroup membership: /kubepods.slice/.../cri-containerd-....scope
cgroup2 mount: rw
/sys/fs/cgroup owner: 65534:65534
mkdir: Permission denied
remount: Permission denied
cgroup-writable-initially=no
cgroup-writable-after-remount=no
```

Unlike the non-privileged probe's read-only private cgroup view, the
privileged control appeared to see an `rw` mount of the broader host
hierarchy: its membership remained the full `/kubepods.slice/...` path rather
than `/`, and `cgroup.type` was absent at the mount root. That did not grant
the Pod user namespace ownership or delegation over it. This was a diagnostic
control only; privileged Coder workspaces are not the desired design, and
even that control did not remove the cgroup blocker.

The Auto Mode rootful-DinD branch therefore stops here. The tested runtime
needs an equivalent of containerd's `cgroup_writable = true` delegation, and
neither tested Pod security-context shape provided it.

## Final conclusion and operational implications

A privileged, host-user DaemonSet successfully raised the node-global
`user.max_user_namespaces` value. A provisioning-time startup taint reliably
kept the workspace off the node until that preparation completed. The
prepared user-namespaced workspace then started and used its EBS volume.

The overall rootful-DinD design was nevertheless blocked on the tested Auto
Mode runtime. The non-privileged workspace received a read-only cgroup
hierarchy, and even the user-namespaced privileged control received no
writable cgroup delegation. Because this prerequisite failed, dockerd and
BuildKit were not run on Auto Mode; doing so would not have changed the
cgroup result.

The node-preparation portion is technically viable and replacement-safe:

- AWS recommends DaemonSets when Auto Mode users need custom host-level
  tooling because AMI customization is unavailable; the documentation found
  does not say that a DaemonSet makes the cluster unsupported. It does not,
  however, specifically document this privileged proc-sysctl mutation, so
  that exact dependency should be confirmed with AWS if it becomes a
  production requirement;
- it requires a provisioning-time startup taint and a node-patching agent;
- every replacement node must repeat preparation before workspaces can use
  it; the NodePool startup taint and DaemonSet handle this ordering as long as
  the DaemonSet image, Kubernetes API, and node-patching permissions remain
  available.

These are operational characteristics, not reasons the approach fails. The
single demonstrated blocker to an operable rootful-DinD solution is writable
cgroup delegation: both cgroup controls failed, and the documented Auto Mode
configuration used here exposes no equivalent of the MNG experiment's
`cgroup_writable = true` handler.

The mutation and subsequent workspace test were performed only on the
disposable experiment NodePool.

## Evidence files and manifests

- `cluster.yaml`
- `nodepool.yaml`
- `storage-and-pvc.yaml`
- `userns-volume-probe.yaml`
- `node-sysctl-probe.yaml`
- `nodepool-userns-prepped.yaml`
- `node-userns-prep.yaml`
- `node-userns-prep-super-t-patch.yaml`
- `node-userns-prep-super-t-s0-patch.yaml`
- `node-userns-prep-sys-admin-patch.yaml`
- `node-userns-prep-privileged-control-patch.yaml`
- `node-userns-prep-debug-patch.yaml`
- `userns-volume-probe-prepped.yaml`
- `cgroup-probe-prepped.yaml`
- `cgroup-probe-prepped-userns-privileged.yaml`
- `test4-node-userns-prep.log`
- `test4-userns-volume-prepped.log`
- `test4-prepped-node.yaml`
- `test4-userns-volume-prepped.describe.txt`
- `test5-cgroup-nonprivileged.log`
- `test5-cgroup-userns-privileged.log`
- `runbook.md`

## References

- [Kubernetes user namespaces](https://kubernetes.io/docs/concepts/workloads/pods/user-namespaces/)
- [Linux namespace limits](https://man7.org/linux/man-pages/man7/namespaces.7.html)
- [Linux `unshare(2)` errors](https://man7.org/linux/man-pages/man2/unshare.2.html)
- [EKS Auto Mode managed instances](https://docs.aws.amazon.com/eks/latest/userguide/automode-learn-instances.html)
- [EKS Auto Mode NodeClass](https://docs.aws.amazon.com/eks/latest/userguide/create-node-class.html)
- [EKS Auto Mode EBS StorageClass](https://docs.aws.amazon.com/eks/latest/userguide/create-storage-class.html)
- [Kubernetes writable-cgroups enhancement](https://github.com/kubernetes/enhancements/issues/5474)
