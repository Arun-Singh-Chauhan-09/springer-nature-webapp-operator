variable "cluster_name" {
  description = "kind cluster name (kubectl context becomes kind-<name>)"
  type        = string
  default     = "webapp-gitops"
}

variable "node_image" {
  description = "kind node image, i.e. the Kubernetes version"
  type        = string
  default     = "kindest/node:v1.31.0"
}

variable "repo_url" {
  description = "Git repository ArgoCD syncs from"
  type        = string
  default     = "https://github.com/Arun-Singh-Chauhan-09/springer-nature-webapp-operator.git"
}

variable "target_revision" {
  description = "Branch ArgoCD tracks"
  type        = string
  default     = "main"
}
