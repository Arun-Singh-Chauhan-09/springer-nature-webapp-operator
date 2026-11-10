# Platform bootstrap: the only thing Terraform installs inside the cluster is
# ArgoCD and one root Application. Everything else is pulled from Git by ArgoCD.
# The same module is reused by every environment (kind today, EKS in Phase 5).

locals {
  # ArgoCD dropped built-in health for Application resources, so sync waves in an
  # app-of-apps would not wait for the previous wave to be Healthy. This restores it.
  application_health = <<-LUA
    hs = { status = "Progressing", message = "" }
    if obj.status ~= nil and obj.status.health ~= nil then
      hs.status = obj.status.health.status
      if obj.status.health.message ~= nil then hs.message = obj.status.health.message end
    end
    return hs
  LUA

  # Teach ArgoCD what a healthy WebApp is, using the phase the operator reports.
  # A Degraded WebApp (e.g. bad image) then shows as Degraded in the ArgoCD UI.
  webapp_health = <<-LUA
    hs = { status = "Progressing", message = "Waiting for the operator" }
    if obj.status ~= nil and obj.status.phase ~= nil then
      if obj.status.phase == "Ready" then
        hs.status = "Healthy"
      elseif obj.status.phase == "Degraded" or obj.status.phase == "Error" then
        hs.status = "Degraded"
      end
      hs.message = "phase: " .. obj.status.phase
    end
    return hs
  LUA

  argocd_values = {
    dex           = { enabled = false }
    notifications = { enabled = false }
    configs = {
      params = { "server.insecure" = true } # TLS is terminated by port-forward locally
      cm = {
        "timeout.reconciliation"                                     = "60s"
        "resource.customizations.health.argoproj.io_Application"     = local.application_health
        "resource.customizations.health.platform.example.com_WebApp" = local.webapp_health
      }
    }
  }
}

resource "helm_release" "argocd" {
  name             = "argocd"
  namespace        = "argocd"
  create_namespace = true
  repository       = "https://argoproj.github.io/argo-helm"
  chart            = "argo-cd"
  version          = var.argocd_chart_version
  wait             = true
  timeout          = 600
  values           = [yamlencode(local.argocd_values)]
}

# Root Application (app-of-apps). Created through the argocd-apps chart instead of
# a kubernetes_manifest so `terraform plan` works before the ArgoCD CRDs exist.
resource "helm_release" "root_app" {
  name       = "platform-root"
  namespace  = "argocd"
  repository = "https://argoproj.github.io/argo-helm"
  chart      = "argocd-apps"
  version    = var.argocd_apps_chart_version

  values = [yamlencode({
    applications = {
      platform-root = {
        namespace = "argocd"
        project   = "default"
        source = {
          repoURL        = var.repo_url
          targetRevision = var.target_revision
          path           = var.apps_path
        }
        destination = {
          server    = "https://kubernetes.default.svc"
          namespace = "argocd"
        }
        syncPolicy = {
          automated = { prune = true, selfHeal = true }
        }
      }
    }
  })]

  depends_on = [helm_release.argocd]
}
