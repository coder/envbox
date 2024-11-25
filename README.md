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
| `CODER_MOUNTS`                 | A list of mounts to mount into the inner container. Mounts default to `rw`. Ex: `CODER_MOUNTS=/home/coder:/home/coder,/var/run/mysecret:/var/run/mysecret:ro`                                                                                                                                                                                                                                                                                                                                                                  | false    |
| `CODER_USR_LIB_DIR`            | The mountpoint of the host `/usr/lib` directory. Only required when using GPUs.                                                                                                                                                                                                                                                                                                                                                                                                                                                | false    |
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

```
kubectl -n <coder namespace> create secret docker-registry regcred \
        --docker-server=<your-registry-server> \
        --docker-username=<your-name> \
        --docker-password=<your-pword> \
        --docker-email=<your-email>
```

Then reference the secret in your template as such:

```
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
