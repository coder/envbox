# EKS Auto Mode user-namespace rootful-DinD runbook

Date prepared: 2026-08-06

## Purpose

Determine whether a Coder workspace can run a normal **rootful** Docker daemon
and BuildKit in a native Kubernetes user-namespace Pod on EKS 1.36 Auto Mode,
without Envbox or Sysbox and without a host-privileged workspace Pod.

The successful managed-node-group (MNG) experiment required:

1. `hostUsers: false`;
2. a containerd RuntimeClass configured with `cgroup_writable = true`;
3. a startup wrapper that keeps the delegated cgroup root empty, enables
   domain controllers there, and runs workspace processes and Docker child
   containers in sibling cgroup subtrees;
4. an EBS/ext4 PVC for Docker's `overlay2` data root.

This Auto Mode experiment determines which of those requirements work on an
AWS-managed node and, most importantly, whether the AWS-managed container
runtime provides writable cgroup delegation.

The initial run found that the stock node sets
`user.max_user_namespaces=0`, preventing the first `hostUsers: false` Pod
from being created. Section 8 is a research-only continuation that tests a
non-privileged host-user DaemonSet, protected by a provisioning-time startup
taint, before repeating the native-user-namespace probe.

Do not assume the node operating system from the product name or this
directory name. Record `status.nodeInfo.osImage`, kernel version, and
container-runtime version from the node actually provisioned. AWS selects and
manages the Auto Mode AMI, OS, and runtime.

## Expected result and decision rule

The working hypothesis is:

- native user namespaces work;
- Auto Mode EBS volumes work with a user-namespaced Pod;
- the broad, but user-namespace-confined, Docker security context is admitted;
- the stock Auto Mode runtime does **not** expose a writable delegated cgroup;
- Auto Mode offers no supported node bootstrap or containerd configuration
  mechanism with which to install the MNG experiment's custom handler.

If the cgroup probe cannot create a child under `/sys/fs/cgroup`, record the
experiment as **blocked by missing writable-cgroup runtime support** and do not
run dockerd. Image pulls and dockerd startup would not change that conclusion.

If cgroup creation unexpectedly succeeds, continue to the conditional
rootful-DinD test and use the same domain-cgroup preparation that passed on the
MNG.

## Scope

This runbook tests:

1. EKS 1.36 Auto Mode node provisioning and the actual node OS/runtime;
2. `hostUsers: false` and nonzero host UID/GID mappings;
3. an Auto Mode EBS/ext4 PVC mounted into the user-namespaced Pod;
4. the final MNG experiment's non-privileged security-context shape;
5. private mount operations inside the user namespace;
6. cgroup-v2 mount mode, ownership, controller visibility, and child creation;
7. conditionally, rootful Docker, BuildKit, bridge networking, and nested
   resource limits.

Tests 1 through 7 do not mutate the node or edit the host's containerd
configuration. The research-only continuation in Section 8 does modify one
host-global sysctl. AWS recommends DaemonSets for custom host-level tooling
on Auto Mode and does not state that this makes a cluster unsupported. The
exact privileged proc-sysctl mutation is not specifically documented, so the
experiment tests its technical behavior rather than claiming an explicit AWS
compatibility guarantee for that operation.

## Prerequisites

- `aws`, `kubectl`, and `eksctl` installed and authenticated.
- AWS permissions for disposable EKS, IAM, VPC, EC2, and Auto Mode resources.
- EKS Kubernetes 1.36 and Auto Mode available in the chosen region.
- Permission to create a namespace using the `privileged` Pod Security
  profile. The final Pod still sets `privileged: false`; the namespace profile
  is needed because it requests all capabilities, unmasked `/proc`, and an
  unconfined seccomp profile.

Run from this directory:

```bash
cd ~/sysbox/0/eks-auto-mode-bottlerocket-userns-rootful-dind-experiment

export AWS_REGION="us-east-2"
export CLUSTER_NAME="userns-rootful-dind-auto-136"
export EXPERIMENT_NS="userns-rootful-dind-auto"
```

Confirm the account before creating billable resources:

