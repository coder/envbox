# EKS managed node group 1.36 nested-virtualization runbook

Date prepared: 2026-07-29

## Purpose

Determine whether a standard Amazon EKS managed node group (MNG) running
Kubernetes 1.36 can launch an EC2 instance with nested virtualization enabled
through a customer-supplied EC2 launch template, and whether a container can
access `/dev/kvm` on that node.

This is the comparison experiment to the EKS Auto Mode 1.36 test. Auto Mode
selected a `c8i.2xlarge` but the resulting EC2 `CpuOptions` did not contain
`NestedVirtualization: enabled`. This experiment explicitly supplies that
setting through the launch template.

The official term for this EKS option is an **EKS managed node group**. AWS
manages the Auto Scaling group, node registration, draining, and update
workflow, while the customer can provide a launch template for supported EC2
customization.

## Scope and interpretation

This runbook answers:

1. Whether EKS accepts an MNG launch template containing
   `CpuOptions.NestedVirtualization=enabled`.
2. Whether the launched EC2 instance reports that option as enabled.
3. Whether the node kernel exposes KVM.
4. Whether a privileged diagnostic container can open `/dev/kvm`.

It does not by itself prove that an unprivileged envbox workspace or a
production microVM supervisor can run. Those require a later envbox/VMM test.

The primary test uses the EKS-optimized Amazon Linux 2023 AMI selected by EKS.
After that succeeds, the optional Bottlerocket variant can use the same EC2
launch-template CPU option to isolate the OS/runtime question.

## Prerequisites

Install and authenticate:

```bash
aws --version
kubectl version --client
eksctl version
```

The AWS identity needs permissions to create and delete EKS, EC2 launch
templates, VPC, CloudFormation, IAM, and related resources. Choose a region
where all of the following are available:

- EKS Kubernetes 1.36;
- `c8i.2xlarge` capacity;
- the selected EKS AMI type; and
- the required ECR/EC2 endpoints.

Use a disposable AWS account or an explicitly approved test account. The
cluster and node group incur charges while running.

## Set variables

Keep these variables in the same shell session for collection and cleanup.

```bash
export AWS_REGION="us-east-2"
export CLUSTER_NAME="nested-virt-mng-136"
export K8S_VERSION="1.36"
export NODEGROUP_NAME="nested-virt-mng"
export NODE_INSTANCE_TYPE="c8i.2xlarge"
export LAUNCH_TEMPLATE_NAME="nested-virt-mng-136-lt"
```

Check the account and region:

```bash
aws sts get-caller-identity
aws configure get region || true
```

## Create the Kubernetes 1.36 cluster

Create `cluster.yaml`:

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: nested-virt-mng-136
  region: us-east-2
  version: "1.36"
```

Create the control plane and configure kubectl:

```bash
eksctl create cluster -f cluster.yaml
aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME"
```

Record the cluster version. Stop if it is not 1.36.

```bash
aws eks describe-cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME" \
  --query 'cluster.{name:name,version:version,platformVersion:platformVersion,status:status}' \
  --output yaml | tee cluster-version.yaml

kubectl get nodes -o wide
```

## Create the EC2 launch template

Create `launch-template-data.json`:

```json
{
  "InstanceType": "c8i.2xlarge",
  "CpuOptions": {
    "NestedVirtualization": "enabled"
  }
}
```

The instance type is deliberately specified in the launch template. Do not
also specify a conflicting instance type in the managed node group. AWS
documents nested virtualization for 8th-generation Intel instance types,
including the `c8i` family.

Create the template and record its ID:

```bash
aws ec2 create-launch-template \
  --region "$AWS_REGION" \
  --launch-template-name "$LAUNCH_TEMPLATE_NAME" \
  --launch-template-data file://launch-template-data.json \
  --query 'LaunchTemplate.{Id:LaunchTemplateId,Name:LaunchTemplateName,Version:LatestVersionNumber}' \
  --output yaml | tee launch-template.yaml

