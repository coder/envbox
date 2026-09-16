# Direct Sysbox on an EKS Amazon Linux 2023 managed node group

Date prepared: 2026-08-09

## Purpose

Determine whether Coder workspaces that need systemd, rootful Docker,
BuildKit, Compose, and nested containers can run directly under Sysbox on a
standard Amazon EKS managed node group (MNG) using the EKS-optimized Amazon
Linux 2023 (AL2023) AMI.

This experiment tests a small downstream installer adaptation. Upstream
Sysbox 0.7.1 can build and run its static binaries on AL2023, but the
Kubernetes installer still treats AL2023 as unsupported. The experimental
installer adds:

- AL2023 distro recognition;
- use of the generic static Sysbox binaries;
- a minimum AL2023 kernel version of 6.1;
- `dnf` installation of `ca-certificates`, `rsync`, `fuse3`, and `iptables`;
- an AL2023-specific Shiftfs bypass, because AL2023 uses native id-mapped
  mounts and does not provide Ubuntu's Shiftfs module;
- omission of Debian's `kernel.unprivileged_userns_clone` sysctl on AL2023,
  where that knob does not exist and user namespaces are enabled without it;
- registration of `sysbox-runc` under containerd 2.x's
  `io.containerd.cri.v1.runtime` plugin schema, including repair and removal of
  the ignored legacy `io.containerd.grpc.v1.cri` entry.

The intended architecture is:

```text
EKS 1.35 AL2023 managed node
  ├─ containerd (the existing EKS node runtime)
  ├─ sysbox-mgr and sysbox-fs (trusted host services)
  └─ sysbox-runc (containerd RuntimeClass handler)
       └─ Coder workspace Pod
            ├─ hostUsers: false
            ├─ privileged: false
            ├─ systemd
            └─ rootful Docker and child containers
```

Unlike Envbox, this design has no privileged outer container per workspace.
Unlike native userns rootful DinD, Sysbox mediates and virtualizes parts of
`/proc`, `/sys`, and the system-container environment. It does, however,
install trusted software and services on every selected node. The installer
DaemonSet itself is privileged and has broad host mounts.

## Pinned prototype inputs

Record these exact inputs in all results:

- Kubernetes: EKS 1.35.
- Node OS: EKS-optimized AL2023, Linux/amd64.
- Sysbox binaries: released Sysbox 0.7.1.
- Experimental installer source:
  `~/sysbox/sysbox-pkgr` at upstream commit `e78ba6a`, with local AL2023
  changes in:
  - `k8s/scripts/sysbox-deploy-k8s.sh`
  - `k8s/scripts/sysbox-installer-helper.sh`
- Installer image:

  ```text
  849808308023.dkr.ecr.us-east-2.amazonaws.com/envbox-eks-cache-experiment@sha256:fe6b53286b836b932692907921656c154922d62366287429dd5534c5347575f3
  ```

The ECR repository name is inherited from an earlier disposable experiment;
the pinned digest above contains the Sysbox installer, not Envbox. Always use
the digest, not the mutable-looking repository name or tag.

### Kubernetes user-namespace version boundary

Kubernetes 1.36 is **not** the minimum version for this experiment. Pod user
namespaces first appeared as alpha in Kubernetes 1.25, became beta in 1.30,
were enabled by default in 1.33, and became stable in 1.36. EKS 1.35 therefore
uses the enabled-by-default beta implementation of `hostUsers: false`.

The current Sysbox documentation requires Kubernetes 1.30 or later and
containerd 2.0.5 or later for this deployment form. Independently, upstream
Kubernetes requires a kernel with the necessary idmapped-mount support
(practically Linux 6.3 or later), containerd 2.0 or later, and runc 1.2 or
later. The experimental installer's AL2023 kernel check of 6.1 is only its
Sysbox/AL2023 compatibility floor; it does not override the stricter
Kubernetes Pod-user-namespace prerequisites. Verify all of these on the
actual EKS node before installing Sysbox.

## Decision rules

