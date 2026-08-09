# EKS Amazon Linux managed-node-group user-namespace rootful-DinD runbook

Date prepared: 2026-08-05

## Purpose

Determine whether a Coder workspace can run a **rootful** Docker daemon and
BuildKit inside a native Kubernetes user-namespace Pod on an EKS Kubernetes
1.36 cluster using a dedicated **Amazon Linux managed node group (MNG)**,
without Envbox or Sysbox.

This is the baseline experiment. Amazon Linux MNG nodes are deliberately used
before EKS Auto Mode/Bottlerocket: they remove Auto Mode provisioning and
Bottlerocket policy as confounding variables and offer a more practical place
to diagnose a failure. A pass here is necessary but not sufficient evidence
for a later Auto Mode/Bottlerocket compatibility test.

The intended security model is:

```text
EKS node (Amazon Linux MNG)
  └─ Kubernetes Pod: hostUsers: false
       └─ workspace / dockerd runs as UID 0 inside the Pod
            └─ Docker child containers run as UID 0 inside Docker
```

The kubelet maps the Pod's UID 0 to an unprivileged, non-overlapping host ID
range. `privileged: true` therefore supplies capabilities inside the Pod's
user namespace, not equivalent host-root capabilities. This is deliberately
different from **rootless Docker**: the Docker daemon under test is rootful
from its own point of view.

The experiment answers whether this can replace Envbox for ordinary Coder
workspaces that need Docker builds and test containers. It does not establish
feature parity with Envbox's system-container use cases (for example arbitrary
host integration or full VM-like `systemd` behavior).

## Decision rules

Record each result as **pass**, **fail**, or **blocked**. Do not silently
substitute an ordinary Pod for `hostUsers: false`, or a rootless Docker daemon
for rootful Docker; either would test a different design.

The baseline is promising only if all of these pass on the MNG target:

1. The EKS cluster admits a `hostUsers: false`, `privileged: true`,
   `procMount: Unmasked` Pod.
2. The workspace's real persistent-volume filesystem can mount into that Pod
   (user-namespace Pods require idmapped-mount support).
3. A normal rootful `dockerd` starts without Envbox/Sysbox and can run,
   build, network, and persist Docker containers/images.
4. `/proc/self/uid_map` shows that container UID 0 maps to a nonzero host ID.
5. The chosen storage driver is acceptable. `overlay2` or a demonstrably
   performant `fuse-overlayfs` result is a potential pass; `vfs` is only a
   diagnostic fallback, not a production-quality result.

A failure of native user namespaces, idmapped volume mounts, or admission
policy is a stop condition for this MNG baseline. A Docker failure after those
pass is useful: it identifies a narrower Docker/runtime issue to investigate.

## Scope and non-goals

This runbook tests one Linux/amd64 EKS cluster with a dedicated Amazon Linux
MNG and an EBS-backed workspace-like PVC. It does not test NFS; NFS is
currently incompatible with Kubernetes user-namespace Pods because the Linux
NFS client does not support idmapped mounts.

It also does not claim that a `privileged` Pod will be accepted in every
customer environment. Even when its effective capabilities are user-namespace
scoped, `privileged: true` and `procMount: Unmasked` can still be rejected by
Pod Security Admission or an organization-specific admission policy.

## Prerequisites

- A disposable EKS cluster running Kubernetes **1.36**, with a dedicated
  Amazon Linux 2023 MNG. Kubernetes user namespaces are GA in 1.36. Confirm
  the exact EKS version and node runtime; do not infer support from the
  Kubernetes API alone.
- `aws`, `kubectl`, and `eksctl` installed and authenticated.
- Permission to create/delete the test namespace, Pods, and PVC. If
  creating a disposable cluster, the AWS identity also needs normal EKS/VPC/
  EC2/IAM creation and deletion permissions.
- Approval for a namespaced privileged Pod. No hostPath, host networking,
  host PID, or host IPC is requested by this experiment.

Before changing anything, record the cluster and client state:

