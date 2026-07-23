// Coder template sketch: KVM-backed workspace pod (no envbox).
//
// This is the Terraform analog of manifests/workspace-pod.yaml, showing how a
// Coder template would provision the pod. It is intentionally trimmed to the
// bits that differ from the existing kubernetes-envbox template: no
// runtime_class_name, no privileged, device resources instead.
//
// SKETCH ONLY -- not runnable as-is.

resource "kubernetes_pod" "workspace" {
  count = data.coder_workspace.me.start_count

  metadata {
    name      = "coder-${data.coder_workspace_owner.me.name}-${data.coder_workspace.me.name}"
    namespace = var.namespace
  }

  spec {
    # Land on KVM-capable nodes (*.metal or c8i/m8i/r8i + nested virt).
    node_selector = {
      "coder.com/kvm" = "true"
    }

    toleration {
      key      = "coder.com/workspace-kvm"
      operator = "Exists"
      effect   = "NoSchedule"
    }

    # No runtime_class_name (customers can't install one).
    # No privileged security_context.
    security_context {
      seccomp_profile {
        type = "RuntimeDefault"
      }
    }

    container {
      name  = "workspace"
      image = "REPLACE_ME/coder-workspace-vmm:latest"

      # KVM requested as a device, not via privileged.
      resources {
        requests = {
          "devices.kubevirt.io/kvm" = "1"
          "devices.kubevirt.io/tun" = "1"
          "cpu"                     = "2"
          "memory"                  = "4Gi"
        }
        limits = {
          "devices.kubevirt.io/kvm" = "1"
          "devices.kubevirt.io/tun" = "1"
          "cpu"                     = "4"
          "memory"                  = "8Gi"
        }
      }

      env {
        name  = "CODER_INNER_IMAGE"
        value = data.coder_parameter.inner_image.value
      }
      env {
        name  = "CODER_AGENT_TOKEN"
        value = coder_agent.main.token
      }

      volume_mount {
        name       = "docker-cache"
        mount_path = "/var/lib/coder/docker"
      }
    }

    # Persistent guest docker cache -> fixes cold pulls.
    volume {
      name = "docker-cache"
      persistent_volume_claim {
        claim_name = kubernetes_persistent_volume_claim.docker_cache.metadata.0.name
      }
    }
  }
}