Record every stage as **pass**, **fail**, or **blocked**. Do not weaken the
workspace Pod to make the experiment pass.

The experiment is a functional pass only if all of the following hold:

1. The installer recognizes AL2023, installs its dependencies with `dnf`, and
   deliberately skips Shiftfs.
2. The node remains on the EKS-provided containerd runtime. Installing or
   activating the bundled CRI-O fallback is a failure for this target.
3. `sysbox-mgr`, `sysbox-fs`, and the `sysbox-runc` containerd handler become
   operational, and the node receives `sysbox-runtime=running`.
4. Ordinary non-Sysbox cluster Pods remain healthy through installation.
5. A Pod using `runtimeClassName: sysbox-runc`, `hostUsers: false`, and
   `privileged: false` starts with an EBS-backed PVC.
6. The workspace runs systemd, rootful Docker, BuildKit, ordinary Docker
   networks, Compose, resource-limited child containers, and published ports.
7. Kubernetes Service and direct Pod-IP connectivity work across nodes.
8. Docker state and workspace data persist across workspace Pod replacement.
9. Multiple workspaces remain isolated and cannot see each other's mounts,
   processes, Docker state, or writable cgroup hierarchy.
10. A replacement or newly scaled node installs Sysbox automatically and can
    run a fresh workspace.

The following are stop conditions rather than reasons to broaden privilege:

- the workspace requires `privileged: true`;
- the workspace requires host PID, host IPC, host networking, host devices,
  or arbitrary host paths;
- Sysbox replaces the EKS container runtime with CRI-O;
- the installer leaves the node persistently `NotReady`;
- an AL2023 AMI update cannot preserve or repeat the installation cleanly.

## Scope and non-goals

The first pass uses one disposable EKS 1.35 cluster and one AL2023 MNG node.
Later lifecycle and cross-node phases scale the same MNG to two nodes. This is
not an Auto Mode or Bottlerocket experiment: direct host runtime installation
is incompatible with those managed-node models.

The first functional pass proves feasibility, not production readiness. A
production recommendation additionally requires security review, upgrade and
rollback ownership, compatibility testing against new EKS-optimized AMIs,
resource-exhaustion testing, Coder agent integration, and a support plan for
the downstream installer patch.

## Prerequisites

- `aws`, `eksctl`, `kubectl`, Docker, and standard Unix tools.
- AWS permission to create and delete EKS, VPC, EC2, IAM, EBS, and ECR-related
  resources.
- The installer image digest listed above must remain pullable by the MNG node
  role.
- No production workloads may run on the disposable cluster.
- The local `~/sysbox` checkout and its dirty `sysbox-pkgr` submodule must be
  preserved until the experiment artifacts are documented.

Set the experiment variables:

```bash
cd ~/sysbox/0/eks-mng-amazon-linux-direct-sysbox-experiment

export AWS_REGION="us-east-2"
export CLUSTER_NAME="direct-sysbox-al2023-mng-135"
export NODEGROUP_NAME="direct-sysbox"
export EXPERIMENT_NS="direct-sysbox"
export SYSBOX_INSTALLER_IMAGE="849808308023.dkr.ecr.us-east-2.amazonaws.com/envbox-eks-cache-experiment@sha256:fe6b53286b836b932692907921656c154922d62366287429dd5534c5347575f3"
```

Capture the local source state before provisioning anything:

```bash
git -C ~/sysbox status --short \
  | tee source-superproject-status.txt

git -C ~/sysbox/sysbox-pkgr status --short \
  | tee source-sysbox-pkgr-status.txt

git -C ~/sysbox/sysbox-pkgr diff \
  > source-sysbox-pkgr-al2023.patch

docker image inspect \
  ghcr.io/nestybox/sysbox-deploy-k8s:v0.7.1-0-al2023-poc \
  > installer-local-image-inspect.json

aws ecr describe-images \
  --region "$AWS_REGION" \
  --repository-name envbox-eks-cache-experiment \
  --image-ids imageTag=sysbox-al2023-poc3 \
  > installer-ecr-image.json
```