LAUNCH_TEMPLATE_ID="$(aws ec2 describe-launch-templates \
  --region "$AWS_REGION" \
  --launch-template-names "$LAUNCH_TEMPLATE_NAME" \
  --query 'LaunchTemplates[0].LaunchTemplateId' \
  --output text)"
export LAUNCH_TEMPLATE_ID
echo "$LAUNCH_TEMPLATE_ID"

aws ec2 describe-launch-template-versions \
  --region "$AWS_REGION" \
  --launch-template-id "$LAUNCH_TEMPLATE_ID" \
  --versions '$Latest' \
  --query 'LaunchTemplateVersions[0].LaunchTemplateData.{InstanceType:InstanceType,CpuOptions:CpuOptions,ImageId:ImageId}' \
  --output yaml | tee launch-template-version.yaml
```

The launch-template output must show `NestedVirtualization: enabled` before
continuing.

## Create the managed node group

Create `managed-nodegroup.yaml`, replacing the launch-template ID with the
value from `$LAUNCH_TEMPLATE_ID`:

```yaml
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig
metadata:
  name: nested-virt-mng-136
  region: us-east-2
managedNodeGroups:
  - name: nested-virt-mng
    amiFamily: AmazonLinux2023
    desiredCapacity: 1
    minSize: 1
    maxSize: 1
    launchTemplate:
      id: REPLACE_WITH_LAUNCH_TEMPLATE_ID
      version: "1"
    labels:
      workload: nested-virt
    taints:
      - key: workload
        value: nested-virt
        effect: NoSchedule
```

Create the MNG:

```bash
sed "s/REPLACE_WITH_LAUNCH_TEMPLATE_ID/$LAUNCH_TEMPLATE_ID/" \
  managed-nodegroup.yaml > managed-nodegroup.actual.yaml

eksctl create nodegroup -f managed-nodegroup.actual.yaml
kubectl get nodes -o wide --show-labels
```

If creation fails, preserve the complete `eksctl` error. In particular,
record whether the failure is an EKS launch-template validation error or an
EC2 capacity/availability error. Do not interpret a capacity error as a
nested-virtualization result.

## Identify the test node

Unlike Auto Mode, a managed node group maintains its configured desired
capacity. The node group already has one Ready node, so a trigger pod is not
required. Select the existing node directly:

```bash
export NODE_NAME="$(kubectl get nodes \
  -l workload=nested-virt \
  -o jsonpath='{.items[0].metadata.name}')"
kubectl get node "$NODE_NAME" -o wide --show-labels | tee node-summary.txt
kubectl get node "$NODE_NAME" -o yaml > node.yaml
```

The following trigger pod is optional. Use it only if the node group has been
scaled to zero or if you need a normal workload to keep the node scheduled
while investigating it.

Create `nested-virt-trigger.yaml`:

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

Apply it and identify the node:

```bash
kubectl apply -f nested-virt-trigger.yaml
kubectl wait --for=condition=Ready pod/nested-virt-trigger --timeout=10m
export NODE_NAME="$(kubectl get pod nested-virt-trigger -o jsonpath='{.spec.nodeName}')"

kubectl get node "$NODE_NAME" -o wide --show-labels | tee node-summary.txt
kubectl get node "$NODE_NAME" -o yaml > node.yaml
```

The node should be `c8i.2xlarge`, x86-64, and a member of the managed node
group. Record the AMI, OS image, kernel, Kubernetes version, and container
runtime.

## Verify the EC2 setting

Extract the instance ID and query the authoritative EC2 state:

```bash
export INSTANCE_ID="$(kubectl get node "$NODE_NAME" \
  -o jsonpath='{.spec.providerID}' | sed 's#^.*/##')"
echo "$INSTANCE_ID"

aws ec2 describe-instances \
  --region "$AWS_REGION" \
  --instance-ids "$INSTANCE_ID" \
  --query 'Reservations[0].Instances[0].{InstanceId:InstanceId,InstanceType:InstanceType,ImageId:ImageId,State:State.Name,CpuOptions:CpuOptions,LaunchTime:LaunchTime}' \
  --output yaml | tee ec2-instance.yaml