```bash
aws sts get-caller-identity
aws configure get region || true
aws --version
eksctl version
kubectl version --client -o yaml
```

Keep the successful MNG cluster until this experiment is complete and its
evidence is saved. Use a separate cluster so Auto Mode provisioning and
cleanup do not disturb the control result.

## 1. Create the EKS 1.36 Auto Mode cluster

Save as `cluster.yaml`:

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: userns-rootful-dind-auto-136
  region: us-east-2
  version: "1.36"

autoModeConfig:
  enabled: true
```

Create it and configure the client:

```bash
eksctl create cluster -f cluster.yaml
aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME"

aws eks describe-cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME" \
  --query 'cluster.{name:name,version:version,platformVersion:platformVersion,status:status}' \
  --output yaml | tee cluster-version.yaml

kubectl version -o yaml | tee kubectl-version.yaml
kubectl get nodepools,nodeclasses -o wide | tee auto-mode-pools-and-classes.txt
kubectl get nodes -o wide | tee nodes-before.txt
```

Stop if the server version is not 1.36.

## 2. Create a dedicated Auto Mode NodePool

Use a dedicated label and taint so the experiment cannot accidentally run on
another compute type. Save as `nodepool.yaml`:

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: userns-dind-auto
spec:
  template:
    metadata:
      labels:
        experiment.coder.com/userns-dind-auto: "true"
    spec:
      nodeClassRef:
        group: eks.amazonaws.com
        kind: NodeClass
        name: default
      taints:
        - key: experiment.coder.com/userns-dind-auto
          value: "true"
          effect: NoSchedule
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values: [m6i.large]
        - key: kubernetes.io/os
          operator: In
          values: [linux]
        - key: kubernetes.io/arch
          operator: In
          values: [amd64]
        - key: karpenter.sh/capacity-type
          operator: In
          values: [on-demand]
```

Apply it:

```bash
kubectl apply -f nodepool.yaml
kubectl get nodepool userns-dind-auto -o yaml > nodepool.actual.yaml
```

The NodePool may have no node until the first Pod is created. That is normal.

## 3. Create the Auto Mode StorageClass, namespace, and PVC

Auto Mode block storage uses `ebs.csi.eks.amazonaws.com`, not the standard EBS
CSI add-on's `ebs.csi.aws.com` provisioner.

Save as `storage-and-pvc.yaml`:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: auto-gp3
provisioner: ebs.csi.eks.amazonaws.com
parameters:
  type: gp3
  csi.storage.k8s.io/fstype: ext4
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
---
apiVersion: v1
kind: Namespace
metadata:
  name: userns-rootful-dind-auto
  labels:
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/audit: privileged
    pod-security.kubernetes.io/warn: privileged
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: workspace-data
  namespace: userns-rootful-dind-auto
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: auto-gp3
  resources:
    requests:
      storage: 30Gi
```

Apply it:

```bash
kubectl apply -f storage-and-pvc.yaml
kubectl get storageclass auto-gp3 -o yaml > storageclass.actual.yaml
kubectl -n "$EXPERIMENT_NS" get pvc workspace-data -o yaml > pvc.initial.yaml
```

The WFFC claim should remain `Pending` until Test 1 causes Auto Mode to create
a node. Do not wait for it to bind before creating the Pod.

## 4. Test native user namespaces and the real PVC

Save as `userns-volume-probe.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: userns-volume-probe
  namespace: userns-rootful-dind-auto
spec:
  hostUsers: false
  restartPolicy: Never
  nodeSelector:
    experiment.coder.com/userns-dind-auto: "true"
  tolerations:
    - key: experiment.coder.com/userns-dind-auto
      value: "true"
      effect: NoSchedule
  containers:
    - name: probe
      image: public.ecr.aws/docker/library/alpine:3.21
      resources:
        requests:
          cpu: 250m
          memory: 256Mi
      command: ["/bin/sh", "-c"]
      args:
        - |
          set -eux
          id
          echo '--- uid/gid mapping ---'
          cat /proc/self/uid_map
          cat /proc/self/gid_map
          echo '--- PVC write ---'
          echo "$(date -u +%FT%TZ) auto-mode-userns-volume-probe" > /workspace/probe.txt
          cat /workspace/probe.txt
          sleep 600
      volumeMounts:
        - name: workspace-data
          mountPath: /workspace
  volumes:
    - name: workspace-data
      persistentVolumeClaim:
        claimName: workspace-data