## Phase 1: create the disposable EKS 1.35 AL2023 cluster

Save the following as `cluster.yaml`:

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: direct-sysbox-al2023-mng-135
  region: us-east-2
  version: "1.35"

autoModeConfig:
  enabled: false

managedNodeGroups:
  - name: direct-sysbox
    amiFamily: AmazonLinux2023
    instanceType: m6i.large
    minSize: 1
    desiredCapacity: 1
    maxSize: 2
    labels:
      experiment.coder.com/direct-sysbox: "true"
      sysbox-install: "yes"
    iam:
      withAddonPolicies:
        ebs: true

addons:
  - name: aws-ebs-csi-driver
```

Create the cluster and explicitly select its context:

```bash
eksctl create cluster -f cluster.yaml \
  |& tee cluster-create.log

aws eks update-kubeconfig \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME"

kubectl config current-context
```

Stop if the current context does not name the new cluster.

Capture the baseline before installing Sysbox:

```bash
aws sts get-caller-identity \
  | tee aws-caller-identity.json

aws eks describe-cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME" \
  --query 'cluster.{name:name,version:version,platformVersion:platformVersion,status:status,endpoint:endpoint}' \
  --output yaml \
  | tee cluster-version.yaml

kubectl version -o yaml \
  | tee kubectl-version.yaml

kubectl get nodes -o wide \
  | tee nodes-before-sysbox.txt

kubectl get nodes \
  -L eks.amazonaws.com/nodegroup,eks.amazonaws.com/ami-id,sysbox-install,sysbox-runtime \
  | tee nodes-labelled-before-sysbox.txt

kubectl get pods -A -o wide \
  | tee pods-before-sysbox.txt

kubectl get storageclass \
  | tee storageclasses-before.txt
```

Confirm from `kubectl get nodes -o yaml` that:

- Kubernetes is 1.35;
- the node is Linux/amd64 and belongs to `direct-sysbox`;
- the kernel is 6.3 or later;
- `containerRuntimeVersion` reports containerd 2.0.5 or later;
- the node has both `experiment.coder.com/direct-sysbox=true` and
  `sysbox-install=yes`.

Also record the node's runc version and confirm it is 1.2 or later. If any
user-namespace prerequisite is missing, record the experiment as blocked on
the EKS node stack rather than removing `hostUsers: false`.

Do not continue if any other node is labeled `sysbox-install=yes`.

## Phase 2: render and validate the digest-pinned installer

Create a local installer manifest from the current upstream manifest while
replacing only its image reference:

```bash
sed \
  "s#registry.nestybox.com/nestybox/sysbox-deploy-k8s:v0.7.0-0#${SYSBOX_INSTALLER_IMAGE}#" \
  ~/sysbox/sysbox-k8s-manifests/sysbox-install.yaml \
  > sysbox-install-al2023.yaml
```

Verify that the rendered manifest contains the expected selector, privileged
installer, RuntimeClass, and digest—and no upstream installer image:

```bash
grep -nE \
  'image:|privileged:|sysbox-install|sysbox-runtime|kind: RuntimeClass|handler:' \
  sysbox-install-al2023.yaml \
  | tee installer-render-summary.txt

if grep -q \
  'registry.nestybox.com/nestybox/sysbox-deploy-k8s' \
  sysbox-install-al2023.yaml; then
  echo 'failed: upstream installer image remains in rendered manifest'
  exit 1
fi

kubectl apply --server-side --dry-run=server \
  -f sysbox-install-al2023.yaml \
  | tee installer-server-dry-run.txt
```

The private ECR image should be pullable by the normal eksctl-created MNG node
role. Do not add a broad static registry credential unless the pull actually
fails and the IAM cause is understood.

## Phase 3: install Sysbox and capture node mutation

Apply the installer:

```bash
kubectl apply -f sysbox-install-al2023.yaml

kubectl -n kube-system get daemonset,pod \
  -l sysbox-install=yes \
  -o wide
