# Cgroup-writable RuntimeClass follow-up

This follow-up keeps the existing EKS 1.36 cluster and original AL2023 MNG as
the control. It adds a second AL2023 MNG whose containerd 2.x configuration
registers a stock-runc handler with `cgroup_writable = true`.

The name `runc-cgroup-writable` is local configuration, not another runtime
binary. The handler still uses the node's stock `io.containerd.runc.v2`.

## 1. Create the second MNG

```bash
eksctl create nodegroup -f nodegroup.yaml

kubectl get nodes \
  -L eks.amazonaws.com/nodegroup,experiment.coder.com/userns-dind-cgroup-writable
```

Do not continue until the new node is `Ready` and has the expected label.

## 2. Register the RuntimeClass

```bash
kubectl apply -f runtimeclass.yaml
kubectl get runtimeclass runc-cgroup-writable -o yaml
```

## 3. Prove cgroup delegation

```bash
kubectl apply -f cgroup-probe.yaml
kubectl -n userns-rootful-dind wait \
  --for=condition=Ready pod/cgroup-writable-probe --timeout=15m
kubectl -n userns-rootful-dind logs cgroup-writable-probe
```

The required result is `cgroup-writable-probe-ok`. If `mkdir` under
`/sys/fs/cgroup` fails, stop and inspect the new node's generated containerd
configuration; do not proceed to Docker.

## 4. Run rootful Docker

Delete the probe, create the new WFFC PVC, and start the Docker Pod:

```bash
kubectl -n userns-rootful-dind delete pod cgroup-writable-probe --wait=true
kubectl -n userns-rootful-dind delete pod rootful-dind-cgroup-writable \
  --ignore-not-found --wait=true
kubectl apply -f pvc.yaml
kubectl apply -f rootful-dind.yaml
kubectl -n userns-rootful-dind wait \
  --for=condition=Ready pod/rootful-dind-cgroup-writable --timeout=15m
kubectl -n userns-rootful-dind logs -f rootful-dind-cgroup-writable
```

Stop following logs after this completion message:

```text
Domain-cgroup rootful DinD and Compose tests complete; keeping dockerd alive.
```

The Pod becoming Ready only proves that its container started; it does not
prove that the embedded Docker, BuildKit, resource-limit, and Compose tests
finished. Verify the completion artifact before interpreting the result:

```bash
until kubectl -n userns-rootful-dind exec rootful-dind-cgroup-writable -- \
  test -f /tmp/rootful-dind-test-complete; do
  sleep 2
done
```

Inspect results with:

```bash
kubectl -n userns-rootful-dind exec rootful-dind-cgroup-writable -- sh -c '
  artifacts=/workspace/docker-artifacts-cgroup-domain
  cat "$artifacts/uid-map.txt"
  cat "$artifacts/cgroup.txt"
  cat "$artifacts/root-cgroup-type.txt"
  cat "$artifacts/root-cgroup-subtree-control.txt"
  cat "$artifacts/storage-driver.txt"
  cat "$artifacts/build-result.txt"
  cat "$artifacts/resource-limit-status.txt"
  cat "$artifacts/compose-inner-network-result.txt"
  cat "$artifacts/compose-published-port-status.txt"
'
```

The successful manifest pins Docker Engine 27.5.1 and the observed Alpine and
Nginx image digests. It leaves the Compose server running with port 18080
published into the workspace Pod network namespace for the peer tests below.

## 5. Reproduce same-node and cross-node networking

The embedded checks above prove Compose service-name resolution, HTTP between
Compose services, outbound networking, and access to the published port from
the workspace itself. The companion manifest adds:

- a ClusterIP Service selecting the workspace Pod;
- a headless Service used only to discover the workspace Pod IP; and
- a restricted peer Pod pinned to the original MNG, which accesses the Nginx
  container through both the ClusterIP and the workspace Pod IP.

Delete any previous completed peer Pod, apply the manifest, and wait for the
new peer to finish:

```bash
kubectl -n userns-rootful-dind delete pod compose-network-peer-probe \
  --ignore-not-found --wait=true
kubectl apply -f compose-network-peer.yaml
kubectl -n userns-rootful-dind wait \
  --for=jsonpath='{.status.phase}'=Succeeded \
  pod/compose-network-peer-probe --timeout=10m
kubectl -n userns-rootful-dind logs compose-network-peer-probe
```

The required final line is:

```text
compose-cross-node-network-tests-ok
```

Confirm that the peer and workspace actually ran on different nodes; do not
call this a cross-node pass based only on the peer log:

```bash
workspace_node="$(kubectl -n userns-rootful-dind get pod \
  rootful-dind-cgroup-writable -o jsonpath='{.spec.nodeName}')"
peer_node="$(kubectl -n userns-rootful-dind get pod \
  compose-network-peer-probe -o jsonpath='{.spec.nodeName}')"
printf 'workspace_node=%s\npeer_node=%s\n' "$workspace_node" "$peer_node"
test "$workspace_node" != "$peer_node"

kubectl -n userns-rootful-dind get \
  service/compose-network-target \
  service/compose-network-target-headless \
  pod/rootful-dind-cgroup-writable \
  pod/compose-network-peer-probe -o wide
```

## Interpretation

- Cgroup probe fails: the node/runtime configuration did not produce a usable
  delegation boundary.
- Cgroup probe passes but Docker fails: capture `dockerd.log`; the next
  boundary is likely device, network, or controller delegation rather than
  image storage.
- Docker run/build/network/resource-limit and embedded Compose tests pass,
  but peer test fails: preserve the peer log and distinguish Service, DNS,
  Pod-IP routing, node placement, and CNI policy failures.
- All embedded and cross-node tests pass: the native-userns design is
  technically viable on a purpose-configured MNG, but the runtime-wide
  writable-cgroup handler and broad in-user-namespace security context still
  need production security and exhaustion reviews.

## Cleanup

The Service objects can be applied repeatedly. The completed peer Pod must be
deleted before each replay because a Pod's command is immutable.

```bash
kubectl delete -f compose-network-peer.yaml --ignore-not-found
kubectl delete -f rootful-dind.yaml --ignore-not-found
kubectl delete -f pvc.yaml --ignore-not-found
kubectl delete -f runtimeclass.yaml --ignore-not-found
```

Delete the `userns-dind-cgroup-writable` node group separately when the entire
experiment is complete.
