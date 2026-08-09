# EKS Docker-in-Docker workspace solutions matrix

Last updated: 2026-08-09

## Goal

Identify practical ways to provide Coder workspaces with rootful Docker,
BuildKit, systemd, Compose networking, and nested containers on Amazon EKS.
Customers differ in the EKS versions, node operating systems, AMI controls,
and privileged-workload policies they permit.

This document distinguishes completed experiments from proposed solutions.

## Executive recommendation

| Customer constraint | Recommended direction |
| --- | --- |
| EKS Auto Mode with AWS-managed Bottlerocket is required, and a privileged outer workspace container is acceptable | Envbox |
| An older EKS version or node runtime without usable Pod user namespaces is required, and a privileged outer workspace container is acceptable | Envbox; it creates the user namespace and private runtime itself |
| Privileged workspace containers are prohibited, but a mutable AL2023 managed node group and trusted node runtime extension are acceptable | Direct Sysbox through `RuntimeClass`; the AL2023 POC passed PSS `restricted` enforcement |
| Installing Sysbox node daemons is unacceptable, but a custom AL2023 MNG runtime handler and broader namespaced Pod permissions are acceptable | Native Kubernetes user namespaces with a dedicated `cgroup_writable` runtime |
| EKS Auto Mode/Bottlerocket and no privileged Pods are both hard requirements | No validated rootful-DinD solution |
| VM-grade isolation is required and a configurable MNG is acceptable | Continue the Firecracker/microVM path; it is not yet a complete Coder workspace solution |

## Solution comparison

| Approach | Node requirements | Workspace security shape | Main advantages | Main costs and limitations | Evidence status |
| --- | --- | --- | --- | --- | --- |
| Envbox | Works on the tested EKS 1.36 Auto Mode Bottlerocket nodes after node-global namespace sysctls are prepared | Privileged outer Envbox container; unprivileged Sysbox `workspace_cvm` inner container with a writable Sysbox-managed cgroup view | Works without installing a runtime into the immutable host; complete Docker, BuildKit, networking, resource-limit, persistence, and replacement-node workflow passed; a separate node-local inner-image cache POC also passed | Requires a privileged outer container and privileged node-preparation DaemonSet; carries private Docker/containerd/Sysbox daemons per workspace; the kubelet image cache covers only the outer Envbox image, so efficient inner-image reuse requires a separate node-local cache populated independently on every node; systemd has nonblocking degraded units | **Passed** for published Envbox 0.6.7 on the tested Auto Mode/Bottlerocket release |
| Native Kubernetes user namespace plus rootful DinD | Configurable EKS 1.36 AL2023 MNG; custom containerd handler with `cgroup_writable = true`; prepared domain-cgroup topology | `hostUsers: false`, `privileged: false`; runtime-default capabilities plus namespaced `CAP_SYS_ADMIN` and `CAP_NET_ADMIN`; unmasked `/proc`, unconfined seccomp, a directly delegated writable cgroup subtree, and privilege escalation | No Sysbox or privileged workspace container; smaller trusted stack; Docker, BuildKit, Compose networking, cross-node Service/Pod-IP access, limits, and EBS storage passed | Depends on a custom node runtime handler and entrypoint cgroup choreography; exposes more raw namespaced kernel interfaces directly to the workspace and relies heavily on the user namespace as the security boundary; requires further security and lifecycle testing | **Technically passed** on the tested EKS 1.36 MNG; `ALL` capabilities were proven unnecessary; not yet production-ready |
| Native user namespace on Auto Mode/Bottlerocket | AWS-managed Auto Mode node | Intended non-privileged user-namespaced workspace | Would avoid both Envbox and host-installed Sysbox | Initial user namespaces required privileged sysctl preparation; after preparation, Auto Mode still exposed no writable cgroup delegation equivalent to the MNG runtime handler | **Failed** for rootful DinD on the tested Auto Mode/Bottlerocket runtime |
| Direct node-installed Sysbox | Dedicated mutable EKS MNG; tested on EKS 1.35, AWS-managed AL2023, kernel 6.12, and containerd 2.2.5; requires the experimental AL2023 installer fork or an equivalent custom AMI | `runtimeClassName: sysbox-runc`, `hostUsers: false`, `privileged: false`; Pod spec passed PSS `restricted` with capability drop `ALL`, runtime-default seccomp, and no privilege escalation; Sysbox supplies system-container capabilities inside the private user namespace and a writable managed cgroup view | No privileged workspace Pod; systemd, Docker, BuildKit, Compose, persistence, concurrency, isolation, cross-node access, and new-node installation passed; node runtime/cache is shared; fewer per-workspace daemons and less nesting than Envbox | Privileged node installer and host runtime restart unless baked into AMI; trusted node daemons; requires modern Pod-userns support; does not work on Auto Mode/Bottlerocket; AL2023 support is a downstream POC, not upstream support; security/lifecycle review remains | **Functional POC passed** on EKS 1.35 AL2023 MNG under actual PSS `restricted` enforcement |
| Direct Sysbox on Auto Mode/Bottlerocket | Would require modifying the AWS-managed immutable node OS and container runtime | Workspace could be non-privileged if installation were possible | Same theoretical workspace benefits as direct Sysbox on an MNG | Conflicts with the Auto Mode managed-instance contract and the Sysbox installer model; AWS controls the AMI, OS, and runtime and says software cannot be installed directly | **Do not prioritize**; expected to be unsupported rather than a useful customer solution |
| Firecracker microVM | EKS MNG with a launch template enabling EC2 nested virtualization, plus KVM/TUN device plugins | Non-privileged VMM Pod; Firecracker also ran as UID 1000 | Strongest isolation boundary among the tested directions; multiple concurrent microVMs booted | Requires custom EC2 launch settings, privileged node device plugins, guest image lifecycle, networking, Coder agent integration, and VM orchestration | **Core infrastructure passed** on MNG; complete networked Coder workspace remains untested. Auto Mode did not enable nested virtualization |