```

Watch the installer log directly. The important AL2023 evidence is distro
acceptance, dependency installation through `dnf`, explicit Shiftfs skipping,
containerd configuration, and final node labeling:

```bash
kubectl -n kube-system logs \
  -l sysbox-install=yes \
  -f --prefix \
  | tee installer-live.log
```

The installer intentionally remains running after printing `Done.`. Stop
following the log with Ctrl-C after that message; do not delete the DaemonSet.

Wait for the runtime label:

```bash
deadline=$((SECONDS + 900))

while (( SECONDS < deadline )); do
  runtime_label="$(
    kubectl get nodes \
      -l experiment.coder.com/direct-sysbox=true \
      -o jsonpath='{.items[0].metadata.labels.sysbox-runtime}' \
      2>/dev/null
  )"

  printf '%s sysbox-runtime=%s\n' \
    "$(date -u +%FT%TZ)" "${runtime_label:-absent}"

  [ "$runtime_label" = running ] && break
  sleep 5
done

[ "$runtime_label" = running ]
```

Capture the completed installation:

```bash
kubectl -n kube-system logs \
  -l sysbox-install=yes --prefix \
  > installer-complete.log

kubectl get nodes \
  -L eks.amazonaws.com/nodegroup,sysbox-install,sysbox-runtime \
  | tee nodes-after-sysbox.txt

kubectl get runtimeclass sysbox-runc -o yaml \
  | tee runtimeclass-sysbox-runc.yaml

kubectl get pods -A -o wide \
  | tee pods-after-sysbox.txt
```

Fail this phase if the installer log shows an unsupported distro, empty
artifact path, `apt-get` or `dpkg` execution on AL2023, a Shiftfs build
attempt, or installation of CRI-O.

## Phase 4: prove the node kept EKS containerd

Kubernetes must still report containerd after installation:

```bash
kubectl get nodes \
  -l experiment.coder.com/direct-sysbox=true \
  -o jsonpath='{range .items[*]}{.metadata.name}{" runtime="}{.status.nodeInfo.containerRuntimeVersion}{" kernel="}{.status.nodeInfo.kernelVersion}{" os="}{.status.nodeInfo.osImage}{"\n"}{end}' \
  | tee node-runtime-after-sysbox.txt

if grep -qv 'runtime=containerd://' node-runtime-after-sysbox.txt; then
  echo 'failed: node is no longer using EKS containerd'
  exit 1
fi
```

Use the privileged installer only as a diagnostic window into its target
host. Record service state and the containerd configuration without changing
anything:

```bash
installer_pod="$(
  kubectl -n kube-system get pod \
    -l sysbox-install=yes \
    -o jsonpath='{.items[0].metadata.name}'
)"

kubectl -n kube-system exec "$installer_pod" -- bash -ec '
  echo "=== host OS ==="
  cat /mnt/host/os-release

  echo "=== Sysbox services ==="
  systemctl is-active sysbox sysbox-mgr sysbox-fs
  systemctl status --no-pager sysbox sysbox-mgr sysbox-fs

  echo "=== EKS containerd ==="
  systemctl is-active containerd
  grep -n -A12 -B4 "sysbox-runc" /mnt/host/etc/containerd/config.toml

  echo "=== CRI-O must be absent or inactive ==="
  if systemctl is-active --quiet crio; then
    echo "failed: CRI-O is active"
    exit 1
  fi
  echo "passed: CRI-O is not active"
' |& tee host-runtime-evidence.txt
```

Also confirm CoreDNS, VPC CNI, kube-proxy, EBS CSI, and metrics-server remain
healthy. Any node or add-on disruption belongs in the findings even if the
workspace later starts.

## Phase 5: create the workspace namespace and EBS storage

Save as `storageclass.yaml`:

```yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: direct-sysbox-gp3
provisioner: ebs.csi.aws.com
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
parameters:
  type: gp3
  encrypted: "true"