```

Run it. Initial scheduling can take several minutes while Auto Mode launches a
node and provisions EBS:

```bash
kubectl apply -f userns-volume-probe.yaml
kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready pod/userns-volume-probe --timeout=20m
kubectl -n "$EXPERIMENT_NS" logs userns-volume-probe \
  | tee test1-userns-volume.log
kubectl -n "$EXPERIMENT_NS" describe pod userns-volume-probe \
  > test1-userns-volume.describe.txt
kubectl -n "$EXPERIMENT_NS" get pvc workspace-data -o yaml \
  > pvc.bound.yaml

export NODE_NAME="$(kubectl -n "$EXPERIMENT_NS" get pod userns-volume-probe \
  -o jsonpath='{.spec.nodeName}')"

kubectl get node "$NODE_NAME" -o yaml > auto-mode-node.yaml
kubectl get nodeclaims -o yaml > nodeclaims.yaml
kubectl get node "$NODE_NAME" \
  -o jsonpath='{.metadata.name}{"\nOS: "}{.status.nodeInfo.osImage}{"\nkernel: "}{.status.nodeInfo.kernelVersion}{"\nruntime: "}{.status.nodeInfo.containerRuntimeVersion}{"\n"}' \
  | tee auto-mode-node-runtime.txt
```

Required pass conditions:

- UID 0 maps to a nonzero host UID range;
- the PVC becomes `Bound`;
- the Pod writes and reads `/workspace/probe.txt`;
- the Pod is scheduled on the dedicated Auto Mode NodePool.

If the node's `osImage` is not Bottlerocket, retain the result but describe it
as an EKS Auto Mode result for the actual reported OS.

Delete the probe before reusing the RWO PVC:

```bash
kubectl -n "$EXPERIMENT_NS" delete pod userns-volume-probe --wait=true
```

## 5. Test the final non-privileged Docker security context

This matches the successful MNG Pod's outer Kubernetes security context. It
does **not** set `privileged: true`.

Save as `userns-capability-probe.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: userns-capability-probe
  namespace: userns-rootful-dind-auto
spec:
  hostUsers: false
  restartPolicy: Never
  nodeSelector:
    experiment.coder.com/userns-dind-auto: "true"
  tolerations:
    - key: experiment.coder.com/userns-dind-auto
      value: "true"
      effect: NoSchedule
  containers:
    - name: probe
      image: public.ecr.aws/docker/library/alpine:3.21
      securityContext:
        privileged: false
        allowPrivilegeEscalation: true
        procMount: Unmasked
        capabilities:
          add: ["ALL"]
        seccompProfile:
          type: Unconfined
      command: ["/bin/sh", "-c"]
      args:
        - |
          set -eux
          id
          cat /proc/self/uid_map
          grep '^Cap' /proc/self/status
          mkdir /tmp/private-mount
          mount -t tmpfs tmpfs /tmp/private-mount
          mount | grep ' /tmp/private-mount '
          umount /tmp/private-mount
          echo userns-capability-probe-ok
          sleep 600
```

Run and collect it:

```bash
kubectl apply -f userns-capability-probe.yaml
kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready pod/userns-capability-probe --timeout=15m
kubectl -n "$EXPERIMENT_NS" logs userns-capability-probe \
  | tee test2-userns-capability.log
kubectl -n "$EXPERIMENT_NS" describe pod userns-capability-probe \
  > test2-userns-capability.describe.txt
