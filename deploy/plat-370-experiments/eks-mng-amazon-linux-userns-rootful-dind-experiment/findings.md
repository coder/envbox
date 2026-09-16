# Findings: native Kubernetes user namespaces with rootful DinD

Experiment run: 2026-08-06

Record last updated: 2026-08-08

## Question tested

Can a Coder workspace run a normal **rootful** Docker daemon and BuildKit in a
native Kubernetes user-namespace Pod, without Envbox or Sysbox, while keeping
Pod UID 0 mapped to an unprivileged host UID range?

This was not a rootless-Docker test. The ultimately successful shape was:

```text
EKS AL2023 node
  └─ containerd RuntimeClass: stock runc + cgroup_writable = true
       └─ Pod: hostUsers: false, privileged: false
            ├─ workspace processes in /workspace-processes cgroup
            └─ rootful dockerd managing sibling /docker cgroup hierarchy
                 └─ ordinary Docker containers / BuildKit workers
```

The original fully successful Pod used `capabilities.add: ["ALL"]`,
`procMount: Unmasked`, an unconfined seccomp profile, and
`allowPrivilegeEscalation: true`. The capability-minimization follow-up below
shows that the runtime's default capability set is insufficient, while adding
only `SYS_ADMIN` and `NET_ADMIN` produces a full workload pass. `ALL` is
therefore not required. Those powers were inside the Pod's user namespace:
container UID 0 mapped to a nonzero host UID range.
The resulting workspace was effectively privileged over resources owned by
that user namespace, but it was neither a Kubernetes `privileged: true`
container nor privileged in the host's initial user namespace. Consequently,
`privileged: false` here must not be read as the security posture of a
conventionally restricted application Pod.

## Environment

- EKS Kubernetes `v1.36.2-eks-254016e` in `us-east-2`.
- Amazon Linux 2023 `m6i.large` managed node-group nodes.
- Node kernel: `6.18.38-76.139.amzn2023.x86_64`.
- Node containerd: `2.2.5+unknown`.
- EBS CSI driver, with an experiment-specific `gp3-csi` StorageClass using
  `ebs.csi.aws.com` and `WaitForFirstConsumer`.
- Docker test image: `docker:27-dind`, which resolved to Docker Engine
  `27.5.1`; the replay manifest now pins `docker:27.5.1-dind`.
- Final Docker data root: an EBS/ext4 PVC.

The MNG was selected as the debugging-friendly baseline before considering
EKS Auto Mode/Bottlerocket. It permits explicit node bootstrap and containerd
configuration.

## Baseline results on the stock runtime

### Native user namespace with an EBS PVC: pass

`userns-volume-probe.yaml` ran with `hostUsers: false`, bound the EBS PVC, and
wrote and read `/workspace/probe.txt` successfully:

```text
uid=0(root)
/proc/self/uid_map:
         0 3130523648      65536
```

This proved both non-host UID mapping and compatibility with the CSI-mounted
EBS/ext4 workspace volume.

### Namespaced privileged probe: pass

The initial capability probe used `privileged: true` and
`procMount: Unmasked`. It retained a non-host UID mapping and successfully
created a private tmpfs mount. This established that the requested kernel
operations were available inside the user namespace, but the final DinD Pod
did not need Kubernetes `privileged: true`.

### Rootful Docker on the stock runtime: fail

Dockerd started and initialized `overlay2` on the PVC, and image pulling
worked. Every attempt to start a child container failed with:

```text
unable to apply cgroup configuration:
mkdir /sys/fs/cgroup/docker: permission denied
```

A focused probe confirmed that the stock runtime exposed no cgroup directory
writable by the Pod. Therefore `hostUsers: false` alone was insufficient for
rootful DinD.

## Cgroup-writable RuntimeClass follow-up

The follow-up added a second AL2023 MNG. Its containerd configuration
registered a named handler using the stock `io.containerd.runc.v2` runtime:

```toml
[plugins.'io.containerd.cri.v1.runtime'.containerd.runtimes.runc-cgroup-writable]
  runtime_type = 'io.containerd.runc.v2'
  cgroup_writable = true
```

`runc-cgroup-writable` is a local RuntimeClass/handler name, not a custom runc
binary.