```

Save as `namespace-and-pvc.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: direct-sysbox
  labels:
    pod-security.kubernetes.io/enforce: restricted
    pod-security.kubernetes.io/audit: restricted
    pod-security.kubernetes.io/warn: restricted
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: workspace-docker
  namespace: direct-sysbox
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: direct-sysbox-gp3
  resources:
    requests:
      storage: 30Gi
```

Apply them:

```bash
kubectl apply -f storageclass.yaml
kubectl apply -f namespace-and-pvc.yaml

kubectl -n "$EXPERIMENT_NS" get namespace,pvc -o wide \
  | tee namespace-and-pvc-initial.txt
```

The PVC may remain `Pending` until the first workspace schedules because the
StorageClass uses `WaitForFirstConsumer`.

## Phase 6: direct-Sysbox workspace smoke test

The initial upstream system-container image is tag-based. Record the resolved
image ID from Kubernetes, and replace the tag with a digest before treating
the final experiment as reproducible.

Save as `workspace.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: direct-sysbox-workspace
  namespace: direct-sysbox
  labels:
    app.kubernetes.io/name: direct-sysbox-workspace
spec:
  runtimeClassName: sysbox-runc
  hostUsers: false
  restartPolicy: Never
  nodeSelector:
    experiment.coder.com/direct-sysbox: "true"
  containers:
    - name: workspace
      image: registry.nestybox.com/nestybox/ubuntu-focal-systemd-docker@sha256:e451ce586ea5c0224c511d4c87b9aa215173db0d1eac6473d0e4b3b5abe41ba5
      imagePullPolicy: Always
      command: ["/sbin/init"]
      securityContext:
        privileged: false
        allowPrivilegeEscalation: false
        capabilities:
          drop:
            - ALL
        seccompProfile:
          type: RuntimeDefault
      resources:
        requests:
          cpu: "1"
          memory: 2Gi
        limits:
          cpu: "2"
          memory: 6Gi
      volumeMounts:
        - name: docker-data
          mountPath: /var/lib/docker
  volumes:
    - name: docker-data
      persistentVolumeClaim:
        claimName: workspace-docker
```

Validate and apply it:

```bash
kubectl apply --server-side --dry-run=server \
  -f workspace.yaml

workspace_started_at="$(date +%s)"

kubectl apply -f workspace.yaml

kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready \
  pod/direct-sysbox-workspace \
  --timeout=15m

echo "workspace ready seconds: $(($(date +%s) - workspace_started_at))"

kubectl -n "$EXPERIMENT_NS" get pod direct-sysbox-workspace -o wide
kubectl -n "$EXPERIMENT_NS" describe pod direct-sysbox-workspace \
  > workspace-describe.txt
kubectl -n "$EXPERIMENT_NS" get pod direct-sysbox-workspace -o yaml \
  > workspace-actual.yaml
```

If the Pod fails, capture its logs, events, node conditions, Sysbox service
logs, and installer logs before retrying. Do not add privilege as a shortcut.

## Phase 7: security-boundary and system-container evidence

Capture the workspace's identity, user namespace, capabilities, seccomp,
SELinux label, mounts, and cgroup view:

```bash
kubectl -n "$EXPERIMENT_NS" exec direct-sysbox-workspace -- bash -ec '
  echo "=== identity ==="
  id
  cat /proc/self/uid_map
  cat /proc/self/gid_map
  grep "^Cap" /proc/self/status
  grep -E "^Seccomp|^Seccomp_filters" /proc/self/status
  cat /proc/self/attr/current || true

  echo "=== namespaces ==="
  readlink /proc/self/ns/user
  readlink /proc/self/ns/cgroup
  cat /proc/self/cgroup

  echo "=== system mounts ==="
  findmnt -R /proc
  findmnt -R /sys
  findmnt -R /sys/fs/cgroup

  echo "=== writable descendant cgroup ==="
  mkdir /sys/fs/cgroup/direct-sysbox-write-probe
  rmdir /sys/fs/cgroup/direct-sysbox-write-probe

  echo "=== systemd and Docker ==="
  systemctl is-system-running --wait || true
  systemctl --failed || true
  systemctl is-active docker
  docker version
  docker info