```

The required result is `userns-capability-probe-ok` with a nonzero outer UID
mapping. Record admission, SELinux, seccomp, and mount failures verbatim.

Delete it:

```bash
kubectl -n "$EXPERIMENT_NS" delete pod userns-capability-probe --wait=true
```

## 6. Decisive test: writable cgroup delegation

This uses the default Auto Mode runtime. It does not name the MNG experiment's
custom RuntimeClass because a RuntimeClass object only selects an already
installed handler; it cannot install or configure one on the node.

Save as `cgroup-probe.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: cgroup-probe
  namespace: userns-rootful-dind-auto
spec:
  hostUsers: false
  restartPolicy: Never
  nodeSelector:
    experiment.coder.com/userns-dind-auto: "true"
  tolerations:
    - key: experiment.coder.com/userns-dind-auto
      value: "true"
      effect: NoSchedule
  containers:
    - name: probe
      image: public.ecr.aws/docker/library/alpine:3.21
      securityContext:
        privileged: false
        allowPrivilegeEscalation: true
        procMount: Unmasked
        capabilities:
          add: ["ALL"]
        seccompProfile:
          type: Unconfined
      command: ["/bin/sh", "-c"]
      args:
        - |
          set -ux
          echo '--- identity and user namespace ---'
          id
          cat /proc/self/uid_map
          cat /proc/self/gid_map
          echo '--- cgroup membership and mount ---'
          cat /proc/self/cgroup
          mount | grep -E 'cgroup|/sys/fs/cgroup' || true
          echo '--- cgroup metadata ---'
          ls -ldn /sys/fs/cgroup
          cat /sys/fs/cgroup/cgroup.type 2>&1 || true
          cat /sys/fs/cgroup/cgroup.controllers 2>&1 || true
          cat /sys/fs/cgroup/cgroup.subtree_control 2>&1 || true
          echo '--- child-cgroup creation ---'
          if mkdir /sys/fs/cgroup/userns-dind-write-probe; then
            ls -ldn /sys/fs/cgroup/userns-dind-write-probe
            rmdir /sys/fs/cgroup/userns-dind-write-probe
            echo cgroup-writable=yes
          else
            echo cgroup-writable=no
          fi
          sleep 600
```

Run and collect it:

```bash
kubectl apply -f cgroup-probe.yaml
kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready pod/cgroup-probe --timeout=15m
kubectl -n "$EXPERIMENT_NS" logs cgroup-probe \
  | tee test3-cgroup.log
kubectl -n "$EXPERIMENT_NS" describe pod cgroup-probe \
  > test3-cgroup.describe.txt
