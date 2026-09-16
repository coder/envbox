# EKS Auto Mode and Docker-in-Coder Findings

Date: 2026-07-28

## Executive summary

The EKS Auto Mode experiment successfully provisioned a supported `c8i.2xlarge` node, but the node was not launched with EC2 nested virtualization enabled.

The KVM/microVM design therefore cannot currently be used on this Auto Mode setup. Existing `envbox` is a separate design: it does not use KVM, but it requires a privileged outer pod and host-kernel integration. It may be technically testable on Auto Mode, but it is not demonstrated or supported by this experiment.

## What was tested

An EKS Auto Mode cluster was created with a NodePool selecting:

```yaml
node.kubernetes.io/instance-type: c8i.2xlarge
```

The NodePool caused Auto Mode to provision:

```text
Instance:       i-03c3b8990586d371a
Instance type:  c8i.2xlarge
Zone:           us-east-2c
NodeClaim:      nested-virt-9dqzn
```

The initial probe could not pull images because the worker subnets had no default route. The VPC had private subnets and no NAT Gateway. A public subnet, Internet Gateway route, Elastic IP, and NAT Gateway were created; the private worker route table was then given a `0.0.0.0/0` route through the NAT Gateway. Image pulls succeeded afterward.

## Nested virtualization result

The probe ran successfully after networking was fixed and reported:

```text
CPU virtualization flags:
(none)

/dev/kvm:
No such file or directory

/dev/net/tun:
No such file or directory
```

The EC2 API confirmed that nested virtualization was not enabled:

```json
{
  "CoreCount": 4,
  "ThreadsPerCore": 2
}
```

An enabled instance would report `NestedVirtualization: "enabled"` in `CpuOptions`. AWS requires launching a supported instance with:

```bash
--cpu-options "NestedVirtualization=enabled"
```

Reference: [AWS nested virtualization documentation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html).

## Auto Mode configuration findings

The Auto Mode NodeClaim selected the requested `c8i.2xlarge`, but exposed no launch-template or CPU-option configuration.

The Auto Mode NodeClass contained configuration for:

- IAM role
- ephemeral storage
- security groups
- subnets
- network policy
- SNAT policy

It contained no AMI, launch-template, or `NestedVirtualization` field. The AWS EC2 `CpuOptions` response also lacked the nested virtualization field.

Therefore, selecting a compatible instance type in an Auto Mode NodePool is not sufficient to enable nested virtualization. The current Auto Mode API provides no demonstrated way to request that EC2 option.

## Implications for a KVM-backed workspace

A VMM supervisor design would need, at minimum:

- `/dev/kvm` exposed to the workspace/VMM pod
- nested virtualization enabled on the EC2 node
- a way to allocate KVM capacity between pods
- guest networking, commonly using `/dev/net/tun` or an equivalent mechanism
- a Coder-maintained guest image containing a kernel, init system, Docker, and the Coder agent

The KVM device itself can support multiple VMs concurrently. Kubernetes scheduling is the separate issue: a device plugin normally allocates extended devices exclusively and does not overcommit them. A custom resource model would be needed to represent multiple KVM-backed workspaces per node.

## Existing envbox findings

The current envbox implementation is not the KVM design. It bundles and supervises Sysbox, then runs an inner Docker daemon. Its outer pod requires:

- `privileged: true`
- creation of `/dev/net/tun` and `/dev/fuse`
- host `/lib/modules` and `/usr/src` mounts
- a compatible host kernel and Sysbox integration

Relevant source notes:

- [envbox deployment analysis](/home/geo/envbox/deploy/no-envbox-kvm/README.md:11)
- [envbox Docker invocation](/home/geo/envbox/README.md:114)

EKS Auto Mode uses AWS-managed immutable Bottlerocket nodes and does not provide host AMI customization. This makes envbox’s host-kernel assumptions operationally fragile and currently unverified, but it does not prove that envbox cannot run: envbox does not require KVM. A separate privileged-pod experiment would be required to establish whether the required privileges, mounts, device creation, and Sysbox kernel compatibility work on Auto Mode.

The exploratory `deploy/no-envbox-kvm` changes are not a finished implementation of the KVM approach. They describe a future unprivileged VMM supervisor and device-plugin model, but do not provide a complete guest image or working VMM supervisor.

## Overall conclusion

For Coder workspaces on EKS Auto Mode:

1. **KVM/microVM Docker-in-workspace:** blocked in the tested configuration because Auto Mode did not enable nested virtualization and exposes no documented NodeClass setting for it.
2. **Current envbox:** not ruled out technically, but requires a separate validation effort and is not a supported/demonstrated Auto Mode deployment model.
3. **Practical alternatives:** use a remote Docker daemon, investigate rootless Docker with its limitations, or use a conventional EKS managed/self-managed node group or EC2-based node pool where EC2 launch options and host configuration can be controlled.

The most useful comparison test is a non-Auto-Mode supported EC2 instance launched with `NestedVirtualization=enabled`, followed by the same probe. That isolates the AWS instance capability from Auto Mode’s node provisioning behavior.