' |& tee workspace-security-and-runtime.txt
```

From Kubernetes, verify there is no privileged workspace or host namespace:

```bash
kubectl -n "$EXPERIMENT_NS" get pod direct-sysbox-workspace \
  -o jsonpath='runtimeClass={.spec.runtimeClassName}{" hostUsers="}{.spec.hostUsers}{" privileged="}{.spec.containers[0].securityContext.privileged}{" hostPID="}{.spec.hostPID}{" hostIPC="}{.spec.hostIPC}{" hostNetwork="}{.spec.hostNetwork}{"\n"}' \
  | tee workspace-kubernetes-security.txt
```

Expected essentials are `runtimeClass=sysbox-runc`, `hostUsers=false`, and
`privileged=false`; host PID, IPC, and networking must not be true.

## Phase 8: Docker, BuildKit, limits, and networking

Run a basic child container and inspect its privilege:

```bash
kubectl -n "$EXPERIMENT_NS" exec direct-sysbox-workspace -- bash -ec '
  set -euxo pipefail

  docker pull alpine:3.21
  docker run --rm alpine:3.21 id

  child="$(docker create alpine:3.21 sleep 60)"
  docker inspect "$child" \
    --format "privileged={{.HostConfig.Privileged}} pid={{.HostConfig.PidMode}} network={{.HostConfig.NetworkMode}}"
  docker rm "$child"
' |& tee docker-child-smoke.txt
```

Run a BuildKit build:

```bash
kubectl -n "$EXPERIMENT_NS" exec direct-sysbox-workspace -- bash -ec '
  set -euxo pipefail

  tmpdir="$(mktemp -d)"
  printf "%s\n" \
    "FROM alpine:3.21" \
    "RUN id && echo buildkit-ok >/result" \
    > "$tmpdir/Dockerfile"

  DOCKER_BUILDKIT=1 docker build \
    -t direct-sysbox-buildkit:1 "$tmpdir"

  docker run --rm direct-sysbox-buildkit:1 cat /result
' |& tee docker-buildkit.txt
```

Exercise child limits and the ordinary bridge network:

```bash
kubectl -n "$EXPERIMENT_NS" exec direct-sysbox-workspace -- bash -ec '
  set -euxo pipefail

  docker run --rm \
    --memory=128m --cpus=0.25 --pids-limit=64 \
    alpine:3.21 sh -ec "cat /sys/fs/cgroup/memory.max; cat /sys/fs/cgroup/pids.max"

  docker network create direct-sysbox-net
  docker run -d --name web --network direct-sysbox-net nginx:1.27-alpine
  docker run --rm --network direct-sysbox-net alpine:3.21 \
    sh -ec "wget -qO- http://web | grep -q Welcome"
  docker rm -f web
  docker network rm direct-sysbox-net

  echo docker-limits-and-bridge-network-ok
' |& tee docker-limits-and-network.txt
```

Then repeat the Compose, outbound-network, published-port, Kubernetes
ClusterIP, direct Pod-IP, and cross-node tests from the native-userns
experiment. Reuse the same pinned child images and success markers so the
solutions can be compared directly. A simple same-Pod bridge success is not a
substitute for the cross-node Kubernetes tests.

## Phase 9: persistence and replacement

Record a Docker image and marker on the PVC:

```bash
kubectl -n "$EXPERIMENT_NS" exec direct-sysbox-workspace -- bash -ec '
  set -eux
  docker image inspect direct-sysbox-buildkit:1
  printf "%s\n" persistence-v1 > /var/lib/docker/direct-sysbox-marker
'
```

Delete and recreate the workspace Pod without deleting the PVC:

```bash
kubectl -n "$EXPERIMENT_NS" delete pod direct-sysbox-workspace \
  --wait=true --timeout=10m

kubectl apply -f workspace.yaml

kubectl -n "$EXPERIMENT_NS" wait \
  --for=condition=Ready pod/direct-sysbox-workspace \
  --timeout=15m