```

Interpretation:

- `cgroup-writable=no`: stop. The Auto Mode runtime lacks the required
  delegation, so the proven MNG design cannot be expressed on this node.
- `cgroup-writable=yes`: unexpected positive result; continue below.
- admission or runtime rejection: preserve the full error and classify the
  exact boundary rather than calling it a generic Docker failure.

## 7. Conditional rootful-DinD test

Only perform this section if Test 6 reports `cgroup-writable=yes`.

Create an Auto Mode version of the MNG experiment's final
`rootful-dind.yaml` with these deliberate differences:

- omit `runtimeClassName: runc-cgroup-writable`;
- use the Auto Mode node selector and toleration from the probes;
- use namespace `userns-rootful-dind-auto`;
- use PVC `workspace-data`;
- retain `hostUsers: false` and `privileged: false`;
- retain the `ALL` capabilities, unmasked proc, unconfined seccomp profile,
  and `allowPrivilegeEscalation: true`;
- before starting dockerd, move workspace processes to
  `/sys/fs/cgroup/workspace-processes`, leave the root empty, and enable
  `cpuset cpu io memory pids` in the root's `cgroup.subtree_control`;
- run dockerd with the `cgroupfs` driver and relative cgroup parent `docker`.

Delete `cgroup-probe`, apply that manifest, and require all of the MNG test's
results:

```bash
kubectl -n "$EXPERIMENT_NS" delete pod cgroup-probe --wait=true
kubectl apply -f rootful-dind.yaml
kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready pod/rootful-dind --timeout=15m
kubectl -n "$EXPERIMENT_NS" logs -f rootful-dind
```

Required full-pass evidence:

- root cgroup type remains `domain` and its `cgroup.procs` is empty;
- `cpuset cpu io memory pids` are enabled for children;
- dockerd uses `overlay2` on EBS/ext4;
- ordinary `docker run` works;
- BuildKit `RUN` works;
- bridge networking between nested containers works;
- a child configured with memory and PID limits starts successfully.

Do not interpret mere dockerd startup or image pulling as a pass.

## 8. Research continuation: initialize user namespaces with a startup taint

The baseline experiment found `user.max_user_namespaces=0`. This continuation
tests whether an Auto Mode DaemonSet can raise that host-global sysctl while
still declaring `privileged: false`, and whether a Karpenter startup taint can
prevent a workspace Pod from racing ahead of node preparation.

This is a research experiment, not a supported Auto Mode configuration. The
prep container uses the host user namespace, runs as host UID 0, and exposes
an unmasked `/proc`. It is therefore a trusted node agent despite setting
`privileged: false`. Kubernetes does not permit `procMount: Unmasked` together
with `hostUsers: true`, so the Pod bind-mounts only the host's
`/proc/sys/user/max_user_namespaces` file at
`/host-sysctl/max_user_namespaces`. Its service account can get and patch Node objects;
Kubernetes RBAC cannot restrict those verbs to only the Pod's own node.

### 8.1 Confirm startup-taint support

```bash
kubectl explain nodepool.spec.template.spec.startupTaints
```

The Auto Mode cluster used in this experiment exposes this field. Its schema
explicitly describes startup taints as an initialization-ordering mechanism
normally removed by a tolerating DaemonSet.

### 8.2 Create a fresh, startup-tainted NodePool

Do not retrofit the original experiment node: that would not prove that the
taint exists before the scheduler can use a newly registered node. Apply the
separate `nodepool-userns-prepped.yaml`. It has:

- a unique node label, so this continuation requires a newly provisioned node;
- the experiment's permanent isolation taint;
- `experiment.coder.com/userns-prep=required:NoSchedule` as a
  `startupTaint`.

```bash
kubectl apply -f nodepool-userns-prepped.yaml
kubectl get nodepool userns-dind-auto-prepped -o yaml \
  > nodepool-userns-prepped.actual.yaml
```

It is normal for this NodePool to remain at zero nodes until a matching
workload is pending.

### 8.3 Install the experimental prep DaemonSet

`node-userns-prep.yaml` contains an experiment-only Go program in a ConfigMap
and runs it with the standard Go image. This avoids publishing a custom image
before the permission model is proven. The Pod uses:

```yaml
hostUsers: true
hostNetwork: true
securityContext:
  runAsUser: 0
  privileged: false
  allowPrivilegeEscalation: false
  capabilities:
    drop: ["ALL"]
```

The container receives one writable hostPath file:

```yaml
hostPath:
  path: /proc/sys/user/max_user_namespaces
  type: File
```

This is narrower than exposing an unmasked `/proc`. The Kubernetes API rejects
`hostUsers: true` with `procMount: Unmasked`, so the exact-file hostPath is the
permission boundary being tested.

The program performs these operations in order:

1. reads `user.max_mnt_namespaces` as the node's target namespace limit;
2. writes and verifies `user.max_user_namespaces`;
3. reads its own Node object using the Downward API node name;
4. atomically verifies and removes only the startup taint while adding
   `experiment.coder.com/userns-ready=true`;
5. remains running and verifies the sysctl every 30 seconds.

If writing or verification fails, the process exits and the startup taint is
not removed. The workspace therefore remains blocked.

```bash
kubectl apply -f node-userns-prep.yaml
kubectl -n "$EXPERIMENT_NS" get daemonset node-userns-prep
```

The DaemonSet initially showing zero desired Pods is expected because the new
NodePool still has no node.

### 8.4 Trigger provisioning with the blocked workspace probe

Delete the failed baseline probe so it no longer holds the RWO claim, then
apply the prepped variant. It selects the fresh NodePool and tolerates only
the permanent isolation taint. It deliberately does **not** tolerate the
startup taint.

```bash
kubectl -n "$EXPERIMENT_NS" delete pod userns-volume-probe \
  --ignore-not-found --wait=true
