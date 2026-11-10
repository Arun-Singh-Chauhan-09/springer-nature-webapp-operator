variable "repo_url" {
  description = "Git repository ArgoCD syncs from"
  type        = string
}

variable "target_revision" {
  description = "Branch, tag or commit ArgoCD tracks"
  type        = string
  default     = "main"
}

variable "apps_path" {
  description = "Path in the repo holding the child Application manifests (app-of-apps)"
  type        = string
  default     = "gitops/apps"
}

variable "argocd_chart_version" {
  description = "argo-cd Helm chart version (pinned for reproducible installs)"
  type        = string
  default     = "7.7.0"
}

variable "argocd_apps_chart_version" {
  description = "argocd-apps Helm chart version, used to create the root Application"
  type        = string
  default     = "2.0.0"
}
