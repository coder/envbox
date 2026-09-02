# EKS Auto Mode 1.36 nested-virtualization runbook

Date prepared: 2026-07-29

## Purpose

Determine whether an Amazon EKS Auto Mode cluster running Kubernetes 1.36
provisions an EC2 node with nested virtualization enabled, and whether a
container scheduled on that node can access `/dev/kvm`.

This runbook is a confirmation experiment. Selecting a supported instance
type such as `c8i.2xlarge` does not by itself enable nested virtualization;
the EC2 instance must be launched with
`CpuOptions.NestedVirtualization=enabled`. The test must therefore record the
actual EC2 `CpuOptions` response rather than infer the result from the
instance type.

AWS currently documents EKS Auto Mode as using EKS-owned managed instances and
AWS-selected immutable AMIs. Auto Mode NodeClasses expose infrastructure
settings, but this runbook does not assume that they expose EC2 CPU options.

## Scope and expected interpretation

This test answers:

1. Does Auto Mode 1.36 enable nested virtualization for the requested node?
2. Does the node expose processor virtualization extensions to a pod?
3. Does the node have `/dev/kvm`?
4. Can a container open `/dev/kvm` when the device is explicitly passed to it?

It does not prove that a production microVM supervisor, Firecracker, or Cloud
Hypervisor will work. It also does not test a standard EKS managed node group
with an EC2 launch template; that is a separate comparison experiment.

## Prerequisites

Install and authenticate:

```bash
aws --version
kubectl version --client
eksctl version
```

Use an `eksctl` version supported by the current EKS Auto Mode documentation
(at least `0.195.0`). The AWS CLI identity needs permissions to create and
delete EKS, EC2, VPC, CloudFormation, IAM, and related resources.

Choose a region where all of the following are available:

- EKS Kubernetes 1.36
- EKS Auto Mode
- the `c8i.2xlarge` instance type
- the required ECR/EC2 endpoints and capacity

The default `eksctl` VPC is convenient for an experiment. If using an existing
VPC, ensure its subnets and routing allow Auto Mode nodes to pull the probe
image and reach the EKS API.

## Set experiment variables

Use a unique cluster name. Keep the values in the shell session so that the
same variables are used for collection and cleanup.

```bash
export AWS_REGION="us-east-2"
export CLUSTER_NAME="nested-virt-auto-136"
export K8S_VERSION="1.36"
export NODE_INSTANCE_TYPE="c8i.2xlarge"
```

Check that the chosen region is the intended account and partition:

```bash
aws sts get-caller-identity
aws configure get region || true
```

## Create the Auto Mode cluster

Create `cluster.yaml`:

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: nested-virt-auto-136
  region: us-east-2
  version: "1.36"

autoModeConfig:
  enabled: true
```

Replace the name and region with the values selected above. The documented
`autoModeConfig.enabled: true` configuration enables Auto Mode and creates the
default `general-purpose` and `system` NodePools.

Create the cluster:

```bash
eksctl create cluster -f cluster.yaml
aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME"
```

Record the versions immediately after creation:

```bash
aws eks describe-cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME" \
  --query 'cluster.{name:name,version:version,platformVersion:platformVersion,status:status}' \
  --output yaml

kubectl version -o yaml
kubectl get nodes -o wide
kubectl get nodepools,nodeclasses
```

The output must confirm Kubernetes `1.36`. If it does not, stop and do not
interpret the experiment as an EKS 1.36 result.

## Request a supported nested-virtualization instance type

Create `nested-virt-nodepool.yaml`:

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: nested-virt
spec:
  template:
    metadata:
      labels:
        workload: nested-virt
    spec:
      nodeClassRef:
        group: eks.amazonaws.com
        kind: NodeClass
        name: default
      taints:
        - key: workload
          value: nested-virt
          effect: NoSchedule
      requirements:
        - key: node.kubernetes.io/instance-type
          operator: In
          values:
            - c8i.2xlarge
        - key: kubernetes.io/arch
          operator: In
          values:
            - amd64
        - key: karpenter.sh/capacity-type
          operator: In
          values:
            - on-demand
```

Apply it:

```bash
kubectl apply -f nested-virt-nodepool.yaml
kubectl get nodepool nested-virt -o yaml
```

Create a CPU-only trigger pod so Auto Mode must provision from the new
NodePool. The pod does not need `/dev/kvm`; its purpose is to cause the node to
launch.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nested-virt-trigger
spec:
  restartPolicy: Never
  nodeSelector:
    workload: nested-virt
  tolerations:
    - key: workload
      value: nested-virt
      effect: NoSchedule
  containers:
    - name: trigger
      image: public.ecr.aws/amazonlinux/amazonlinux:2023
      command: ["/bin/bash", "-c"]
      args: ["sleep 3600"]
      resources:
        requests:
          cpu: "2"
          memory: 2Gi
        limits:
          cpu: "2"
          memory: 2Gi