kubectl apply -f userns-volume-probe-prepped.yaml

kubectl -n "$EXPERIMENT_NS" get pods -o wide --watch
```

Karpenter ignores startup taints for provisioning, so the pending workspace
can create the new node. Once the node registers, the startup taint blocks the
workspace, while the tolerating DaemonSet can run and prepare the node.

In another shell, inspect the sequence:

```bash
kubectl get nodes \
  -L experiment.coder.com/userns-dind-auto-prepped,experiment.coder.com/userns-ready
kubectl -n "$EXPERIMENT_NS" get daemonset,pods -o wide
kubectl -n "$EXPERIMENT_NS" logs -l app=node-userns-prep --prefix --tail=100
```

Wait for both the DaemonSet and workspace probe:

```bash
kubectl -n "$EXPERIMENT_NS" rollout status daemonset/node-userns-prep \
  --timeout=15m
kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready pod/userns-volume-probe-prepped --timeout=15m
```

Capture the result:

```bash
export PREPPED_NODE="$(kubectl -n "$EXPERIMENT_NS" \
  get pod userns-volume-probe-prepped -o jsonpath='{.spec.nodeName}')"

kubectl -n "$EXPERIMENT_NS" logs -l app=node-userns-prep --prefix \
  | tee test4-node-userns-prep.log
kubectl -n "$EXPERIMENT_NS" logs userns-volume-probe-prepped \
  | tee test4-userns-volume-prepped.log
kubectl get node "$PREPPED_NODE" -o yaml \
  > test4-prepped-node.yaml
kubectl get node "$PREPPED_NODE" \
  -o jsonpath='{.metadata.name}{"\nlabels: "}{.metadata.labels}{"\ntaints: "}{.spec.taints}{"\n"}' \
  | tee test4-prepped-node-state.txt
```

Required pass conditions:

- the prep program reports a verified nonzero `user.max_user_namespaces` and
  the exact security context required to achieve that is recorded;
- the Node has `experiment.coder.com/userns-ready=true` and no
  `experiment.coder.com/userns-prep` taint;
- the permanent experiment isolation taint remains;
- the workspace starts with a nonidentity UID/GID map and writes the PVC.

Do not manually remove the startup taint if the DaemonSet fails. Preserve its
logs and Pod events; leaving the taint in place is the safety property being
tested.

On the tested Auto Mode node, the default SELinux-labeled Pod reached the
exact hostPath file but failed to open it for writing:

```text
node=i-0c1011a1ef58147da /host-sysctl/max_user_namespaces before=0 target=31073
panic: open /host-sysctl/max_user_namespaces: permission denied
```

This is the expected Auto Mode SELinux boundary: host UID 0, a writable
hostPath declaration, and ordinary file permissions do not make a
non-privileged `container_t` process able to modify host state.

An optional attribution test applies Bottlerocket's `super_t` SELinux type
while retaining `privileged: false`, the default seccomp profile, and an empty
capability set:

```bash
kubectl -n "$EXPERIMENT_NS" patch daemonset node-userns-prep \
  --type=strategic --patch-file node-userns-prep-super-t-patch.yaml

kubectl -n "$EXPERIMENT_NS" rollout status daemonset/node-userns-prep \
  --timeout=15m
