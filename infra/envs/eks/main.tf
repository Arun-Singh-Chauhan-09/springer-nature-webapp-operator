# EKS environment for the same platform that runs on kind.
#
# NOT APPLIED: this is validated in CI (fmt, validate, tflint, checkov) but has not
# been run against a real AWS account, to avoid the running cost (see README.md here).
# Everything above the cluster is shared with kind: the same platform-bootstrap
# module installs ArgoCD, which then deploys the same gitops/apps from Git.

locals {
  tags = {
    Project     = "webapp-operator"
    Environment = "demo"
    ManagedBy   = "terraform"
  }
}

################################################################################
# Network: nodes in private subnets, load balancers in public subnets
################################################################################

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "6.7.2" # exact pins; upgrades via PR (e.g. Renovate)

  name = var.cluster_name
  cidr = var.vpc_cidr
  azs  = var.azs

  private_subnets = [for i, _ in var.azs : cidrsubnet(var.vpc_cidr, 4, i)]
  public_subnets  = [for i, _ in var.azs : cidrsubnet(var.vpc_cidr, 8, i + 48)]

  enable_nat_gateway   = true
  single_nat_gateway   = true # one NAT instead of one per AZ: cost over AZ-level redundancy, fine for a demo
  enable_dns_hostnames = true

  enable_flow_log                                 = true
  create_flow_log_cloudwatch_log_group            = true
  create_flow_log_cloudwatch_iam_role             = true
  flow_log_cloudwatch_log_group_retention_in_days = 7

  # Tags the AWS Load Balancer Controller / in-tree controller use to pick subnets.
  public_subnet_tags  = { "kubernetes.io/role/elb" = 1 }
  private_subnet_tags = { "kubernetes.io/role/internal-elb" = 1 }
}

################################################################################
# EKS cluster
################################################################################

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "21.25.0"

  name               = var.cluster_name
  kubernetes_version = var.kubernetes_version

  # Public endpoint restricted to admin CIDRs, private endpoint for the nodes.
  endpoint_public_access       = true
  endpoint_public_access_cidrs = var.admin_cidrs
  endpoint_private_access      = true

  # The identity running terraform gets cluster-admin through an EKS access entry
  # (no aws-auth ConfigMap).
  enable_cluster_creator_admin_permissions = true

  enabled_log_types = ["api", "audit", "authenticator"]

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  addons = {
    vpc-cni                = { before_compute = true }
    eks-pod-identity-agent = { before_compute = true }
    kube-proxy             = {}
    coredns                = {}
    aws-ebs-csi-driver     = {} # IAM via the Pod Identity association below
  }

  eks_managed_node_groups = {
    platform = {
      ami_type       = "AL2023_x86_64_STANDARD"
      instance_types = var.node_instance_types
      min_size       = 1
      max_size       = 3
      desired_size   = 2
    }
  }
}

################################################################################
# Storage: EBS CSI driver identity + encrypted gp3 default StorageClass
# (EKS >= 1.30 no longer marks any StorageClass as default, so Loki's and
# Prometheus' PVCs would stay Pending without this.)
################################################################################

module "ebs_csi_pod_identity" {
  source  = "terraform-aws-modules/eks-pod-identity/aws"
  version = "2.0.0"

  name                      = "${var.cluster_name}-ebs-csi"
  attach_aws_ebs_csi_policy = true
}

resource "aws_eks_pod_identity_association" "ebs_csi" {
  cluster_name    = module.eks.cluster_name
  namespace       = "kube-system"
  service_account = "ebs-csi-controller-sa"
  role_arn        = module.ebs_csi_pod_identity.iam_role_arn
}

resource "kubernetes_storage_class_v1" "gp3" {
  metadata {
    name = "gp3"
    annotations = {
      "storageclass.kubernetes.io/is-default-class" = "true"
    }
  }

  storage_provisioner    = "ebs.csi.aws.com"
  volume_binding_mode    = "WaitForFirstConsumer" # create the volume in the AZ the pod lands in
  allow_volume_expansion = true
  reclaim_policy         = "Delete"
  parameters = {
    type      = "gp3"
    encrypted = "true"
  }

  depends_on = [module.eks]
}

################################################################################
# Platform: the same bootstrap module as kind (ArgoCD + root app-of-apps)
################################################################################

module "platform" {
  source          = "../../modules/platform-bootstrap"
  repo_url        = var.repo_url
  target_revision = var.target_revision

  depends_on = [module.eks, aws_eks_pod_identity_association.ebs_csi, kubernetes_storage_class_v1.gp3]
}