```bash
export AWS_REGION="us-east-2"
export CLUSTER_NAME="REPLACE_WITH_CLUSTER_NAME"
export EXPERIMENT_NS="userns-rootful-dind"

aws sts get-caller-identity
aws eks describe-cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME" \
  --query 'cluster.{name:name,version:version,platformVersion:platformVersion,status:status}' \
  --output yaml | tee cluster-version.yaml
kubectl version -o yaml | tee kubectl-version.yaml
kubectl get nodes -o wide | tee nodes-before.txt
kubectl get storageclass | tee storageclasses.txt
kubectl get nodes -L eks.amazonaws.com/nodegroup,kubernetes.io/os,kubernetes.io/arch \
  > nodes-labelled-before.txt
```

Stop if the server version is below 1.36. Also stop if no EBS-like storage
class is available; test the same storage class that a Coder workspace would
use, not `emptyDir` alone.

## Optional: create a disposable EKS cluster and Amazon Linux MNG

Skip this section when an approved test cluster already exists. Otherwise,
save this as `cluster.yaml`, choosing a unique name and region:

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: userns-rootful-dind-mng-136
  region: us-east-2
  version: "1.36"

# Preserve the conventional-MNG setup if a future eksctl release enables Auto
# Mode by default.
autoModeConfig:
  enabled: false

managedNodeGroups:
  - name: userns-dind
    amiFamily: AmazonLinux2023
    instanceType: m6i.large
    minSize: 1
    desiredCapacity: 1
    maxSize: 2
    labels:
      experiment.coder.com/userns-dind: "true"
    iam:
      withAddonPolicies:
        ebs: true

addons:
  - name: aws-ebs-csi-driver
```

Create it and configure `kubectl`:

```bash
eksctl create cluster -f cluster.yaml
export CLUSTER_NAME="userns-rootful-dind-mng-136"
aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME"
```

Re-run the prerequisite collection commands after creation.

## Create or select a dedicated Amazon Linux MNG

The disposable-cluster configuration above already creates the target MNG. If
using an existing cluster, create an equivalent dedicated Amazon Linux 2023
MNG with the label below, or apply it through the approved node-group
management path. Do not run this baseline on Auto Mode/Bottlerocket nodes.

Do not taint this small, dedicated MNG: EBS CSI and other cluster add-ons need
to schedule on it. The label is enough to ensure the experiment Pods select it.

Required node label:

```yaml
labels:
  experiment.coder.com/userns-dind: "true"
```

Confirm the target nodes are Amazon Linux MNG nodes and capture them:

```bash
kubectl get nodes -l experiment.coder.com/userns-dind=true -o wide \
  | tee userns-dind-nodes.txt
kubectl get nodes -l experiment.coder.com/userns-dind=true -o yaml \
  > userns-dind-nodes.yaml
```

Stop if this selector yields no nodes, if the nodes are not Linux/amd64, or if
they are not members of the intended Amazon Linux MNG. Record the MNG AMI and
container-runtime versions if they are visible from the node description.

## Create the namespace and PVC

The namespace deliberately requests the `privileged` Pod Security Admission
profile, because the main probe requests `privileged: true` and an unmasked
proc mount. If organizational policy rejects this namespace label, record the
rejection as an admission-policy block rather than weakening the experiment.

Prefer a direct `ebs.csi.aws.com` StorageClass rather than a legacy
`kubernetes.io/aws-ebs` class. The included `storageclass.yaml` creates a
`gp3-csi` class for this purpose. Apply it before the included
`namespace-and-pvc.yaml` manifest.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: userns-rootful-dind
  labels:
    pod-security.kubernetes.io/enforce: privileged
    pod-security.kubernetes.io/audit: privileged
    pod-security.kubernetes.io/warn: privileged
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: workspace-data
  namespace: userns-rootful-dind
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: gp3-csi
  resources:
    requests:
      storage: 30Gi
```

Apply it and record its initial state:

