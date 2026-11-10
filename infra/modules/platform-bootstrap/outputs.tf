output "argocd_namespace" {
  value = helm_release.argocd.namespace
}

output "root_application" {
  value = "platform-root"
}