## Direct Sysbox's incremental customer coverage

The completed experiment shows that direct Sysbox adds real coverage on an
AWS-managed **AL2023 managed node group**, not only on an Ubuntu custom AMI.
The experimental installer fork configured Sysbox alongside EKS containerd
2.2.5, and the same privileged DaemonSet automatically prepared a second node
added by MNG scale-up.

Its strongest differentiator is its workspace policy shape. The tested
workspace ran systemd and rootful Docker while its Kubernetes specification
satisfied actual PSS `restricted` enforcement:

- `privileged: false`;
- `allowPrivilegeEscalation: false`;
- `capabilities.drop: ["ALL"]`; and
- `seccompProfile.type: RuntimeDefault`.

That covers customers who reject Envbox's privileged outer workspace Pod and
also reject the native-userns prototype's explicit `SYS_ADMIN` and
`NET_ADMIN`, unmasked `/proc`, unconfined seccomp, and privilege escalation.
The tradeoff is accepting a privileged installer and trusted Sysbox services
on every selected node.

Direct Sysbox still needs a writable cgroup hierarchy and broad system-
container powers. Sysbox supplies them inside the workspace's private user
namespace even though the Pod requests no capabilities. This is a more
mediated interface and a cleaner admission-policy result, but it is not proof
of categorical security superiority. Sysbox, its host mounts and daemons, the
namespaced capability set, writable cgroups, and the observed SELinux posture
all remain in the trusted computing base and need review.

Direct Sysbox is relevant when all of the following are true:

1. Privileged workspace Pods are unacceptable.
2. A dedicated mutable managed node group is acceptable.
3. A reviewed privileged installer or versioned custom AMI is acceptable.
4. The cluster and host runtime support `hostUsers: false` Pods.
5. The customer accepts a downstream AL2023 installer patch until equivalent
   support is upstream.

Within that segment it provides benefits the other tested approaches do not
provide simultaneously:

- PSS `restricted`-compliant workspace specifications with functional rootful
  Docker, BuildKit, Compose, systemd, and persistent storage;
- Sysbox virtualization of system-container `/proc` and `/sys` behavior;
- one node-level Sysbox stack instead of Envbox's per-workspace privileged
  outer container and private runtime stack;
- normal node containerd image/snapshot reuse, without Envbox's separate
  node-local inner-image archive cache; and
- explicit `RuntimeClass` scheduling and node-readiness gating.

It does not cover immutable Auto Mode/Bottlerocket nodes or policies that
prohibit all privileged node software. It also does not replace Envbox on
older EKS/runtime combinations without usable Pod user namespaces. Envbox's
privileged outer container creates its own user namespace and private runtime,
so it is not coupled to the Kubernetes `hostUsers: false` feature.

## Image-cache implications