### Writable-cgroup probe: pass

A non-privileged, user-namespaced Pod scheduled through this RuntimeClass and
created a child cgroup successfully:

```text
/proc/self/uid_map:
         0 2088894464      65536
cgroup on /sys/fs/cgroup type cgroup2 (rw,...,nsdelegate,...)
cgroup-writable-probe-ok
```

This fixed the original permission-denied failure.

### First rootful-Docker attempt: partial pass

Without preparing the cgroup topology, the following worked:

- dockerd startup;
- `overlay2` on the EBS/ext4 PVC;
- image pulls and ordinary `docker run`;
- BuildKit `RUN` steps;
- Docker bridge networking between nested containers.

Only a container using `--memory=64m --pids-limit=64` failed. The hierarchy
showed:

```text
/sys/fs/cgroup:        domain threaded
/sys/fs/cgroup/docker: threaded
```

PID 1, dockerd, and containerd occupied the delegated root while threaded
controllers were enabled. This forced a threaded topology in which Docker
could not apply the domain `memory` controller.

### Domain-cgroup topology: full workload pass

Before starting dockerd, the final entrypoint:

1. created `/sys/fs/cgroup/workspace-processes`;
2. moved PID 1 and the workspace processes into it;
3. left the delegated cgroup root empty;
4. enabled `cpuset cpu io memory pids` in the root's
   `cgroup.subtree_control`;
5. started dockerd in `workspace-processes`, with Docker children under the
   sibling `/docker` hierarchy.

The recorded result was:

```text
/proc/self/uid_map:
         0 1990918144      65536
root cgroup type: domain
root cgroup processes: <empty>
root subtree controllers: cpuset cpu io memory pids
workspace cgroup type: domain
Docker storage driver: overlay2
BuildKit result: buildkit-ok
resource-limited container launch: pass
```

Image pull, ordinary nested execution, BuildKit, bridge networking, and a
nested container configured with memory and PID limits all completed.

### Capability-minimization follow-up: two added capabilities pass

On 2026-08-08, the cluster and cgroup-writable MNG were recreated and the
writable-cgroup control probe passed again. The final DinD manifest was then
rerun unchanged except that `capabilities.add: ["ALL"]` was removed. This
left the container with the runtime's default capability set:

```text
CapPrm: 00000000a80425fb
CapEff: 00000000a80425fb
CapBnd: 00000000a80425fb
```

Dockerd did not become ready. Its log identified two capability-sensitive
failures:

```text
failed to mount overlay: operation not permitted
failed to create NAT chain DOCKER: iptables ... Permission denied
```

The earlier root-propagation setup also reported `operation not permitted`.
These results show that the default capability set is insufficient for this
rootful-DinD configuration. They point to `CAP_SYS_ADMIN` for mount and
overlay operations and `CAP_NET_ADMIN` for iptables/NAT setup. They do **not**
show that every capability in `ALL` is necessary.

A second variant retained the runtime's default capability set and added only
`SYS_ADMIN` and `NET_ADMIN`:

```yaml
capabilities:
  add: ["SYS_ADMIN", "NET_ADMIN"]
```

Its effective capability mask was:

```text
CapPrm: 00000000a82435fb
CapEff: 00000000a82435fb
CapBnd: 00000000a82435fb
```

The complete embedded suite passed: dockerd became ready with `overlay2`, an
ordinary child container ran, BuildKit completed a build, bridge networking
and HTTP worked, memory and PID limits were accepted, Compose service DNS and
HTTP worked, outbound networking worked, and the published port was reachable
through the workspace loopback address.

The companion peer test was then scheduled on the original MNG node, distinct
from the workspace's cgroup-writable MNG node. It reached the Compose-published
server through both the Kubernetes ClusterIP Service and the workspace Pod IP:

```text
cross-node-clusterip-service-ok
cross-node-direct-pod-ip-ok
compose-cross-node-network-tests-ok
```

This disproves `ALL` as a requirement. `SYS_ADMIN` plus `NET_ADMIN` is the
smallest **tested** successful addition set. The experiment has not yet rerun
the suite with either capability individually removed, so it does not by
itself formally prove that both are independently necessary. The observed
mount/overlay and iptables failures strongly explain why each is present.

