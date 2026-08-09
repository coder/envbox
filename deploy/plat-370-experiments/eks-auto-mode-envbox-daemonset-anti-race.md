# Preventing the Envbox sysctl-preparation race on EKS Auto Mode

## Question

Dogfood retains a privileged DaemonSet manifest from an earlier approach. It
writes the node-global sysctls required by Envbox/Sysbox (notably
`user.max_*_namespaces`). Could that approach use the same anti-race idea as
the Sysbox Kubernetes installer so new EKS Auto Mode nodes are prepared before
a workspace starts there?

## Short answer

Yes in principle, but a DaemonSet that is merely small or high priority is not
an ordering guarantee. A correct design needs a scheduling/readiness gate
between node creation and workspace scheduling.

The retained DaemonSet manifest documents the race explicitly: a workspace may
be scheduled to a newly provisioned node before the node-prep DaemonSet has
written the sysctls. The workspace then fails; the practical mitigation is only
that the small DaemonSet image usually starts before the much larger Envbox
image. This reduces the probability; it does not remove the race.

It is not the current Dogfood mechanism. Current workspace Pods are pinned to
a dedicated Managed Node Group (MNG), whose cloud-init applies the sysctls
before kubelet starts. The DaemonSet is relevant as an Auto Mode experiment or
alternative, not as the deployed Dogfood path.

## The upstream Sysbox pattern

The full `sysbox-deploy-k8s` installer handles installation/update on ordinary
nodes with a stricter sequence:

1. Add `sysbox-runtime=not-running:NoSchedule` to the node.
2. Install and verify the runtime.
3. Add the readiness label `sysbox-runtime=running`.
4. Remove the temporary taint.
5. Have the `sysbox-runc` RuntimeClass select only nodes with the readiness
   label.

The key is not RuntimeClass alone. RuntimeClass can merge a node selector and
tolerations into a Pod, but it does not wait for a DaemonSet. The taint plus
the label publication protocol establishes the ordering.

## Applying that idea to Envbox

Envbox currently runs as a normal privileged workspace Pod, not as a
RuntimeClass. The equivalent protocol would be:

1. A custom EKS Auto Mode NodePool declares a bootstrap **startup taint**, for
   example `coder.com/envbox-sysctls=not-ready:NoSchedule`. The resulting new
   node receives that taint, along with a stable pool label such as
   `coder.com/workspace-pool=true`. The prep DaemonSet, but not workspace
   Pods, tolerates the startup taint.
2. The privileged `sysbox-node-prep` DaemonSet selects that stable pool label,
   runs there, writes the required values, and reads them back to verify them.
   The retained Dogfood manifest already performs the writes; the verification
   and following node mutations are the proposed extension.
3. Only after verification, the DaemonSet removes the bootstrap taint. It may
   additionally publish a diagnostic `coder.com/envbox-sysctls=ready` label.
   This requires permission to patch Nodes. A normal DaemonSet's service
   account cannot use ordinary RBAC alone to express “patch only the node to
   which I was assigned”, so this permission needs careful design and a
   defensive check of the assigned node before mutation.
4. Workspace Pods select a stable pool label, such as
   `coder.com/workspace-pool=true`; they do **not** select the post-prep label.
   The startup taint is the readiness gate: it blocks the Kubernetes scheduler
   until preparation is complete.

The implementation must also cover node replacement and DaemonSet upgrades:
do not remove the gate until the required version of node preparation is known
to be complete.

## Dynamic-provisioning circularity

There is an extra issue on a dynamically provisioned EKS Auto Mode pool.

If a workspace Pod requires `coder.com/envbox-sysctls=ready`, a provisioner may
not create a node for it: the NodePool cannot truthfully advertise that label
before the DaemonSet has run. Conversely, if the workspace Pod tolerates an
ordinary `NoSchedule` bootstrap taint so that it can trigger node creation, it
can also schedule before the DaemonSet removes the taint. That recreates the
race. The workspace should instead select a stable NodePool label and leave
the bootstrap startup taint untolerated.

The desirable primitive is a **startup taint**: provisioning understands it is
temporary and may create a node for an otherwise incompatible workload, while
the Kubernetes scheduler keeps that workload off the node until the prep
DaemonSet removes the taint. In a custom EKS Auto Mode NodePool, configure it
as `spec.template.spec.startupTaints`; this is distinct from ordinary
`spec.template.spec.taints`. Confirm the installed NodePool CRD accepts the
field with a server-side dry run before relying on it.

## Fallbacks if startup taints cannot be used

### 1. Prewarmed static capacity

Run a small static EKS Auto Mode NodePool. The DaemonSet prepares its nodes
before users need them. Here, unlike the dynamically provisioned case,
workspaces can select a post-prep ready label: the static pool has already
created the nodes, so the label is not needed to trigger provisioning. This is
a simple correctness solution, at the cost of baseline nodes and no
scale-to-zero for that capacity. EKS Auto Mode supports NodePools with a fixed
`replicas` count.

### 2. Make workspace startup wait for preparation

Keep the DaemonSet, but make the workspace/Envbox startup wait and retry until
the required sysctls have been verified. The check can use the actual sysctl
values and/or a trusted node-ready marker. This eliminates a user-visible hard
failure but does not prevent the Pod from being scheduled first; it trades the
race for startup latency and needs a timeout/error path.

### 3. Keep the dedicated Managed Node Group

The current Dogfood MNG is the strongest existing solution: cloud-init writes
the sysctls before kubelet starts, so the first workspace cannot observe an
unprepared node. It also avoids relying on a runtime DaemonSet and the
additional Auto Mode compatibility validation that approach needs.

## What this does not establish

This protocol addresses only the sysctl-preparation race. It does not prove
that Envbox is supported on EKS Auto Mode/Bottlerocket. That still needs a
smoke test of:

- admission of the privileged node-prep DaemonSet;
- successful host-visible writes of the required node-global sysctls;
- the Envbox outer container's required host mounts and Sysbox lifecycle;
- behavior after Auto Mode node rotation.

Directly installing Sysbox on Bottlerocket is a broader and separate question:
the upstream installer modifies host runtime configuration and files, while
Envbox bundles Sysbox inside its outer Pod.

## Evidence and references

- Retained Dogfood race explanation and privileged sysctl DaemonSet manifest:
  `~/dogfood/clusters/dogfood-workspaces/coder/workspaces-namespace/daemonset-sysbox-node-prep.yaml`
- Current Dogfood MNG boot-time sysctl configuration:
  `~/dogfood/terraform/workspace-clusters.tf`
- Sysbox taint/label protocol:
  `~/sysbox/sysbox-pkgr/k8s/scripts/sysbox-deploy-k8s.sh` and
  `~/sysbox/sysbox-pkgr/k8s/manifests/runtime-class/sysbox-runtimeclass.yaml`
- Kubernetes RuntimeClass scheduling:
  <https://kubernetes.io/docs/concepts/containers/runtime-class/>
- Karpenter `startupTaints` semantics and NodePool example:
  <https://karpenter.sh/v1.0/concepts/nodepools/>
- EKS Auto Mode custom NodePool configuration:
  <https://docs.aws.amazon.com/eks/latest/userguide/create-node-pool.html>
- EKS Auto Mode static-capacity NodePools:
  <https://docs.aws.amazon.com/eks/latest/userguide/ml-node-pools.html>