```

This is the most important result. It must contain:

```yaml
CpuOptions:
  NestedVirtualization: enabled
```

If the field is absent or disabled, the MNG/launch-template path did not
enable nested virtualization. Do not infer the setting from the instance
family alone.

## Probe CPU flags and devices from an ordinary pod

Create `nested-virt-cpu-probe.yaml` with the following contents. For example,
run `nano nested-virt-cpu-probe.yaml`, paste the YAML below, save, and exit:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nested-virt-cpu-probe
spec:
  restartPolicy: Never
  nodeName: REPLACE_WITH_NODE_NAME
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

Replace the node name and run it:

```bash
sed "s/REPLACE_WITH_NODE_NAME/$NODE_NAME/" \
  nested-virt-cpu-probe.yaml > nested-virt-cpu-probe.actual.yaml
kubectl apply -f nested-virt-cpu-probe.actual.yaml
kubectl logs nested-virt-cpu-probe | tee cpu-probe.log
```

Missing `/dev/kvm` here is not conclusive: Kubernetes does not automatically
pass host devices into an ordinary pod.

## Probe explicit `/dev/kvm` access

This is a privileged diagnostic only. It does not represent the desired
unprivileged envbox security model.

Create `nested-virt-kvm-probe.yaml` with the following contents. For example,
run `nano nested-virt-kvm-probe.yaml`, paste the YAML below, save, and exit:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nested-virt-kvm-probe
spec:
  restartPolicy: Never
  nodeName: REPLACE_WITH_NODE_NAME
  containers:
    - name: probe
      image: public.ecr.aws/amazonlinux/amazonlinux:2023
      command: ["/bin/bash", "-c"]
      args:
        - |
          set -eux
          ls -l /dev/kvm
          exec 3<> /dev/kvm
          echo 'opened /dev/kvm read/write'
          exec 3>&-
      securityContext:
        privileged: true
      volumeMounts:
        - name: kvm
          mountPath: /dev/kvm
  volumes:
    - name: kvm
      hostPath:
        path: /dev/kvm
        type: CharDevice
```

Replace the node name and run it:

```bash
sed "s/REPLACE_WITH_NODE_NAME/$NODE_NAME/" \
  nested-virt-kvm-probe.yaml > nested-virt-kvm-probe.actual.yaml

# The generated manifest is now node-specific and is the file applied below.
kubectl apply -f nested-virt-kvm-probe.actual.yaml
kubectl describe pod nested-virt-kvm-probe | tee kvm-probe.describe.txt
kubectl logs nested-virt-kvm-probe | tee kvm-probe.log
```

Record admission, mount, runtime, and device-open errors. A successful open
demonstrates container access with privilege; it does not prove that an
unprivileged pod or a device-plugin-provided device will work.

## Test 3: KVM access from an unprivileged pod through a device plugin

The previous test used both `privileged: true` and a hostPath mount. It proves
that the node and a privileged container can use KVM, but it does not validate
the intended microVM security model.

This test verifies that Kubernetes can schedule an otherwise unprivileged pod,
inject only `/dev/kvm`, and allow the pod to open it. The device-plugin
DaemonSet itself normally requires privileged host access; the workspace pod
does not.

For this experiment, build the small purpose-built plugin included in this
directory. It registers one resource, `devices.coder.com/kvm`, and allocates
only `/dev/kvm`. The plugin DaemonSet is privileged because it must register
with kubelet and access the host device; the workspace pod remains
unprivileged.

```text
devices.coder.com/kvm
```

Build and push the plugin to ECR. Run these commands from this experiment
directory:

```bash
export AWS_ACCOUNT_ID="$(aws sts get-caller-identity \
  --query Account --output text)"
export KVM_ECR_REPOSITORY="nested-virt-kvm-device-plugin"
export KVM_IMAGE="${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com/${KVM_ECR_REPOSITORY}:test-1"

aws ecr describe-repositories \
  --region "$AWS_REGION" \
  --repository-names "$KVM_ECR_REPOSITORY" >/dev/null 2>&1 || \
aws ecr create-repository \
  --region "$AWS_REGION" \
  --repository-name "$KVM_ECR_REPOSITORY" >/dev/null

aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin \
    "${AWS_ACCOUNT_ID}.dkr.ecr.${AWS_REGION}.amazonaws.com"

docker build -f device-plugins/Dockerfile -t "$KVM_IMAGE" .
docker push "$KVM_IMAGE"
echo "$KVM_IMAGE" | tee kvm-device-plugin-image.txt
```