The workspace container also explicitly sets
`allowPrivilegeEscalation: true`. This is not currently an independent setting
that can simply be tightened while retaining the tested capability set:
Kubernetes treats privilege escalation as enabled whenever a container has
`CAP_SYS_ADMIN`. In this design that authority remains scoped by
`hostUsers: false` to the Pod's user namespace, but it still removes the
`no_new_privs` defense-in-depth boundary inside the workspace.

### `/proc`-masking follow-up: Kubernetes default fails

A third variant retained the successful `SYS_ADMIN` and `NET_ADMIN` additions,
unconfined seccomp, and writable cgroups, but removed
`procMount: Unmasked`. Kubernetes consequently restored its default protected
`/proc` submounts, including a read-only `/proc/sys` and masked sensitive files
such as `/proc/kcore` and `/proc/keys`.

Dockerd itself started, initialized `overlay2`, and answered `docker info`.
The first fresh nested `docker run`, however, failed while Docker configured
the container's veth interface:

```text
failed to add interface ... to sandbox:
failed to configure ipv6: failed to disable IPv6 on container's interface
eth0: unknown
```

For the tested Docker 27.5.1 bridge-network configuration, default `/proc`
masking is therefore insufficient: Docker needs a writable namespaced
`/proc/sys` path during nested-container network setup. This result does not
prove that exposing every path covered by `procMount: Unmasked` is inherently
necessary. A differently configured Docker network or a future mechanism for
narrower `/proc` exposure may reduce this requirement.

### Seccomp follow-up: `RuntimeDefault` fails

A fourth variant restored `procMount: Unmasked` and retained the successful
`SYS_ADMIN` and `NET_ADMIN` additions and writable cgroups, but replaced the
outer workspace container's `Unconfined` seccomp profile with
`RuntimeDefault`. `/proc/self/status` confirmed one active seccomp filter:

```text
Seccomp: 2
Seccomp_filters: 1
```

Dockerd started, initialized `overlay2`, and answered `docker info`. The first
fresh nested `docker run` failed during nested runc initialization:

```text
unable to join session keyring:
unable to create session key: operation not permitted
```

The tested runtime-default profile is therefore insufficient for this nested
runc path. This does not prove that the outer workspace must be fully
unconfined. A tailored seccomp profile that permits the required keyring
syscalls, or a validated configuration that tells nested runc not to create a
new session keyring, may retain most default filtering while allowing DinD.

This topology is not unique to the native MNG experiment. Current Envbox and
Sysbox solve the same cgroup-v2 no-internal-process constraint at two levels:

- Envbox's outer-dockerd wrapper (`cli/wrap_dockerd.sh`) creates an `/init`
  leaf, moves processes out of the visible cgroup root, and enables its
  controllers before starting the outer dockerd. This keeps inner-container
  cgroups beneath the Envbox Pod's host cgroup tree.
- Sysbox-runc creates an `init.scope` leaf for the system container, places
  its init and exec processes there, and delegates ownership of the cgroup-v2
  control files so inner systemd or Docker can create domain sub-cgroups.

The native wrapper's `workspace-processes` leaf and sibling `/docker`
hierarchy explicitly reproduce the latter delegation pattern using stock runc
and containerd's `cgroup_writable = true` handler. The wrapper is therefore an
explicit replacement for behavior that Sysbox normally supplies invisibly,
not an unrelated workaround.

### Docker Compose networking follow-up: pass

A targeted Docker Compose test created a user-defined bridge network with an
`nginx:1.27-alpine` server and an `alpine:3.21` client. It demonstrated:

- Compose network creation and attachment of both service containers;
- Docker embedded DNS and bare service-name resolution through libc;
- HTTP from the client to `http://server`;
- outbound HTTP from the nested client to the internet;
- a nested server published as `0.0.0.0:18080->80/tcp`;
- access to that published port from the workspace through
  `127.0.0.1:18080`;
- access from another Kubernetes Pod through the workspace Pod IP; and
- access through a Kubernetes ClusterIP Service targeting the published port.