```bash
kubectl apply -f storageclass.yaml
kubectl apply -f namespace-and-pvc.yaml
kubectl -n "$EXPERIMENT_NS" get namespace,pvc -o yaml > namespace-and-pvc.actual.yaml
```

Many EBS StorageClasses use `WaitForFirstConsumer`. For those, a newly created
PVC correctly remains `Pending` until Test 1 schedules; do **not** wait for it
to become `Bound` before creating that Pod. Test 1's successful start and PVC
write are the binding/mount proof.

## Test 1: native user namespace plus real PVC

This probe has no Docker daemon and no privileged security context. It
separates Kubernetes user-namespace and idmapped-volume support from the later
Docker test. Save as `userns-volume-probe.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: userns-volume-probe
  namespace: userns-rootful-dind
spec:
  hostUsers: false
  restartPolicy: Never
  nodeSelector:
    experiment.coder.com/userns-dind: "true"
  containers:
    - name: probe
      image: public.ecr.aws/docker/library/alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
      command: ["/bin/sh", "-c"]
      args:
        - |
          set -eux
          id
          echo '--- uid/gid mapping ---'
          cat /proc/self/uid_map
          cat /proc/self/gid_map
          echo '--- PVC write ---'
          echo "$(date -u +%FT%TZ) userns-volume-probe" > /workspace/probe.txt
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

Run it:

```bash
kubectl apply -f userns-volume-probe.yaml
kubectl -n "$EXPERIMENT_NS" wait --for=condition=Ready pod/userns-volume-probe --timeout=15m
kubectl -n "$EXPERIMENT_NS" logs userns-volume-probe | tee test1-userns-volume.log
kubectl -n "$EXPERIMENT_NS" get pod userns-volume-probe -o yaml > test1-pod.yaml
kubectl -n "$EXPERIMENT_NS" describe pod userns-volume-probe > test1-describe.txt
export NODE_NAME="$(kubectl -n "$EXPERIMENT_NS" get pod userns-volume-probe -o jsonpath='{.spec.nodeName}')"
kubectl get node "$NODE_NAME" -o yaml > test1-node.yaml
```

Expected result:

- the Pod is Ready;
- `uid_map` contains a mapping from container ID `0` to a nonzero host ID;
- the PVC write succeeds.

If it fails with `MOUNT_ATTR_IDMAP`, the PVC filesystem or node runtime does
not support this design. If `hostUsers` is rejected or ignored, stop: the
cluster cannot test the proposed security model.

Delete the probe before the next test so the RWO PVC can attach to the Docker
Pod:

```bash
kubectl -n "$EXPERIMENT_NS" delete pod userns-volume-probe --wait=true
```

## Test 2: admission of namespaced privileged / unmasked proc

This short probe identifies an admission-policy or runtime restriction before
Docker muddies the result. Save as `privileged-userns-probe.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: privileged-userns-probe
  namespace: userns-rootful-dind
spec:
  hostUsers: false
  restartPolicy: Never
  nodeSelector:
    experiment.coder.com/userns-dind: "true"
  containers:
    - name: probe
      image: public.ecr.aws/docker/library/alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d
      securityContext:
        privileged: true
        procMount: Unmasked
      command: ["/bin/sh", "-c"]
      args:
        - |
          set -eux
          id
          cat /proc/self/uid_map
          mkdir /tmp/private-mount
          mount -t tmpfs tmpfs /tmp/private-mount
          mount | grep /tmp/private-mount
          umount /tmp/private-mount
          sleep 600
