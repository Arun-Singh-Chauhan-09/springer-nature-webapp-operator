output "cluster_name" {
  value = module.eks.cluster_name
}

output "region" {
  value = var.region
}

output "configure_kubectl" {
  description = "Add the cluster to your kubeconfig"
  value       = "aws eks update-kubeconfig --name ${module.eks.cluster_name} --region ${var.region}"
}

output "argocd_ui" {
  value = "kubectl -n argocd port-forward svc/argocd-server 8080:80, then http://localhost:8080 (user: admin)"
}