The external peer check first passed on the workspace node and then passed
from the original MNG node. The cross-node probe reached both the workspace
Pod IP (`192.168.83.149:18080`) and the ClusterIP Service, demonstrating that
Docker's nested bridge/NAT and port-publishing rules interoperated with EKS
Pod routing and Service forwarding across nodes.

One diagnostic nuance was observed. BusyBox `nslookup server` tried the
Kubernetes search domains inherited by the nested container with `ndots:5`
and returned failure, while `nslookup server.`, `getent hosts server`, and
HTTP to the bare name `server` all resolved the Compose service correctly.
This did not prevent ordinary libc-based application resolution, but clients
with unusual raw-DNS/search-list behavior may require separate validation.

The successful
[`rootful-dind.yaml`](cgroup-writable-runtime/rootful-dind.yaml) replay
manifest now automates the Compose service-name, HTTP, outbound-network, and
workspace-loopback checks and records explicit completion artifacts. The
companion
[`compose-network-peer.yaml`](cgroup-writable-runtime/compose-network-peer.yaml)
declaratively creates the ClusterIP and headless Services and pins a restricted
peer Pod to the original MNG. The peer resolves the headless Service to the
workspace Pod IP, accesses that IP directly, and separately accesses the
ClusterIP Service. The Docker patch release and observed Alpine and Nginx
digests are pinned for repeatability.

These manifests encode checks that passed interactively during the recorded
experiment. Their newly combined automated orchestration has not yet itself
been rerun; a future replay must still verify the completion files, peer log,
and distinct workspace/peer node placement before treating the manifests as a
fresh pass.

## Interpretation

Native Kubernetes user namespaces can support rootful DinD on this EKS 1.36
AL2023 MNG without a host-privileged workspace Pod, provided that all of the
following are supplied:

1. `hostUsers: false` and the broad in-user-namespace security context needed
   by dockerd;
2. a containerd RuntimeClass with `cgroup_writable = true`;
3. a startup wrapper that constructs a valid delegated domain-cgroup
   topology before starting dockerd;
4. compatible writable storage; EBS/ext4 with `overlay2` worked here.

The stock EKS runtime remains insufficient. The positive result depends on
purpose-built node/runtime configuration and is currently demonstrated only
on a configurable managed node group.

### AMI compatibility boundary

The positive result was demonstrated on the AWS EKS-optimized Amazon Linux
2023 AMI. That AMI runs `nodeadm` during boot, and `nodeadm` supports merging
additional inline containerd TOML from a `NodeConfig`. The experiment used
that supported bootstrap path to register the handler; it did not modify or
rebuild the AMI itself.

This result does not establish compatibility with every custom, certified, or
hardened AMI. A candidate AMI must preserve the `nodeadm`/`NodeConfig`
bootstrap path, permit the containerd override, provide a containerd version
that supports `cgroup_writable`, and use the matching containerd configuration
schema. In particular, containerd 1.x and 2.x use different CRI plugin paths.
AMI hardening or compliance policy may also prohibit writable delegated
cgroups even when the image can technically accept the configuration.

Therefore the current compatibility boundary is:

- AWS EKS-optimized AL2023 MNG: compatible and proven by this experiment;
- custom AL2023 AMI derived from it: plausible only if the required bootstrap
  and runtime behavior are preserved, and must be tested;
- arbitrary certified or hardened AMI: not guaranteed and requires vendor or
  compliance validation;
- EKS Auto Mode Bottlerocket: this AL2023 bootstrap mechanism is unavailable.

### Storage compatibility boundary

The MNG stack supports idmapped mounts; the `hostUsers: false` volume probe
successfully mounted and wrote to an EBS/ext4 PVC. Ext4 therefore provides a
proven storage path for the normal Coder shape of one workspace Pod using one
RWO persistent volume.

