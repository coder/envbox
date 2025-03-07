# envbox

## Introduction

`envbox` is an image that enables creating non-privileged containers capable of running system-level software (e.g. `dockerd`, `systemd`, etc) in Kubernetes.

It mainly acts as a wrapper for the excellent [sysbox runtime](https://github.com/nestybox/sysbox/) developed by [Nestybox](https://www.nestybox.com/). For more details on the security of `sysbox` containers see sysbox's [official documentation](https://github.com/nestybox/sysbox/blob/master/docs/user-guide/security.md).

## Envbox Configuration

The environment variables can be used to configure various aspects of the inner and outer container.

| env                            | usage                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | required |
|--------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| `CODER_INNER_IMAGE`            | The image to use for the inner container.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | True     |
| `CODER_INNER_USERNAME`         | The username to use for the inner container.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | True     |
| `CODER_AGENT_TOKEN`            | The [Coder Agent](https://coder.com/docs/v2/latest/about/architecture#agents) token to pass to the inner container.                                                                                                                                                                                                                                                                                                                                                                                                            | True     |
| `CODER_INNER_ENVS`             | The environment variables to pass to the inner container. A wildcard can be used to match a prefix. Ex: `CODER_INNER_ENVS=KUBERNETES_*,MY_ENV,MY_OTHER_ENV`                                                                                                                                                                                                                                                                                                                                                                    | false    |
| `CODER_INNER_HOSTNAME`         | The hostname to use for the inner container.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | false    |
| `CODER_IMAGE_PULL_SECRET`      | The docker credentials to use when pulling the inner container. The recommended way to do this is to create an [Image Pull Secret](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#create-a-secret-by-providing-credentials-on-the-command-line) and then reference the secret using an [environment variable](https://kubernetes.io/docs/tasks/inject-data-application/distribute-credentials-secure/#define-container-environment-variables-using-secret-data). See below for example. | false    |
| `CODER_DOCKER_BRIDGE_CIDR`     | The bridge CIDR to start the Docker daemon with.                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | false    |
| `CODER_BOOTSTRAP_SCRIPT`                 | The script used to initiate the workspace startup sequence when the self-contained workspace builds feature is active; begins by downloading the "bootstrap agent" from the coderd process.
                                                                                                                                                                                                                                                                                                                                                                  | false    |
| `CODER_MOUNTS`                 | A list of mounts to mount into the inner container. Mounts default to `rw`. Ex: `CODER_MOUNTS=/home/coder:/home/coder,/var/run/mysecret:/var/run/mysecret:ro`                                                                                                                                                                                                                                                                                                                                                                  | false    |
| `CODER_USR_LIB_DIR`            | The mountpoint of the host `/usr/lib` directory. Only required when using GPUs.                                                                                                                                                                                                                                                                                                                                                                                                                                                | false    |
| `CODER_INNER_USR_LIB_DIR`      | The inner /usr/lib mountpoint. This is automatically detected based on `/etc/os-release` in the inner image, but may optionally be overridden.                                                                                                                                                                                                                                                                                                                                                                                 | false    |
| `CODER_ADD_TUN`                | If `CODER_ADD_TUN=true` add a TUN device to the inner container.                                                                                                                                                                                                                                                                                                                                                                                                                                                               | false    |
| `CODER_ADD_FUSE`               | If `CODER_ADD_FUSE=true` add a FUSE device to the inner container.                                                                                                                                                                                                                                                                                                                                                                                                                                                             | false    |
| `CODER_ADD_GPU`                | If `CODER_ADD_GPU=true` add detected GPUs and related files to the inner container. Requires setting `CODER_USR_LIB_DIR` and mounting in the hosts `/usr/lib/` directory.                                                                                                                                                                                                                                                                                                                                                      | false    |
| `CODER_CPUS`                   | Dictates the number of CPUs to allocate the inner container. It is recommended to set this using the Kubernetes [Downward API](https://kubernetes.io/docs/tasks/inject-data-application/environment-variable-expose-pod-information/#use-container-fields-as-values-for-environment-variables).                                                                                                                                                                                                                                | false    |
| `CODER_MEMORY`                 | Dictates the max memory (in bytes) to allocate the inner container. It is recommended to set this using the Kubernetes [Downward API](https://kubernetes.io/docs/tasks/inject-data-application/environment-variable-expose-pod-information/#use-container-fields-as-values-for-environment-variables).                                                                                                                                                                                                                         | false    |
| `CODER_DISABLE_IDMAPPED_MOUNT` | Disables idmapped mounts in sysbox. For more information, see the [Sysbox Documentation](https://github.com/nestybox/sysbox/blob/master/docs/user-guide/configuration.md#disabling-id-mapped-mounts-on-sysbox).                                                                                                                                                                                                                                                                                                                | false    |
| `CODER_EXTRA_CERTS_PATH`       | A path to a file or directory containing CA certificates that should be made when communicating to external services (e.g. the Coder control plane or a Docker registry)                                                                                                                                                                                                                                                                                                                                                       | false    |

## Coder Template

A [Coder Template](https://github.com/coder/coder/tree/main/examples/templates/envbox) can be found in the [coder/coder](https://github.com/coder/coder) repo to provide a starting point for customizing an envbox container.

To learn more about Coder Templates refer to the [docs](https://coder.com/docs/v2/latest/templates).

## Development

It is not possible to develop `envbox` effectively using a containerized environment (includes developing `envbox` using `envbox`). A VM, personal machine, or similar environment is required to run the [integration](./integration/) test suite.

## CODER_IMAGE_PULL_SECRET Kubernetes Example

If a login is required to pull images from a private repository, create a secret following the instructions from the [Kubernetes Documentation](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#create-a-secret-by-providing-credentials-on-the-command-line) as such:

```shell
kubectl -n <coder namespace> create secret docker-registry regcred \
        --docker-server=<your-registry-server> \
        --docker-username=<your-name> \
        --docker-password=<your-pword> \
        --docker-email=<your-email>
```

Then reference the secret in your template as such:

```shell
env {
  name = "CODER_IMAGE_PULL_SECRET"
  value_from {
    secret_key_ref {
      name = "regcred"
      key =  ".dockerconfigjson"
    }
  }
}
```

> **Note:**
>
> If you use external tooling to generate the secret, ensure that it is generated with the same fields as `kubectl create secret docker-registry`. You can check this with the following command:
>
> ```console
> kubectl create secret docker-registry example --docker-server=registry.domain.tld --docker-username=username --docker-password=password --dry-run=client --output=json | jq -r '.data[".dockerconfigjson"]' | base64 -d | jq
> ```
>
> Sample output:
>
> ```json
> {
>   "auths": {
>     "registry.domain.tld": {
>       "username": "username",
>       "password": "password",
>       "auth": "dXNlcm5hbWU6cGFzc3dvcmQ=" // base64(username:password)
>     }
>   }
> }
> ```

## GPUs

When passing through GPUs to the inner container, you may end up using associated tooling such as the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/index.html) or the [NVIDIA GPU Operator](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/index.html). These will inject required utilities and libraries inside the inner container. You can verify this by directly running (without Envbox) a barebones image like `debian:bookworm` and running `mount` or `nvidia-smi` inside the container.

Envbox will detect these mounts and pass them inside the inner container it creates, so that GPU-aware tools run inside the inner container can still utilize these libraries.

Here's an example Docker command to run a GPU-enabled workload in Envbox. Note the following:

1) The NVidia container runtime must be installed on the host (`--runtime=nvidia`).
2) `CODER_ADD_GPU=true` must be set to enable GPU-specific functionality.
3) When `CODER_ADD_GPU` is set, it is required to also set `CODER_USR_LIB_DIR` to a location where the relevant host directory has been mounted inside the outer container. In the below example, this is `/usr/lib/x86_64-linux-gnu` on the underlying host. It is mounted into the container under the path `/var/coder/usr/lib`. We then set `CODER_USR_LIB_DIR=/var/coder/usr/lib`. The actual location inside the container is not important **as long as it does not overwrite any pre-existing directories containing system libraries**.
4) The location to mount the libraries in the inner container is determined by the distribution ID in the `/etc/os-release` of the inner container. If the automatically determined location is incorrect (e.g. `nvidia-smi` complains about not being able to find libraries), you can set it manually via `CODER_INNER_USR_LIB_DIR`.

   > Note: this step is required in case user workloads require libraries from the underlying host that are not added in by the container runtime.

```shell
docker run -it --rm \
  --runtime=nvidia \
  --gpus=all \
  --name=envbox-gpu-test \
  -v /tmp/envbox/docker:/var/lib/coder/docker \
  -v /tmp/envbox/containers:/var/lib/coder/containers \
  -v /tmp/envbox/sysbox:/var/lib/sysbox \
  -v /tmp/envbox/docker:/var/lib/docker \
  -v /usr/src:/usr/src:ro \
  -v /lib/modules:/lib/modules:ro \
  -v /usr/lib/x86_64-linux-gnu:/var/coder/usr/lib \
  --privileged \
  -e CODER_INNER_IMAGE=nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda10.2 \
  -e CODER_INNER_USERNAME=root \
  -e CODER_ADD_GPU=true \
  -e CODER_USR_LIB_DIR=/var/coder/usr/lib \
  envbox:latest /envbox docker
```

To validate GPU functionality, you can run the following commands:

1) To validate that the container runtime correctly passed the required GPU tools into the outer container, run:

   ```shell
   docker exec -it envbox-gpu-test nvidia-smi
   ```

2) To validate the same inside the inner container, run:

   ```shell
   docker exec -it envbox-gpu-test docker exec -it workspace_cvm nvidia-smi
   ```

3) To validate that the sample CUDA application inside the container runs correctly:

   ```shell
   docker exec -it envbox-gpu-test docker exec -it workspace_cvm /tmp/vectorAdd
   ```

## Hacking

Here's a simple one-liner to run the `codercom/enterprise-minimal:ubuntu` image in Envbox using Docker:

```shell
docker run -it --rm \
  -v /tmp/envbox/docker:/var/lib/coder/docker \
  -v /tmp/envbox/containers:/var/lib/coder/containers \
  -v /tmp/envbox/sysbox:/var/lib/sysbox \
  -v /tmp/envbox/docker:/var/lib/docker \
  -v /usr/src:/usr/src:ro \
  -v /lib/modules:/lib/modules:ro \
  --privileged \
  -e CODER_INNER_IMAGE=codercom/enterprise-minimal:ubuntu \
  -e CODER_INNER_USERNAME=coder \
  envbox:latest /envbox docker
```

This will store persistent data under `/tmp/envbox`.

## Troubleshooting

### `failed to write <number> to cgroup.procs: write /sys/fs/cgroup/docker/<id>/init.scope/cgroup.procs: operation not supported: unknown`

This issue occurs in Docker if you have `cgroupns-mode` set to `private`. To validate, add `--cgroupns=host` to your `docker run` invocation and re-run.

To permanently set this as the default in your Docker daemon, add `"default-cgroupns-mode": "host"` to your `/etc/docker/daemon.json` and restart Docker.
