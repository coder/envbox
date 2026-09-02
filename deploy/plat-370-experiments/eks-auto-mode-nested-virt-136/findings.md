# EKS Auto Mode 1.36 nested-virtualization findings

Date: 2026-07-29

## Executive summary

The experiment concluded that the tested EKS Auto Mode Kubernetes 1.36
configuration did **not** enable EC2 nested virtualization, even when Auto Mode
was constrained to provision a supported `c8i.2xlarge` instance.

The EC2 API did not report `NestedVirtualization: enabled`. A regular pod on
the resulting node also saw no `vmx` CPU flag and no `/dev/kvm` device.

This is a negative result for the tested EKS Auto Mode provisioning path. It
does not prove that nested virtualization is impossible on Bottlerocket or on
EKS generally. In particular, it does not test a standard EKS managed node
group using an EC2 launch template that explicitly requests nested
virtualization.

## Configuration

```text
Cluster:             nested-virt-auto-136
Region:              us-east-2
Kubernetes version:  1.36
EKS platform:        eks.8
eksctl version:      0.229.0
Auto Mode:            enabled
NodePool:             nested-virt
Requested type:      c8i.2xlarge
Requested arch:      amd64
Capacity type:       on-demand
```

The cluster was created with an `eksctl` ClusterConfig containing:

```yaml
metadata:
  name: nested-virt-auto-136
  region: us-east-2
  version: "1.36"

autoModeConfig:
  enabled: true
```

The test NodePool constrained Auto Mode to:

```yaml
node.kubernetes.io/instance-type: c8i.2xlarge
kubernetes.io/arch: amd64
karpenter.sh/capacity-type: on-demand
```

## Cluster and node observations

The control plane reported:

```yaml
name: nested-virt-auto-136
platformVersion: eks.8
status: ACTIVE
version: '1.36'
```

The initially visible node was the default Auto Mode system node:

```text
OS:          Bottlerocket (EKS Auto, Standard) 2026.7.15 (aws-k8s-1.36-standard)
Architecture: arm64
Kernel:      6.18.36 (arm64)
Kubernetes:  v1.36.1-eks-a3a0722
Runtime:     containerd://2.1.9+bottlerocket
```

That node was not used for the nested-virtualization test because it was
Arm64. The test trigger pod caused Auto Mode to provision the requested node:

```text
Node:          i-01bbde7ccca753f6a
Instance type: c8i.2xlarge
Architecture:  amd64
OS:            Bottlerocket (EKS Auto, Standard) 2026.7.15 (aws-k8s-1.36-standard)
Kernel:        6.18.36 (amd64)
Kubernetes:    v1.36.1-eks-a3a0722
Runtime:       containerd://2.1.9+bottlerocket
NodePool:      nested-virt
Zone:          us-east-2a
```

The NodePool reported `NodeClassReady=True`, `ValidationSucceeded=True`, and
`Ready=True` before the trigger pod was scheduled.

## EC2 result

The instance was queried directly with `aws ec2 describe-instances`:

```yaml
CpuOptions:
  CoreCount: 4
  ThreadsPerCore: 2
ImageId: ami-05a6c95f5f2773fbf
InstanceId: i-01bbde7ccca753f6a
InstanceType: c8i.2xlarge
LaunchTime: '2026-07-29T05:05:46+00:00'
State: running
```

Crucially, the response did not contain:

```yaml
NestedVirtualization: enabled
```

For an instance launched with nested virtualization enabled, the EC2
`CpuOptions` response is expected to include that field.

## Container probe result

The probe ran as an ordinary, non-privileged Amazon Linux 2023 container on
the `c8i.2xlarge` node. Its output was:

```text
--- CPU virtualization flags ---
--- /dev/kvm ---
ls: cannot access '/dev/kvm': No such file or directory
--- /dev/net/tun ---
ls: cannot access '/dev/net/tun': No such file or directory
```

The probe saw no `vmx` or `svm` CPU flag and no `/dev/kvm` device.

The absence of `/dev/net/tun` is unrelated to the nested-virtualization
result. The absence of `/dev/kvm` in an ordinary pod is not, by itself,
conclusive because Kubernetes does not automatically pass host devices into a
pod. However, it is consistent with the decisive EC2 API result and the
missing CPU virtualization flags.

No production VMM or unprivileged KVM device-plugin configuration was tested.

## Conclusion

For EKS Auto Mode Kubernetes 1.36, in `us-east-2`, using the AWS-managed
Bottlerocket Auto Mode AMI and a `c8i.2xlarge` NodePool:

1. Auto Mode successfully provisioned the requested supported instance type.
2. Auto Mode did not enable EC2 nested virtualization.
3. The CPU virtualization extensions were not visible inside an ordinary pod.
4. `/dev/kvm` was not available inside the ordinary pod.

The supported conclusion is:

> EKS Auto Mode did not provide nested virtualization in this tested 1.36
> configuration. Selecting a compatible instance type in an Auto Mode NodePool
> was insufficient.

This does not establish that nested virtualization is impossible with:

- standard EKS managed node groups;
- an EC2 launch template containing
  `CpuOptions.NestedVirtualization=enabled`;
- Amazon Linux 2023;
- Bottlerocket outside the Auto Mode managed-instance path; or
- self-managed EKS nodes.

## Recommended comparison experiment

Run a standard EKS managed node group using the same `c8i.2xlarge` instance
type and an EC2 launch template with:

```json
{
  "CpuOptions": {
    "NestedVirtualization": "enabled"
  }
}
```

Then repeat the EC2 API, CPU-flag, `/dev/kvm`, and container-device tests. This
will isolate the Auto Mode provisioning limitation from the EC2 instance and
node-OS capabilities.

## Source evidence

- `cluster.yaml` — cluster creation configuration
- `nested-virt-nodepool.yaml` — Auto Mode NodePool constraints
- `nested-virt-trigger.yaml` — pod that triggered the `c8i.2xlarge` node
- `ec2-instance.yaml` — captured EC2 instance response
- `nested-virt-cpu-probe.yaml` — ordinary container probe
- `nested-virt-cpu-probe.actual.yaml` — node-specific applied probe