NFS volumes are not supported for Kubernetes user-namespace Pods. Kubernetes
1.36 explicitly documents that the Linux NFS client does not support idmapped
mounts, which these Pods require for every filesystem used by a Pod volume.
This also excludes standard EFS CSI volumes because EFS is mounted through
NFS. See the upstream
[user-namespace filesystem requirements](https://kubernetes.io/docs/concepts/workloads/pods/user-namespaces/#filesystem-support).

This is a meaningful compatibility restriction, but not a general blocker for
an EBS-backed Envbox replacement. It becomes blocking for templates that
require NFS/EFS semantics such as RWX storage, concurrently shared home
directories or datasets, or storage without EBS availability-zone affinity.

### Namespace and nested-networking boundaries

Kubernetes disallows combining `hostUsers: false` with `hostNetwork: true`,
`hostPID: true`, or `hostIPC: true`. This is a native user-namespace
restriction and therefore applies to the MNG design. It is unlikely to block
an ordinary Coder workspace, which normally uses Pod networking and isolated
PID and IPC namespaces, but it excludes specialized workspaces that require
direct host networking or host process/IPC inspection. See the upstream
[user-namespace limitations](https://kubernetes.io/docs/concepts/workloads/pods/user-namespaces/#limitations).

The claim that nested networking necessarily uses userspace NAT is not true
for the rootful-Docker design tested here. Dockerd can use Linux bridges, veth
interfaces, and kernel iptables/nftables NAT within the Pod's network
namespace using its namespaced `CAP_NET_ADMIN`; traffic then passes through
the normal Pod CNI and node/VPC networking. Userspace networking such as
`slirp4netns` is principally associated with rootless Docker. See Docker's
[packet-filtering and firewall documentation](https://docs.docker.com/engine/network/packet-filtering-firewalls/).

The experiment and Compose follow-up proved nested-container outbound
connectivity, Compose service-name resolution through libc, published-port
reachability from the workspace, and same-node and cross-node reachability
through both the workspace Pod IP and a ClusterIP Service. Still untested are
CNI NetworkPolicy behavior, large-packet/MTU correctness, IPv6, and external
NodePort, LoadBalancer, or Ingress exposure.

### Security comparison with Envbox/Sysbox

This approach demonstrated the same fundamental user-namespace property as
the Envbox inner container: workspace UID 0 maps to an unprivileged host UID,
and the user-controlled workspace does not run as a host-privileged container.
It is therefore reasonable to describe the two approaches as pursuing the
same core isolation objective.

The complete security postures are not yet proven equivalent. The native
approach removes Envbox's privileged outer container and the Sysbox manager,
filesystem service, and custom runtime from each workspace's trusted stack.
Kubernetes also assigned a distinct high host-UID range to each tested Pod,
rather than using Envbox's fixed `100000` user-namespace offset. These may be
security advantages.

Conversely, the successful native Pod needs broad authority inside its user
namespace: the runtime's default capabilities plus the tested additions
`CAP_SYS_ADMIN` and `CAP_NET_ADMIN`, an unmasked `/proc`, an unconfined seccomp
profile, and a writable delegated cgroup hierarchy. The native approach also
lacks Sysbox-specific virtualization and mediation of system-container
behavior. Those differences must be evaluated rather than assumed equivalent.

More precisely, the successful workspace was effectively privileged inside
its own sandbox. It could administer the Pod's mounts, network namespace,
processes, delegated cgroups, nested containers, PVC contents, credentials,
and reachable network resources. This broad authority is expected for a
Docker-capable developer workspace, where the developer is intentionally
allowed complete control inside the workspace. The relevant security
requirement is therefore containment: that authority must not extend to the
node, other workspaces or their storage, cluster-wide credentials, or network
resources the workspace is not authorized to reach.

It was not effectively host-privileged. `hostUsers: false` mapped UID 0 to an
unprivileged high host UID and scoped namespaced capabilities such as
`CAP_SYS_ADMIN` and `CAP_NET_ADMIN` to resources owned by the Pod's user
namespace; capabilities such as `CAP_SYS_MODULE` cannot affect the host from
that namespace. The manifest also did not automatically grant host UID 0,
host namespaces, arbitrary host mounts, or unrestricted host-device access.
Those are meaningful differences from a Kubernetes `privileged: true`
container. See the upstream documentation on
[user-namespace capability boundaries](https://kubernetes.io/docs/concepts/workloads/pods/user-namespaces/)
and
[privileged-container behavior](https://kubernetes.io/docs/concepts/security/linux-kernel-security-constraints/#privileged-containers).

The remaining risk is still material because all containers share the node's
kernel. An unconfined seccomp profile permits the full syscall surface,
unmasked `/proc` exposes interfaces normally hidden by the runtime, and
namespaced `CAP_SYS_ADMIN` and `CAP_NET_ADMIN` plus
`allowPrivilegeEscalation: true` remove substantial defense in depth inside
the namespace. A kernel or user-namespace vulnerability could cross the
intended boundary. This design therefore relies heavily on the Linux user
namespace as its primary host-security boundary: it is meaningfully safer than
host-privileged DinD, but it is not equivalent to a conventional restricted
Pod and still requires a focused security review.

The supported conclusion is therefore that the native MNG design reproduces
Envbox's fundamental non-host-root workspace boundary and may have a smaller
trusted stack, but full security equivalence requires focused escape,
cross-workspace, `/proc`, cgroup, device, mount, networking, and kernel attack-
surface testing.

In a separate
[EKS Auto Mode/Bottlerocket experiment](../eks-auto-mode-bottlerocket-userns-rootful-dind-experiment/findings.md),
the first `hostUsers: false` probe failed because the AWS-managed Bottlerocket
node had `user.max_user_namespaces = 0`. A privileged node-preparation
DaemonSet, ordered with a NodePool startup taint, successfully raised that
sysctl and allowed a `hostUsers: false` Pod to use an EBS PVC. The subsequent cgroup
probes nevertheless found no writable delegated hierarchy, including in the
user-namespaced privileged control. Auto Mode's supported NodeClass interface
still exposes no equivalent of the custom `cgroup_writable = true` containerd
handler used by this successful MNG experiment.

## Decision and remaining validation

This approach is now a technically credible Envbox/Sysbox alternative for
Coder workspaces on configurable EKS MNGs. It is not yet a production-readiness
or security-equivalence result.

Before recommending it, test at least:

1. actual enforcement of memory, CPU, PID, and IO limits under load, rather
   than only successful creation with limits;
2. multiple concurrent workspaces on one node, including resource-exhaustion
   and cross-Pod isolation attempts;
3. the Coder agent and representative workspace images, broader Compose
   configurations, Testcontainers, and devcontainer workflows;
4. Pod restart, node reboot, autoscaling, eviction, PVC reattachment, and
   cleanup behavior;
5. admission-policy requirements and whether dedicating/gating the custom
   RuntimeClass is operationally acceptable;
6. perform a focused security review of the smallest tested working profile:
   the runtime-default capabilities plus namespaced `SYS_ADMIN` and
   `NET_ADMIN`, effective `allowPrivilegeEscalation: true`, unmasked `/proc`,
   unconfined seccomp, writable cgroups, and nested networking. The Pod user
   namespace limits the authority of this profile but does not eliminate its
   shared-kernel attack surface;
7. if a customer policy requires further minimization, test `SYS_ADMIN` and
   `NET_ADMIN` individually and investigate whether a different Docker network
   configuration or runtime mechanism can avoid fully unmasking `/proc`.
   Also test whether a tailored seccomp profile or a no-new-keyring runtime
   configuration can replace `Unconfined`. These are hardening opportunities,
   not unresolved functional pass criteria;
8. monitor for a future supported EKS Auto Mode/Bottlerocket integration. A
   privileged preparation DaemonSet overcame the tested node's initial
   `user.max_user_namespaces = 0`, but writable cgroup delegation remained
   unavailable and Auto Mode exposed no supported equivalent of the MNG's
   custom containerd handler.

The runtime-wide `cgroup_writable` handler also lacks the finer per-Pod policy
and cgroup-depth/descendant controls expected from a future first-class
Kubernetes writable-cgroups API. Until such an API is available and validated,
the custom handler should be limited to dedicated nodes and explicitly
authorized workloads.

## Runbook corrections made during setup

- The experiment node must not carry an untolerated custom `NoSchedule` taint
  that prevents required EKS add-ons from scheduling.
- The test uses an explicit `gp3-csi` StorageClass rather than the legacy
  in-tree `gp2` StorageClass.
- A new WFFC PVC was used for the second MNG to avoid binding the Docker test
  to the first node's availability zone.
- The Docker test does not hide pipeline exit codes.
- A writable cgroup mount alone is insufficient: the workspace entrypoint
  must keep processes out of the delegated root before enabling domain
  controllers.