```

Apply and collect results:

```bash
kubectl apply -f privileged-userns-probe.yaml
kubectl -n "$EXPERIMENT_NS" wait --for=condition=Ready pod/privileged-userns-probe --timeout=15m
kubectl -n "$EXPERIMENT_NS" logs privileged-userns-probe | tee test2-privileged-userns.log
kubectl -n "$EXPERIMENT_NS" describe pod privileged-userns-probe > test2-describe.txt
```

The private `tmpfs` mount is expected to be possible inside this namespaced
privileged Pod. It is not a host mount and does not demonstrate host access.
If the Pod is rejected, preserve the full API/server message. That is a real
operational limitation even though the user namespace would confine the
effective kernel capabilities.

Delete it before continuing:

```bash
kubectl -n "$EXPERIMENT_NS" delete pod privileged-userns-probe --wait=true
```

## Test 3: rootful Docker and BuildKit

Save this as `rootful-dind.yaml`. The original `docker:27-dind` image resolved
to Docker Engine 27.5.1, so the replay pins `docker:27.5.1-dind`. It supplies
dockerd and the Docker client; the command starts a normal rootful daemon
manually. Do not use a `*-dind-rootless` image for this test.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: rootful-dind
  namespace: userns-rootful-dind
spec:
  hostUsers: false
  restartPolicy: Never
  nodeSelector:
    experiment.coder.com/userns-dind: "true"
  containers:
    - name: workspace
      image: docker:27.5.1-dind
      securityContext:
        privileged: true
        procMount: Unmasked
      env:
        - name: DOCKER_TLS_CERTDIR
          value: ""
        - name: DOCKER_HOST
          value: unix:///run/docker.sock
      command: ["/bin/sh", "-c"]
      args:
        - |
          set -eu
          mkdir -p /run /workspace/docker-data /workspace/docker-artifacts
          alpine_image='alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d'
          nginx_image='nginx:1.27-alpine@sha256:65645c7bb6a0661892a8b03b89d0743208a18dd2f3f17a54ef4b76fb8e2f2a10'
          echo '--- outer user namespace mapping ---' | tee /workspace/docker-artifacts/uid-map.txt
          cat /proc/self/uid_map | tee -a /workspace/docker-artifacts/uid-map.txt
          cat /proc/self/gid_map | tee -a /workspace/docker-artifacts/uid-map.txt

          dockerd \
            --host=unix:///run/docker.sock \
            --data-root=/workspace/docker-data \
            --pidfile=/run/dockerd.pid \
            > /workspace/docker-artifacts/dockerd.log 2>&1 &
          dockerd_pid=$!
          trap 'kill "$dockerd_pid" 2>/dev/null || true; wait "$dockerd_pid" 2>/dev/null || true' EXIT

          i=0
          until docker info >/workspace/docker-artifacts/docker-info.txt 2>&1; do
            i=$((i + 1))
            if [ "$i" -ge 90 ]; then
              echo 'dockerd did not become ready' >&2
              cat /workspace/docker-artifacts/dockerd.log >&2 || true
              exit 1
            fi
            sleep 2
          done

          docker version | tee /workspace/docker-artifacts/docker-version.txt
          docker info | tee /workspace/docker-artifacts/docker-info-full.txt
          docker info --format '{{.Driver}}' | tee /workspace/docker-artifacts/storage-driver.txt

          time docker pull "$alpine_image"
          docker run --rm "$alpine_image" id > /workspace/docker-artifacts/child-id.txt
          cat /workspace/docker-artifacts/child-id.txt

          mkdir -p /tmp/build-context
          printf '%s\n' "FROM $alpine_image" 'RUN id' 'CMD ["/bin/sh", "-c", "echo buildkit-ok"]' > /tmp/build-context/Dockerfile
          DOCKER_BUILDKIT=1 docker build -t userns-dind-smoke:1 /tmp/build-context \
            > /workspace/docker-artifacts/build.log 2>&1
          cat /workspace/docker-artifacts/build.log
          docker run --rm userns-dind-smoke:1 > /workspace/docker-artifacts/build-result.txt
          cat /workspace/docker-artifacts/build-result.txt

          docker run -d --name httpd "$nginx_image"
          httpd_ip="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' httpd)"
          docker run --rm "$alpine_image" wget -qO- "http://${httpd_ip}" \
            > /workspace/docker-artifacts/network-result.txt
          head -c 100 /workspace/docker-artifacts/network-result.txt
          docker rm -f httpd

          # This checks the ordinary Docker resource-control path. A failure is
          # informative; record it rather than silently removing the limit.
          if docker run --rm --memory=64m --pids-limit=64 "$alpine_image" true \
              > /workspace/docker-artifacts/resource-limit-result.txt 2>&1; then
            echo pass > /workspace/docker-artifacts/resource-limit-status.txt
          else
            echo fail > /workspace/docker-artifacts/resource-limit-status.txt
          fi

          # A nested privileged container may have namespaced capabilities. It
          # must still not be interpreted as a host-privilege test.
          docker run --rm --privileged "$alpine_image" sh -c '
            mkdir /tmp/nested-private-mount &&
            mount -t tmpfs tmpfs /tmp/nested-private-mount &&
            umount /tmp/nested-private-mount &&
            echo nested-namespaced-privilege-ok
          ' > /workspace/docker-artifacts/nested-privileged-result.txt
          cat /workspace/docker-artifacts/nested-privileged-result.txt

          touch /workspace/docker-artifacts/test3-complete
          echo 'Test 3 complete; keeping daemon alive for inspection.'
          wait "$dockerd_pid"
      volumeMounts:
        - name: workspace-data
          mountPath: /workspace
  volumes:
    - name: workspace-data
      persistentVolumeClaim:
        claimName: workspace-data
```

