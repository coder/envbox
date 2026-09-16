# EKS managed node group nested-virtualization findings

Date: 2026-07-29 to 2026-07-30

## Executive summary

The core experiment succeeded.

An Amazon EKS managed node group running Kubernetes 1.36 launched an
`c8i.2xlarge` instance with nested virtualization enabled when the EC2 launch
template specified:

```json
{
  "CpuOptions": {
    "NestedVirtualization": "enabled"
  }
}
```

The tested node exposed the Intel virtualization extension to an ordinary
container, and a privileged container successfully opened `/dev/kvm`.

The subsequent device-plugin test exposed `/dev/kvm` to a non-privileged pod,
and Firecracker successfully booted Ubuntu 22.04 inside that pod. The same
test also succeeded with Firecracker running as UID 1000 and with two
microVMs booting concurrently in separate pods.

Networking through `/dev/net/tun` and production-readiness concerns remain
untested.

## Cluster

```yaml
name: nested-virt-mng-136
region: us-east-2
version: '1.36'
platformVersion: eks.8
status: ACTIVE
```

The cluster was created without EKS Auto Mode. The control plane initially ran
with zero worker nodes; the managed node group was added afterward.

## Managed node group

The node group was created successfully with `eksctl` and one desired node.

```yaml
name: nested-virt-mng
amiFamily: AmazonLinux2023
instanceType: c8i.2xlarge
capacityType: ON_DEMAND
launchTemplateId: lt-013959302183b2415
launchTemplateVersion: '1'
```

The node was Ready and reported:

```text
Kubernetes:       v1.36.2-eks-bca9cf6
OS:               Amazon Linux 2023.12.20260727
Kernel:           6.18.38-76.139.amzn2023.x86_64
Architecture:     amd64
Runtime:          containerd://2.2.5+unknown
Instance type:    c8i.2xlarge
```

## AMI

The launch template did not specify an `ImageId`; EKS selected the AMI based
on the managed node group configuration.

```yaml
ImageId: ami-0a3a6389d71f681a0
OwnerId: '602401143452'
Name: amazon-eks-node-al2023-x86_64-standard-1.36-v20260727
Description: EKS-optimized Kubernetes node based on Amazon Linux 2023, (k8s: 1.36.2, containerd: 2.2.5-1.amzn2023.0.4)
State: available
CreationDate: '2026-07-27T06:49:46.000Z'
```

This was an AWS-published, EKS-optimized Amazon Linux 2023 AMI. No custom AMI
was built or supplied, and no AMI contents were modified. The only custom EC2
setting was the launch-template CPU option enabling nested virtualization.

Whether this AMI satisfies a customer-specific “certified” or “approved AMI”
policy remains a separate customer-policy question.

## EC2 nested-virtualization setting

The launch template initially contained an incorrect outer
`LaunchTemplateData` JSON wrapper for the AWS CLI. After correcting the JSON,
the template was created successfully:

```yaml
LaunchTemplateId: lt-013959302183b2415
Version: 1
InstanceType: c8i.2xlarge
CpuOptions:
  NestedVirtualization: enabled
ImageId: null
```

The resulting instance reported:

```yaml
InstanceId: i-0f5ed2c3b07660570
InstanceType: c8i.2xlarge
ImageId: ami-0a3a6389d71f681a0
State: running
CpuOptions:
  CoreCount: 4
  NestedVirtualization: enabled
  ThreadsPerCore: 2
```

The EC2 response is the authoritative confirmation that nested virtualization
was enabled for the instance.

## Ordinary-container probe

The probe pod was non-privileged and did not explicitly mount host devices.
Its output was:

```text
--- CPU virtualization flags ---
vmx
--- /dev/kvm ---
ls: cannot access '/dev/kvm': No such file or directory
--- /dev/net/tun ---
ls: cannot access '/dev/net/tun': No such file or directory
```

Interpretation:

- `vmx` was visible inside an ordinary container, confirming that the CPU
  virtualization extension crossed the container boundary.
- `/dev/kvm` was not automatically present, as expected. Kubernetes does not
  inject host devices into ordinary pods without an explicit device or device
  plugin mechanism.

## Privileged `/dev/kvm` probe

The second probe used:

- `securityContext.privileged: true`;
- a hostPath mount for `/dev/kvm`; and
- no device plugin.

The pod completed successfully with exit code 0. Its output was:

```text
+ ls -l /dev/kvm
crw-rw-rw-. 1 root 36 10, 232 Jul 29 05:52 /dev/kvm
+ exec
+ echo 'opened /dev/kvm read/write'
opened /dev/kvm read/write
+ exec
```

This proves that a privileged container on the node can open `/dev/kvm`.
It does not prove that an unprivileged workspace pod can access KVM because
the probe used broad privilege and an explicit hostPath mount.

## Conclusions

### Confirmed

1. EKS managed node groups can use a customer-supplied EC2 launch template
   with `CpuOptions.NestedVirtualization=enabled`.
2. The configuration works on Kubernetes 1.36 with an EKS-optimized AL2023 AMI.
3. The tested `c8i.2xlarge` instance exposes `vmx` to ordinary containers.
4. A privileged container can mount and open `/dev/kvm`.
5. A purpose-built Kubernetes device plugin can advertise `/dev/kvm` as a
   schedulable resource.
6. A non-privileged container can receive and open `/dev/kvm` through that
   device-plugin path.
7. The EC2/EKS managed-node-group infrastructure is suitable for further
   KVM-backed microVM testing.
8. Firecracker v1.15.1 can boot an Ubuntu 22.04 microVM from a non-privileged
   pod using the exposed KVM device.
9. Firecracker can run as UID 1000 in the pod when the writable rootfs is
   copied into `/tmp`.
10. Two Firecracker microVMs can boot concurrently in separate pods using
    separately allocated logical KVM resources.
11. A non-privileged pod can receive `/dev/net/tun`, create `tap0` with only
    `CAP_NET_ADMIN`, attach Firecracker guest `eth0` to it, and boot a guest
    configured as `172.16.0.2/24` with gateway `172.16.0.1`.

### Not yet confirmed

The experiments have not yet validated a production-grade device plugin,
network isolation, node replacement, or a real workload running inside the
guest. The networking test configured a guest interface successfully, but did
not yet verify guest-to-host or guest-to-internet packet flow.

## Unprivileged device-plugin probe

A purpose-built device plugin was built and pushed to ECR, then deployed as a
privileged node-level DaemonSet. It registered:

```text
devices.coder.com/kvm
```

The probe pod requested one unit of that resource. The pod had:

- no `privileged: true`;
- no hostPath volume;
- no hostPID;
- no hostNetwork;
- `RuntimeDefault` seccomp; and
- all Linux capabilities dropped.

Kubernetes reported the resource in node Capacity and Allocatable, scheduled
the pod, and the pod completed successfully. Its output was:

```text
uid=0(root) gid=0(root) groups=0(root)
--- injected KVM device ---
crw-rw-rw-. 1 root 36 10, 232 Jul 30 05:52 /dev/kvm
opened /dev/kvm read/write without privileged mode
```

This proves the proposed **non-privileged-container** device-plugin path can
inject and use `/dev/kvm`. The probe itself runs as UID 0 inside the container;
that is distinct from Kubernetes `privileged: true`. The subsequent
Firecracker test also succeeded with the VMM running as UID 1000.

## Purpose-built device plugin

The device plugin was implemented specifically for this experiment rather
than relying on an external generic plugin. It uses the Kubernetes-generated
device-plugin gRPC API from `k8s.io/kubelet`, registers the resource
`devices.coder.com/kvm`, reports healthy logical devices, and returns a device
spec mapping host `/dev/kvm` to container `/dev/kvm`. It initially reported
one device; it was then configured to report two logical allocations for the
concurrent-microVM test. Both allocations refer to the same host device.

The plugin image was built with Go 1.26 and pushed to the experiment's ECR
repository. The DaemonSet is privileged because it must register with kubelet
and access the host device; the workload pod is not privileged.

The first plugin image failed because it did not implement the required
`GetDevicePluginOptions` RPC. After adding that method, the second image
registered successfully:

```text
registered devices.coder.com/kvm using /dev/kvm
```

## Firecracker smoke test

The test uses Firecracker v1.15.1. The `firecracker-smoke/` directory
contains a Dockerfile that packages Firecracker, an official Firecracker CI
kernel, and an ext4 guest rootfs, plus a script and an unprivileged pod
manifest requesting only `devices.coder.com/kvm`.

The smoke test intentionally omits networking and therefore does not request
`/dev/net/tun`. A successful run prints:

```text
FIRECRACKER_INSTANCE_START_SUCCEEDED
```

The image was built and pushed to ECR, then run in a pod with no
`privileged: true`, no host namespace sharing, no hostPath mount, and the
`RuntimeDefault` seccomp profile. The pod requested one
`devices.coder.com/kvm` resource from the purpose-built device plugin.

