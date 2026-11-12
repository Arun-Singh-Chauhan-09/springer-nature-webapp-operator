variable "region" {
  description = "AWS region"
  type        = string
  default     = "eu-central-1" # Frankfurt
}

variable "cluster_name" {
  description = "EKS cluster name, also used as a prefix for related resources"
  type        = string
  default     = "webapp-platform"
}

variable "kubernetes_version" {
  description = "EKS Kubernetes version"
  type        = string
  default     = "1.34"
}

variable "azs" {
  description = "Availability zones, pinned explicitly so the network never shifts when AWS adds or reorders AZs"
  type        = list(string)
  default     = ["eu-central-1a", "eu-central-1b", "eu-central-1c"]

  validation {
    condition     = length(var.azs) == 3
    error_message = "Use exactly 3 AZs (one private and one public subnet per AZ)."
  }
}

variable "vpc_cidr" {
  description = "VPC CIDR; split into 3 private (nodes) and 3 public (load balancers) subnets"
  type        = string
  default     = "10.0.0.0/16"
}

variable "node_instance_types" {
  description = "Instance types for the managed node group"
  type        = list(string)
  default     = ["t3.large"]
}

variable "admin_cidrs" {
  description = "CIDRs allowed to reach the public EKS API endpoint, e.g. your office or home IP /32"
  type        = list(string)

  validation {
    condition     = length(var.admin_cidrs) > 0 && !contains(var.admin_cidrs, "0.0.0.0/0")
    error_message = "Set admin_cidrs to specific CIDRs; the API endpoint must not be open to 0.0.0.0/0."
  }
}

variable "repo_url" {
  description = "Git repository ArgoCD syncs from (same as the kind environment)"
  type        = string
  default     = "https://github.com/Arun-Singh-Chauhan-09/springer-nature-webapp-operator.git"
}

variable "target_revision" {
  description = "Branch ArgoCD tracks"
  type        = string
  default     = "main"
}