Run and observe the startup rather than assuming the Pod will become Ready:

```bash
kubectl apply -f rootful-dind.yaml
kubectl -n "$EXPERIMENT_NS" get pod rootful-dind -w
```

In another terminal, collect the outcome:

```bash
kubectl -n "$EXPERIMENT_NS" logs -f rootful-dind
kubectl -n "$EXPERIMENT_NS" describe pod rootful-dind > test3-describe.txt
kubectl -n "$EXPERIMENT_NS" get pod rootful-dind -o yaml > test3-pod.yaml
kubectl -n "$EXPERIMENT_NS" exec rootful-dind -- sh -c '
  find /workspace/docker-artifacts -maxdepth 1 -type f -printf "%f\\n" | sort
  cat /workspace/docker-artifacts/storage-driver.txt
  cat /workspace/docker-artifacts/uid-map.txt
'
```

Expected functional results:

- `dockerd` starts without Envbox/Sysbox;
- `docker info` reports an intentional storage driver;
- the child `alpine` container and BuildKit build succeed;
- one nested container reaches another over Docker's bridge network;
- the inner privileged-container test succeeds only within its own private
  namespaces;
- the outer Pod UID map still maps ID 0 to a nonzero host ID.

If dockerd fails, preserve `dockerd.log` from the PVC before deleting the Pod:

```bash
kubectl -n "$EXPERIMENT_NS" exec rootful-dind -- \
  cat /workspace/docker-artifacts/dockerd.log > test3-dockerd.log || true
```

### Storage-driver fallback diagnostics

Do not change the first test: its default driver tells us whether this design
works naturally. If it fails specifically on overlay storage, make two clearly
labelled diagnostic reruns:

1. Add `--storage-driver=fuse-overlayfs` **only if** `fuse-overlayfs` exists
   in the image and `/dev/fuse` is available inside the Pod.
2. Add `--storage-driver=vfs` only to prove that the remaining Docker stack
   works. Treat a vfs-only result as a performance/design failure, not a final
   acceptance.

For each rerun, use a fresh PVC or remove `/workspace/docker-artifacts` and
`/workspace/docker-data` deliberately, then record the exact daemon
arguments.

## Test 4: Docker-store persistence across a workspace restart

The workspace PVC should preserve Docker's `/workspace/docker-data` store,
while the Pod itself is recreated. This distinguishes a usable per-workspace
Docker cache from an `emptyDir`-only demo.

After Test 3 has created `userns-dind-smoke:1`:

```bash
kubectl -n "$EXPERIMENT_NS" delete pod rootful-dind --wait=true
kubectl apply -f rootful-dind.yaml
kubectl -n "$EXPERIMENT_NS" wait --for=condition=Ready pod/rootful-dind --timeout=15m
until kubectl -n "$EXPERIMENT_NS" exec rootful-dind -- docker info >/dev/null 2>&1; do sleep 2; done
kubectl -n "$EXPERIMENT_NS" exec rootful-dind -- \
  docker image inspect userns-dind-smoke:1 > test4-image-inspect.json
```

Record whether the image is immediately available. This is only a
per-workspace cache. It does **not** give the node-wide sharing that the
Envbox image-cache proposal targets; a second workspace will have its own
Docker store unless a separate registry/cache mechanism is used.

## Interpret results

Use this matrix in the experiment result note:

| Result | Interpretation | Next action |
|---|---|---|
| Test 1 fails | Native Kubernetes user namespaces or the real PVC filesystem are unavailable. | Stop this baseline design; do not debug Docker. |
| Test 2 fails | The cluster policy/runtime will not allow the required namespaced privileged shape. | Determine whether a less-privileged tailored dockerd/BuildKit configuration is possible; otherwise stop. |
| Tests 1–2 pass, dockerd fails | Platform supports the isolation model, but rootful DinD needs a storage, mount, cgroup, or networking adjustment. | Diagnose from `dockerd.log`; compare `overlay2`, `fuse-overlayfs`, then `vfs`. |
| Docker works only with vfs | Functional proof only; likely too slow/disk-heavy for Coder workspaces. | Investigate `fuse-overlayfs` or another supported storage layout. |
| Tests 1–4 pass with acceptable storage | Viable candidate for a Coder workspace backend. | Run Coder-agent and real repository/devcontainer tests next. |

In the recorded run, Tests 1 and 2 passed, but the stock-runtime Docker Pod
could not create `/sys/fs/cgroup/docker`. Continue with
[`cgroup-writable-runtime/runbook.md`](cgroup-writable-runtime/runbook.md) to
reproduce the purpose-configured MNG follow-up and its successful
domain-cgroup topology. Do not modify this baseline control to hide that
failure.

## Follow-up Coder validation

Only after the base test passes, replace the synthetic workspace command with
a Coder agent and run a representative workflow:

1. provision a workspace from the candidate template;
2. clone/open `~/coder` in VS Code;
3. reopen in its `.devcontainer` configuration if requested;
4. run the Docker-using Coder tests and a BuildKit build;
5. verify Coder resource limits, stop/start behavior, and the PVC-backed
   Docker-store persistence;
6. compare cold/warm image pulls with Envbox on the same node class.

Do not describe this as an Envbox replacement until those end-to-end tests
pass. In particular, retain Envbox for workloads requiring system-container
features that the native user-namespace model does not provide.

## Collect artifacts and clean up

Before cleanup, collect events and all PVC-backed logs:

```bash
kubectl -n "$EXPERIMENT_NS" get events --sort-by=.lastTimestamp > events.txt
kubectl -n "$EXPERIMENT_NS" get all -o yaml > final-kubernetes-objects.yaml
kubectl -n "$EXPERIMENT_NS" exec rootful-dind -- \
  tar -C /workspace -czf - docker-artifacts > docker-artifacts.tgz || true
kubectl get node "$NODE_NAME" -o yaml > final-node.yaml
kubectl get nodes -L eks.amazonaws.com/nodegroup -o yaml > final-nodes.yaml
```

Delete the experiment resources when results are recorded:

```bash
kubectl delete -f rootful-dind.yaml --ignore-not-found
kubectl delete namespace "$EXPERIMENT_NS" --ignore-not-found
```

If this runbook created a disposable cluster, delete it after the namespace
has terminated:

```bash
eksctl delete cluster --region "$AWS_REGION" --name "$CLUSTER_NAME"
```

Confirm no test EBS volumes, EC2 instances, managed node groups, or cluster resources
remain. The experiment incurs AWS charges while the cluster and PVC exist.