Envbox's outer image is pulled and cached by the Kubernetes node runtime, but
the actual workspace image is pulled by Envbox's private Docker daemon. A new
workspace with fresh private Docker storage therefore cannot reuse the
kubelet/containerd copy of that inner image. The cache POC solved this by
storing a verified image archive in a separate node-local cache and importing
it into each fresh Envbox Docker store. That cache must be populated on each
node, remains tied to node lifetime and scheduling locality, and needs its own
capacity, integrity, eviction, and concurrency design for production.

Direct Sysbox does not have this extra cache layer. Kubernetes pulls the
workspace image through the host's normal containerd image store, so all
Sysbox workspaces scheduled to that node naturally reuse its image content and
unpacked snapshots. This does not make the cache durable across node
replacement, but it avoids maintaining an Envbox-specific inner-image cache
on each node.

## Why not test direct Sysbox on Auto Mode first

AWS documents Auto Mode nodes as managed instances: AWS chooses and patches
the AMI, operating system, kubelet, and container runtime, and customers cannot
directly access or install software on them. This is directly at odds with the
Sysbox installer, which installs binaries and services, changes sysctls, and
configures the host container runtime.

Envbox succeeded there precisely because it packages Sysbox and a private
container stack inside the privileged outer Pod instead of registering Sysbox
with the host CRI runtime. The only host preparation required by the tested
deployment was the namespace/inotify sysctl gate.

A direct-Sysbox Auto Mode experiment would therefore mostly document a
predictable managed-node restriction. Run it only if a customer needs explicit
negative evidence for an approval process.

## Direct Sysbox decision and remaining validation

The EKS 1.35 AL2023 experiment is complete enough to establish functional
viability. It passed installation, automatic scale-up preparation, systemd,
Docker, BuildKit, Compose, limits, persistence, concurrent isolated
workspaces, cross-node Service and Pod-IP access, and actual PSS `restricted`
enforcement.

Treat direct Sysbox as a production-candidate architecture, not a finished
customer recommendation. The next useful work is:

1. run an actual Coder agent and representative devcontainer/Testcontainers
   workflows;
2. test node reboot, replacement, MNG rolling update, and AL2023 AMI upgrade;
3. test installer upgrade, rollback, uninstall, and partial-failure recovery;
4. test density, resource exhaustion, noisy-neighbor isolation, and nested IO
   limits;
5. measure startup, memory overhead, and image-cache behavior against Envbox
   and native userns DinD;
6. perform a focused security review of the privileged installer, host mounts,
   Sysbox daemons, namespaced capabilities, writable cgroups, seccomp, SELinux,
   and cross-workspace isolation; and
7. test a versioned custom AL2023 AMI with Sysbox preinstalled if customers
   prohibit a live privileged installer.

The AL2023 changes should be treated as a maintained downstream patch until
accepted or otherwise supported upstream. A successful experiment does not
change upstream Sysbox's support statement.

## Existing experiment evidence

- [Envbox on EKS Auto Mode/Bottlerocket](eks-auto-mode-bottlerocket-envbox-experiment/findings.md)
- [Native userns rootful DinD on EKS MNG](eks-mng-amazon-linux-userns-rootful-dind-experiment/findings.md)
- [Native userns rootful DinD on Auto Mode/Bottlerocket](eks-auto-mode-bottlerocket-userns-rootful-dind-experiment/findings.md)
- [Direct Sysbox on EKS MNG/AL2023](eks-mng-amazon-linux-direct-sysbox-experiment/findings.md)
- [Nested virtualization on EKS MNG](eks-managed-node-groups-nested-virt-136/findings.md)
- [Nested virtualization on EKS Auto Mode](eks-auto-mode-nested-virt-136/findings.md)

## External references

- [AWS: EKS Auto Mode managed instances](https://docs.aws.amazon.com/eks/latest/userguide/automode-learn-instances.html)
- [AWS: customize EKS managed nodes with launch templates and custom AMIs](https://docs.aws.amazon.com/eks/latest/userguide/launch-templates.html)
- [Sysbox project and support posture](https://github.com/nestybox/sysbox)
- [Sysbox Kubernetes installation, host, version, and containerd requirements](https://github.com/nestybox/sysbox/blob/master/docs/user-guide/install-k8s.md)
- [Sysbox Kubernetes installer manifest](https://github.com/nestybox/sysbox/blob/master/sysbox-k8s-manifests/sysbox-install.yaml)