The test succeeded:

```text
FIRECRACKER_INSTANCE_START_SUCCEEDED
```

The Firecracker log reported version 1.15.1, and the guest kernel log showed:

```text
Hypervisor detected: KVM
Booting paravirtualized kernel on KVM
Mounted root ... on device 254:0
Welcome to Ubuntu 22.04.5 LTS!
```

This demonstrates that a Firecracker microVM can boot inside a non-privileged
Kubernetes pod on an EKS managed-node-group AL2023 node when KVM is enabled
through the EC2 launch template and exposed through a Kubernetes device
plugin.

The initial smoke test intentionally omitted networking and `/dev/net/tun`.
Separate tests confirmed concurrent microVMs, operation as a non-root process,
and guest network-interface setup inside the pod. The guest rootfs was the official
Firecracker CI Ubuntu 22.04 ext4 image, and the kernel was the official
Firecracker CI `vmlinux-6.1.102` artifact.

### Non-root Firecracker

The test was repeated with the Firecracker container configured with
`runAsUser: 1000`, `runAsGroup: 1000`, and `runAsNonRoot: true`, while
retaining dropped capabilities and the `RuntimeDefault` seccomp profile.
Because the packaged rootfs is not writable by UID 1000, the test copied it
to `/tmp` and used that writable copy as the VM drive.

This also succeeded, including guest boot to Ubuntu/systemd. The basic
microVM path therefore does not require the VMM container to run as root.

### Concurrent microVMs

The device plugin was configured to advertise two KVM allocations, both
backed by the same host `/dev/kvm` device. Two separate non-privileged pods
were then started concurrently. Both logs recorded
`FIRECRACKER_INSTANCE_START_SUCCEEDED`, `Hypervisor detected: KVM`, and a
successful Ubuntu 22.04.5 guest boot. This demonstrates that multiple
microVMs can run concurrently on the node through separately scheduled pods.

Both logs also contain Firecracker's `MissingAddressRange` I/O warning during
guest startup; it did not prevent either guest from booting.

### Networked Firecracker

A separate TUN device plugin advertised `/dev/net/tun` as
`devices.coder.com/tun`. The workload pod requested both KVM and TUN devices,
used only `CAP_NET_ADMIN` in addition to its dropped capabilities, created
`tap0`, and attached it to Firecracker's guest `eth0`.

The guest log confirmed:

```text
virtio_net ... Assigned random MAC address
device=eth0 ... ipaddr=172.16.0.2, mask=255.255.255.0, gw=172.16.0.1
```

This confirms TUN allocation, TAP creation, Firecracker network-interface
attachment, and guest interface configuration. Actual packet flow beyond the
guest interface was not tested.

## Relationship to envbox/Sysbox

Nested virtualization is not required by envbox/Sysbox. Envbox uses a
privileged outer container to run Sysbox and an unprivileged inner workspace;
the inner workspace does not need `/dev/kvm`.

The unprivileged device-plugin and microVM tests are therefore relevant to the
alternative microVM architecture, not to establishing basic envbox
compatibility.

## Evidence files

Expected evidence in this directory includes:

- `cluster-version.yaml`
- `launch-template-data.json`
- `launch-template.yaml`
- `launch-template-version.yaml`
- `managed-nodegroup.yaml`
- `managed-nodegroup.actual.yaml`
- `node-summary.txt`
- `node.yaml`
- `ec2-instance.yaml`
- `cpu-probe.log`
- `kvm-probe.describe.txt`
- `kvm-probe.log`

The following files record the device-plugin test:

- `device-plugins/`
- `kvm-device-plugin-daemonset.actual.yaml`
- `node-device-plugin.describe.txt`
- `nested-virt-kvm-unprivileged-probe.yaml`
- `kvm-unprivileged-probe.describe.txt`
- `kvm-unprivileged-probe.log`
- `tun-device-plugin-daemonset.actual.yaml`

The following files record the Firecracker test:

- `firecracker-smoke/`
- `firecracker-smoke/pod.actual.yaml`
- `firecracker-smoke/pod-nonroot.actual.yaml`
- `firecracker-smoke/pods-concurrent.actual.yaml`
- `firecracker-smoke/pod-network.actual.yaml`
- `firecracker-smoke.log`
- `firecracker-nonroot.log`
- `firecracker-concurrent-a.log`
- `firecracker-concurrent-b.log`
- `firecracker-network.log`
