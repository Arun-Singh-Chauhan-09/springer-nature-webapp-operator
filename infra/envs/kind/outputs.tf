output "kube_context" {
  value = "kind-${kind_cluster.this.name}"
}

output "argocd_ui" {
  value = "make argocd-ui, then open http://localhost:8080 (user: admin)"
}