kubectl -n "$EXPERIMENT_NS" exec direct-sysbox-workspace -- bash -ec '
  set -eux
  grep -Fx persistence-v1 /var/lib/docker/direct-sysbox-marker
  docker image inspect direct-sysbox-buildkit:1
  echo persistence-after-pod-replacement-ok
' |& tee persistence-after-pod-replacement.txt
```

For node replacement, first collect the Node and MNG identity. Replace the
node through the managed-node-group lifecycle rather than manually editing
host files. Confirm the replacement node receives `sysbox-install=yes`, the
installer runs, `sysbox-runtime=running` appears, containerd remains active,
and a newly scheduled workspace passes the smoke suite.

PVC reattachment after node replacement may be constrained by its EBS
availability zone. Treat normal same-zone EBS scheduling separately from
Sysbox installation failure.

## Phase 10: concurrency, isolation, and lifecycle follow-up

Before recommending this approach, add and execute explicit tests for:

1. Two simultaneous Sysbox workspaces on one node.
2. Cross-workspace PID, mount, cgroup, Docker-state, and network isolation.
3. Fork bombs, cgroup-count exhaustion, inotify exhaustion, disk pressure,
   image churn, and nested-container density.
4. Scaling the MNG from one to two nodes and scheduling workspaces on both.
5. Node reboot and replacement.
6. A newer EKS-optimized AL2023 AMI release.
7. Installer reapplication and idempotence.
8. Sysbox upgrade and rollback.
9. Coder agent startup, reconnect, stop/start, and delete behavior.
10. Representative devcontainer and Testcontainers workloads.

Capture CPU, memory, disk, and startup-time overhead and compare them with
Envbox and native userns DinD. Direct Sysbox should avoid Envbox's duplicate
per-workspace outer Docker/containerd/Sysbox daemons, but that expected
advantage must be measured.

## Cleanup

For a functional uninstall test, stop and delete all Sysbox workspace Pods
first. Render the upstream uninstall manifest with the same custom installer
digest; do not run the unmodified upstream image because its installer helper
does not contain the AL2023 changes.

The simplest final cleanup for this disposable experiment is cluster deletion:

```bash
kubectl config current-context

eksctl delete cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME" \
  --wait \
  |& tee cluster-delete.log
```

Do not delete the shared ECR repository until the Envbox and direct-Sysbox
experiments are both complete. When it is eventually removed, record all tags
and digests first so the findings retain an auditable image identity.

## Findings to record

Create `findings.md` as the experiment progresses. At minimum record:

- exact EKS version, platform version, AL2023 AMI ID, kernel, containerd, runc,
  and EBS CSI versions;
- exact installer source diff and image digest;
- `dnf` dependency and Shiftfs-skip evidence;
- whether containerd was edited/restarted and whether CRI-O remained inactive;
- Sysbox service versions, status, and logs;
- actual workspace UID/GID mappings, capabilities, seccomp, SELinux, `/proc`,
  `/sys`, and cgroup views;
- complete Docker, BuildKit, Compose, networking, resource, persistence,
  concurrency, and replacement results;
- installer, workspace, node, and Coder startup timings;
- all nonblocking degraded systemd units and warnings;
- security and operational differences from Envbox and native userns DinD;
- ownership and maintenance cost of carrying the downstream AL2023 installer
  patch across future EKS AMI and Sysbox releases.

## References

- `~/sysbox/docs/distro-compat.md`
- `~/sysbox/docs/user-guide/install-k8s.md`
- `~/sysbox/sysbox-k8s-manifests/sysbox-install.yaml`
- `~/sysbox/sysbox-pkgr/k8s/scripts/sysbox-deploy-k8s.sh`
- `~/sysbox/sysbox-pkgr/k8s/scripts/sysbox-installer-helper.sh`
- `../solutions-matrix.md`
- `../eks-mng-amazon-linux-userns-rootful-dind-experiment/findings.md`
- `../eks-auto-mode-bottlerocket-envbox-experiment/findings.md`