```

Save it as `nested-virt-trigger.yaml` and apply it:

```bash
kubectl apply -f nested-virt-trigger.yaml
kubectl wait --for=condition=Ready pod/nested-virt-trigger --timeout=15m
NODE_NAME="$(kubectl get pod nested-virt-trigger -o jsonpath='{.spec.nodeName}')"
kubectl get node "$NODE_NAME" -o wide --show-labels
```

Confirm the instance type and collect the provider ID:

```bash
kubectl get node "$NODE_NAME" \
  -o jsonpath='{.metadata.name}{"\n"}{.spec.providerID}{"\n"}'

kubectl get node "$NODE_NAME" -o yaml > node.yaml
kubectl get nodeclaims -o yaml > nodeclaims.yaml
kubectl get nodeclasses -o yaml > nodeclasses.yaml
```

Extract the EC2 instance ID from the provider ID, then query EC2. The exact
command below assumes a provider ID such as `aws:///.../i-0123456789abcdef0`.

```bash
INSTANCE_ID="$(kubectl get node "$NODE_NAME" -o jsonpath='{.spec.providerID}' | sed 's#^.*/##')"

aws ec2 describe-instances \
  --region "$AWS_REGION" \
  --instance-ids "$INSTANCE_ID" \
  --query 'Reservations[0].Instances[0].{InstanceId:InstanceId,InstanceType:InstanceType,ImageId:ImageId,State:State.Name,CpuOptions:CpuOptions,LaunchTime:LaunchTime}' \
  --output yaml | tee ec2-instance.yaml
```

## Test 1: processor virtualization flags inside a container

Apply this diagnostic pod. It is intentionally an ordinary, non-privileged
container. It tests whether processor virtualization extensions are visible
through the Kubernetes container boundary.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nested-virt-cpu-probe
spec:
  restartPolicy: Never
  nodeName: REPLACE_WITH_NODE_NAME
  tolerations:
    - operator: Exists
  containers:
    - name: probe
      image: public.ecr.aws/amazonlinux/amazonlinux:2023
      command: ["/bin/bash", "-c"]
      args:
        - |
          set -eux
          echo '--- CPU virtualization flags ---'
          grep -Eo 'vmx|svm' /proc/cpuinfo | sort -u || true
          echo '--- /dev/kvm ---'
          ls -l /dev/kvm || true
          echo '--- /dev/net/tun ---'
          ls -l /dev/net/tun || true
          sleep 30
```

Replace `REPLACE_WITH_NODE_NAME` with `$NODE_NAME`, save as
`nested-virt-cpu-probe.yaml`, and run:

```bash
kubectl apply -f nested-virt-cpu-probe.yaml
kubectl logs nested-virt-cpu-probe
```

Interpretation:

- `vmx` or `svm` means the CPU virtualization extension is visible to the
  ordinary container.
- Missing `/dev/kvm` is not conclusive by itself: device nodes are not
  automatically passed into a pod merely because the host supports KVM.

## Test 2: explicit `/dev/kvm` access from a container

This diagnostic uses explicit host-device mounts and privileged mode. It is a
diagnostic only, not the intended security model for the microVM workspace.
If Auto Mode admission or its node security policy rejects this pod, record
that result separately.

Create `nested-virt-kvm-probe.yaml`, replacing `REPLACE_WITH_NODE_NAME`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nested-virt-kvm-probe
spec:
  restartPolicy: Never
  nodeName: REPLACE_WITH_NODE_NAME
  tolerations:
    - operator: Exists
  containers:
    - name: probe
      image: public.ecr.aws/amazonlinux/amazonlinux:2023
      command: ["/bin/bash", "-c"]
      args:
        - |
          set -eux
          ls -l /dev/kvm
          if command -v dmesg >/dev/null 2>&1; then dmesg | tail -100 || true; fi
          # The open test is the important result. A successful open does not
          # prove that a complete VMM can boot a guest. Bash opens the device
          # read/write without requiring extra packages in the probe image.
          exec 3<> /dev/kvm
          echo 'opened /dev/kvm read/write'
          exec 3>&-
      securityContext:
        privileged: true
      volumeMounts:
        - name: kvm
          mountPath: /dev/kvm
        - name: tun
          mountPath: /dev/net/tun
  volumes:
    - name: kvm
      hostPath:
        path: /dev/kvm
        type: CharDevice
    - name: tun
      hostPath:
        path: /dev/net/tun
        type: CharDevice
```