Render and deploy the plugin DaemonSet:

```bash
sed "s#REPLACE_WITH_ECR_IMAGE#$KVM_IMAGE#" \
  device-plugins/daemonset.yaml > kvm-device-plugin-daemonset.actual.yaml
kubectl apply -f kvm-device-plugin-daemonset.actual.yaml
kubectl get daemonset -n kube-system coder-kvm-device-plugin
kubectl get pods -n kube-system -l app=coder-kvm-device-plugin -o wide
kubectl describe node "$NODE_NAME" | tee node-device-plugin.describe.txt
```

Do not continue until the node advertises `devices.coder.com/kvm`:

```bash
kubectl describe node "$NODE_NAME" | grep -A20 -B5 'Allocatable'
```

Create `nested-virt-kvm-unprivileged-probe.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nested-virt-kvm-unprivileged-probe
spec:
  restartPolicy: Never
  nodeSelector:
    coder.com/kvm: "true"
  tolerations:
    - key: workload
      operator: Equal
      value: nested-virt
      effect: NoSchedule
  containers:
    - name: probe
      image: public.ecr.aws/amazonlinux/amazonlinux:2023
      command: ["/bin/bash", "-c"]
      args:
        - |
          set -eux
          id
          echo '--- injected KVM device ---'
          ls -l /dev/kvm
          exec 3<> /dev/kvm
          echo 'opened /dev/kvm read/write without privileged mode'
          exec 3>&-
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
        seccompProfile:
          type: RuntimeDefault
      resources:
        requests:
          devices.coder.com/kvm: "1"
        limits:
          devices.coder.com/kvm: "1"
```

The manifest intentionally has no `privileged: true`, no `hostPath`, no
`hostPID`, and no `hostNetwork`. Apply it and collect the result:

```bash
kubectl apply -f nested-virt-kvm-unprivileged-probe.yaml
kubectl describe pod nested-virt-kvm-unprivileged-probe \
  | tee kvm-unprivileged-probe.describe.txt
kubectl logs nested-virt-kvm-unprivileged-probe \
  | tee kvm-unprivileged-probe.log
```

Interpretation:

- `Insufficient devices.coder.com/kvm`: the plugin is not registered,
  unhealthy, or the node does not advertise the resource.
- pod starts but `/dev/kvm` is absent: the plugin did not inject the device.
- permission/open failure: device permissions, cgroup handling, SELinux, or
  the runtime blocked access.
- `opened /dev/kvm read/write without privileged mode`: the device-plugin
  path works and a real microVM smoke boot is justified.

This is the make-or-break Kubernetes test for the proposed unprivileged
microVM pod. It is not required for envbox/Sysbox, which uses a privileged
outer container and does not require KVM.

## Optional: test Bottlerocket instead of AL2023

Repeat the node-group portion with a second launch template or node group,
keeping the same `c8i.2xlarge` and `CpuOptions.NestedVirtualization` setting,
but change the managed node group AMI type to:

```yaml
amiFamily: Bottlerocket
```

Use a different node-group name and taint/label, or delete the AL2023 node
group first. Do not compare results unless EC2 reports
`NestedVirtualization: enabled` in both cases.

For Bottlerocket user data, use TOML and only when needed; EKS requires
Bottlerocket launch-template user data to use Bottlerocket's TOML format.
The CPU option itself belongs in the EC2 launch-template data, not in
Bottlerocket user data.

## Required evidence

Save the following in this directory:

```bash
kubectl get nodes -o wide > nodes.txt
kubectl get node "$NODE_NAME" -o yaml > node.yaml
kubectl describe pod nested-virt-trigger > trigger.describe.txt  # if used
kubectl describe pod nested-virt-cpu-probe > cpu-probe.describe.txt
kubectl describe pod nested-virt-kvm-probe > kvm-probe.describe.txt
kubectl logs nested-virt-cpu-probe > cpu-probe.log
kubectl logs nested-virt-kvm-probe > kvm-probe.log
kubectl describe pod nested-virt-kvm-unprivileged-probe > kvm-unprivileged-probe.describe.txt  # if used
kubectl logs nested-virt-kvm-unprivileged-probe > kvm-unprivileged-probe.log  # if used
aws eks describe-nodegroup \
  --region "$AWS_REGION" \
  --cluster-name "$CLUSTER_NAME" \
  --nodegroup-name "$NODEGROUP_NAME" \
  --output yaml > nodegroup.yaml
```

Also retain:

- `cluster-version.yaml`;
- `launch-template.yaml`;
- `launch-template-version.yaml`;
- `managed-nodegroup.actual.yaml`; and
- `ec2-instance.yaml`.

## Result classification

| Result | Meaning |
|---|---|
| MNG creation rejects the launch template | EKS MNG did not accept this configuration; preserve the exact validation error. |
| MNG creates, but EC2 lacks `NestedVirtualization: enabled` | The requested EC2 setting was not applied; nested virtualization is not demonstrated. |
| EC2 option enabled, but host/container lacks KVM | The EC2 setting reached the instance, but the node OS/kernel/device setup is insufficient. |
| Host KVM exists, but privileged pod mount/open fails | Kubernetes/runtime/security policy blocks device access. |
| Privileged pod opens `/dev/kvm` | EC2, node, and privileged container device access work; envbox/VMM testing is justified. |
| Unprivileged device-plugin path works | Strong evidence for a Kubernetes workload design that exposes KVM without making the workspace privileged. |
| Minimal VMM boots a guest | Strongest result; proceed to networking, storage, lifecycle, and Coder/envbox validation. |

State conclusions narrowly. A negative AL2023 or Bottlerocket result does not
automatically prove the other OS or all EKS node options are impossible.

## Cleanup

Save all evidence before deletion. Then remove the diagnostic pods and managed
node group:

```bash
kubectl delete pod nested-virt-trigger nested-virt-cpu-probe nested-virt-kvm-probe \
  nested-virt-kvm-unprivileged-probe \
  --ignore-not-found

# Only if Test 3 was deployed and you want to remove it before cluster
# deletion:
kubectl delete daemonset -n kube-system kvm-device-plugin --ignore-not-found

eksctl delete nodegroup \
  --cluster "$CLUSTER_NAME" \
  --region "$AWS_REGION" \
  --name "$NODEGROUP_NAME"
```

Delete the EC2 launch template:

```bash
aws ec2 delete-launch-template \
  --region "$AWS_REGION" \
  --launch-template-id "$LAUNCH_TEMPLATE_ID"
```

Finally delete the cluster:

```bash
eksctl delete cluster \
  --region "$AWS_REGION" \
  --name "$CLUSTER_NAME"
```

Deleting the cluster also removes Kubernetes objects such as pods and the
managed node group. The explicit node-group and pod deletion above is useful
only for orderly evidence collection and faster cleanup; it is not required
before `eksctl delete cluster`.

Check for leftover EC2 instances, EBS volumes, load balancers, NAT gateways,
and the launch template if the deletion reports warnings.

## References

- [Customize managed nodes with launch templates](https://docs.aws.amazon.com/eks/latest/userguide/launch-templates.html)
- [Simplify node lifecycle with managed node groups](https://docs.aws.amazon.com/eks/latest/userguide/managed-node-groups.html)
- [EKS managed node groups with eksctl](https://docs.aws.amazon.com/eks/latest/eksctl/nodegroup-managed.html)
- [Amazon EC2 nested virtualization](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/amazon-ec2-nested-virtualization.html)
- [EC2 launch-template CPU options](https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_LaunchTemplateCpuOptionsRequest.html)