kubectl -n "$EXPERIMENT_NS" logs -l app=node-userns-prep --prefix --tail=100
```

`super_t` is not an unprivileged security posture: Bottlerocket documents it
as permitting modification of any host file or directory. A success would
identify SELinux as the remaining write barrier, not establish that this is a
safer production substitute for a privileged node-preparation Pod.

The actual experiment found that `super_t` was insufficient both with the
automatically assigned MCS categories and as `super_t:s0`. Adding effective
`CAP_SYS_ADMIN` also failed even though the exact proc bind mount reported
`rw`. The otherwise identical `privileged: true` control succeeded. The Go
program then verified the sysctl, labeled the node ready, removed the startup
taint, and allowed the workspace to schedule. The workspace showed a nonzero
65536-ID mapping and wrote the Auto Mode EBS PVC.

The next non-privileged cgroup probe started but found cgroup v2 mounted
read-only and reported `cgroup-writable=no`. This blocks the proven MNG
rootful-DinD design under the default Auto Mode runtime. The manifest
`cgroup-probe-prepped-userns-privileged.yaml` is the final control: it keeps
`hostUsers: false` but requests `privileged: true`, then tests both the initial
cgroup mount and an explicit remount attempt.

That final control also failed. It saw an `rw` view of the host cgroup
hierarchy rather than a writable private delegation; the hierarchy was owned
by overflow UID/GID 65534 in the Pod user namespace, child creation returned
`permission denied`, and remounting returned `permission denied`. Do not run
the conditional rootful-DinD test on this Auto Mode runtime.

Passing this section removes only the first observed Auto Mode blocker. It
does not establish cgroup writability. Repeat the capability and cgroup probes
on the prepped NodePool before attempting rootful DinD.

## 9. Findings

Record each boundary independently in `findings.md`:

| Boundary | Result | Evidence |
|---|---|---|
| Actual node OS/kernel/runtime | | `auto-mode-node-runtime.txt` |
| `hostUsers: false` UID mapping | Pass after privileged prep | `test4-userns-volume-prepped.log` |
| Auto Mode EBS idmapped mount/write | Pass after privileged prep | Test 4 and bound PVC |
| Final non-privileged security context | | `test2-userns-capability.log` |
| Private mount operation | | `userns-capability-probe-ok` |
| Writable cgroup delegation | Fail for non-privileged and user-namespaced privileged controls | `test5-cgroup-nonprivileged.log`, `test5-cgroup-userns-privileged.log` |
| Domain-controller topology | | Conditional Test 7 |
| Rootful Docker/BuildKit/networking | | Conditional Test 7 |
| Non-privileged host-user sysctl preparation | Fail | permission denied through `super_t:s0` + `CAP_SYS_ADMIN` |
| Privileged host-user sysctl preparation | Pass | `test4-node-userns-prep.log` |
| Startup-taint ordering | Pass | `test4-prepped-node-state.txt` and Pod events |

The likely useful conclusion is not simply “Docker failed.” It is either:

- Auto Mode's managed runtime lacks a supported writable-cgroup interface on
  EKS 1.36; or
- Auto Mode already supplies sufficient delegation, in which case the full
  workload result must be compared with the MNG result.

## 10. Cleanup

Stop log-following with `Ctrl-C`; that does not delete the Pod.

Before deleting the cluster, delete the namespace/PVC and wait for the PV to
be removed so the dynamically provisioned EBS volume follows its `Delete`
reclaim policy:

```bash
kubectl delete -f node-userns-prep.yaml --ignore-not-found
kubectl delete nodepool userns-dind-auto-prepped --ignore-not-found
kubectl delete namespace "$EXPERIMENT_NS" --wait=true
kubectl delete storageclass auto-gp3 --ignore-not-found
kubectl get pv

eksctl delete cluster --region "$AWS_REGION" --name "$CLUSTER_NAME"
```

Verify in AWS that the cluster and experiment EBS volume are gone. Do not
delete the successful MNG control cluster until its own findings and required
artifacts have been retained.

## References

- [EKS Auto Mode managed-instance restrictions](https://docs.aws.amazon.com/eks/latest/userguide/automode-learn-instances.html)
- [EKS compute-option comparison](https://docs.aws.amazon.com/eks/latest/userguide/eks-compute.html)
- [EKS Auto Mode NodeClass fields](https://docs.aws.amazon.com/eks/latest/userguide/create-node-class.html)
- [EKS Auto Mode EBS StorageClass](https://docs.aws.amazon.com/eks/latest/userguide/create-storage-class.html)
- [Kubernetes writable-cgroups enhancement](https://github.com/kubernetes/enhancements/issues/5474)
- [containerd CRI runtime configuration](https://github.com/containerd/containerd/blob/main/docs/cri/config.md)
- [Linux cgroup v2 delegation and domain/threaded topology](https://docs.kernel.org/admin-guide/cgroup-v2.html)