Run it:

```bash
kubectl apply -f nested-virt-kvm-probe.yaml
kubectl get pod nested-virt-kvm-probe -o wide
kubectl describe pod nested-virt-kvm-probe
kubectl logs nested-virt-kvm-probe
```

Record all admission, mount, SELinux, and runtime errors. A successful pod
start and successful `open('/dev/kvm', ...)` demonstrate device access from a
container, but do not demonstrate that an unprivileged pod can obtain the
device through a device plugin.

## Test 3: inspect the host through a diagnostic DaemonSet

If Test 2 is rejected, use the node’s control-plane-visible metadata and
runtime logs to determine whether `/dev/kvm` exists on the host. Do not assume
that failure to mount `/dev/kvm` means the EC2 CPU option was disabled.

For a controlled diagnostic, use a short-lived privileged DaemonSet only if
the cluster’s security policy permits it. The DaemonSet should run only on the
`workload=nested-virt` node and report:

```bash
ls -l /dev/kvm
grep -E 'vmx|svm' /proc/cpuinfo | sort -u
cat /sys/module/kvm/parameters/nested 2>/dev/null || true
```

Delete the diagnostic DaemonSet immediately after collecting its output.

## Required evidence to save

Save these outputs together with the run date and AWS account/region:

```bash
aws eks describe-cluster --region "$AWS_REGION" --name "$CLUSTER_NAME" \
  --query 'cluster.{version:version,platformVersion:platformVersion}' --output yaml

kubectl get nodes -o wide > nodes.txt
kubectl get node "$NODE_NAME" -o yaml > node.yaml
kubectl get nodeclaims -o yaml > nodeclaims.yaml
kubectl get nodeclasses -o yaml > nodeclasses.yaml
kubectl describe pod nested-virt-cpu-probe > cpu-probe.describe.txt
kubectl logs nested-virt-cpu-probe > cpu-probe.log
kubectl describe pod nested-virt-kvm-probe > kvm-probe.describe.txt
kubectl logs nested-virt-kvm-probe > kvm-probe.log
cat ec2-instance.yaml
```

Also record the Bottlerocket version if it is exposed through node labels,
provider metadata, or the EKS/EC2 console. The probe image is Amazon Linux
2023; that does not identify the node operating system.

## Result classification

Classify the result using the strongest evidence available:

| Result | Meaning |
|---|---|
| `CpuOptions.NestedVirtualization` is absent/disabled | Auto Mode did not launch the requested instance with nested virtualization enabled. Stop here for a KVM conclusion. |
| CPU flags absent in ordinary pod | Virtualization extensions are not visible to the container, or the runtime masks them. |
| CPU flags present but no host `/dev/kvm` | The EC2 feature may be enabled, but the node kernel/device setup does not provide KVM. |
| Host `/dev/kvm` exists but explicit pod mount fails | Kubernetes/runtime/security policy blocks container device access. |
| Explicit privileged pod opens `/dev/kvm` | Container device access is possible with privilege; this does not prove the desired unprivileged device-plugin model. |
| Unprivileged device-plugin pod opens `/dev/kvm` | Strong evidence that the proposed container-facing KVM model is feasible on this Auto Mode configuration. |
| Minimal VMM boots a guest | Strongest result; proceed to networking, storage, lifecycle, and Coder-agent tests. |

Do not describe the result as “nested virtualization is impossible” unless
the exact tested scope is stated. A negative result here means only that the
tested EKS Auto Mode 1.36 configuration did not provide the required feature.

## Cleanup

Delete test resources first:

```bash
kubectl delete pod nested-virt-trigger nested-virt-cpu-probe nested-virt-kvm-probe --ignore-not-found
kubectl delete nodepool nested-virt --ignore-not-found
```

Then delete the cluster when all evidence has been saved:

```bash
eksctl delete cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME"
```

Confirm that no EC2 instances, EBS volumes, load balancers, NAT gateways, or
other experiment resources remain if the VPC was not created and managed by
eksctl.

## References

- [EKS Auto Mode with eksctl](https://docs.aws.amazon.com/eks/latest/eksctl/auto-mode.html)
- [Create an EKS Auto Mode cluster with eksctl](https://docs.aws.amazon.com/eks/latest/userguide/automode-get-started-eksctl.html)
- [EC2 nested virtualization](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html)
- [EKS Auto Mode managed instances](https://docs.aws.amazon.com/eks/latest/userguide/automode-learn-instances.html)
