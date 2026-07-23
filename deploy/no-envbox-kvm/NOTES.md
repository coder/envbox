# Notes: open questions and mapping to existing envbox code

## Make-or-break tests (run these before committing to the approach)

1. **Does `/dev/kvm` exist and work in the target node group?**
   On the node: `ls -l /dev/kvm` and a `cloud-hypervisor`/`qemu -enable-kvm`
   smoke boot. On AWS this means `*.metal` or `c8i/m8i/r8i` + nested virt.

2. **Does the device-plugin path avoid `privileged` for the workspace pod?**
   Confirm a pod requesting `devices.kubevirt.io/kvm` can open `/dev/kvm` and
   boot a guest with NO `privileged: true` and NO `hostPID/hostNetwork`.

3. **Bottlerocket: does `super_t` + device plugin cover the VMM's syscalls?**
   Boot the VMM pod on a Bottlerocket node and watch `dmesg`/journal for
   SELinux AVC denials on the KVM ioctls, `mmap`, and tun ops. If `super_t`
   plus the device plugin covers them -> first-class on stock Bottlerocket.
   If a specific op is still denied -> a custom Bottlerocket variant with a
   purpose-built SELinux type is required (heavier; reintroduces OS-image
   maintenance).

## Mapping to today's envbox code (what changes / goes away)

| Envbox today | No-envbox + KVM |
| --- | --- |
| `cli/docker.go` starts sysbox + inner dockerd via `sysbox-runc` | Replaced by an in-pod supervisor that launches the VMM and boots the guest. |
| `xunix/device.go` `mknod`s `/dev/net/tun`, `/dev/fuse` under privileged | Devices come from the device plugin as pod resource requests. |
| `envboxPrivateMounts`: `/lib/modules`, `/usr/src`, `/var/lib/sysbox` | Not needed. The guest ships its own kernel/modules; no host kernel matrix. |
| `--privileged` outer pod | Unprivileged pod + scoped device request. |
| Inner dockerd image store (cold cache) | Persistent guest `/var/lib/docker` PVC + registry mirror. |
| `xunix/gpu.go` host-lib bind mounts + `CODER_ADD_GPU` | GPU passthrough into the guest (open question; `*.metal` likely first). |
| `CODER_INNER_*` env contract | Preserved at the supervisor boundary so templates barely change. |

## Preserve the Coder-facing contract

Keep `CODER_INNER_IMAGE`, `CODER_INNER_USERNAME`, `CODER_INNER_ENVS`,
`CODER_AGENT_TOKEN`, `CODER_MOUNTS`, etc. at the supervisor boundary so
existing envbox templates migrate with minimal edits. The isolation mechanism
changes underneath; the template surface should not.

## Biggest unknowns (ranked)

1. Guest rootfs build + boot time vs. envbox cold start (target: comparable or
   better once the docker cache is warm).
2. Networking: virtio-net + NAT vs. the pod's CNI; making NetworkPolicy /
   service mesh apply to guest traffic.
3. Passing PVC-backed volumes (home dir) into the guest with correct
   ownership (virtiofs).
4. GPU passthrough into a nested-virt guest on non-metal families.
5. Graceful shutdown / signal + agent-token lifecycle into the guest.

## Not doing (yet)

No code wired into the envbox build. This branch is a deployment-surface
sketch to explore options and evaluate feasibility and node/OS constraints.
