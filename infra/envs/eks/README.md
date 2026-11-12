# EKS environment (code only, not applied)

The same platform as `infra/envs/kind`, on Amazon EKS. Only the layer **below**
Kubernetes changes; ArgoCD, the operator, monitoring and the apps come from the same
`platform-bootstrap` module and the same `gitops/apps` in Git.

> **Not applied, on purpose.** This environment is validated on every push
> (`terraform fmt`, `validate`, `tflint` with the AWS ruleset, `checkov`), but I have not
> run it against an AWS account to avoid paying for an idle cluster. Everything it deploys
> on top of the cluster is exercised end to end on kind, with drills D1–D6.

## What it creates

| Layer | Resources |
|---|---|
| Network | VPC across 3 AZs: private subnets for nodes, public subnets for load balancers, one NAT gateway, VPC flow logs (7-day retention) |
| Cluster | EKS (`terraform-aws-modules/eks` v21), public API endpoint limited to `admin_cidrs`, private endpoint for nodes, control-plane logs (api, audit, authenticator), access entries instead of `aws-auth` |
| Nodes | Managed node group, AL2023, 2 × t3.large (min 1, max 3) |
| Add-ons | vpc-cni, kube-proxy, CoreDNS, EKS Pod Identity Agent, EBS CSI driver (IAM via **Pod Identity**, not static keys or IRSA) |
| Storage | Encrypted gp3 **default** StorageClass (EKS ≥ 1.30 has no default, so Loki/Prometheus PVCs would stay Pending) |
| Platform | `modules/platform-bootstrap`: ArgoCD + root app-of-apps, identical to kind |

## Cost (approximate, eu-central-1, on-demand)

| Item | Approx. per month |
|---|---|
| EKS control plane ($0.10/h) | ~$73 |
| 2 × t3.large nodes | ~$140 |
| NAT gateway (hourly, before data) | ~$38 |
| EBS volumes, CloudWatch logs | ~$10 |
| **Total** | **~$260/month (~$8.50/day, ~$0.35/hour)** |

Prices change; check the AWS Pricing Calculator before applying. The single NAT gateway
and 2 small nodes are deliberate cost choices for a demo, not production settings.

## Apply (if you have an AWS account)

Prerequisites: AWS CLI v2 with credentials, Terraform >= 1.10, an S3 bucket for state.

```bash
cd infra/envs/eks
cp backend.hcl.example backend.hcl                         # set your bucket
echo 'admin_cidrs = ["<your-ip>/32"]' > terraform.tfvars  # curl -s https://checkip.amazonaws.com
terraform init -backend-config=backend.hcl
terraform plan -out tfplan                                # or: make eks-plan from the repo root
terraform apply tfplan                                    # ~15-20 min
aws eks update-kubeconfig --name webapp-platform --region eu-central-1
kubectl -n argocd get applications                        # same 7 apps as on kind
KCTX=$(kubectl config current-context) make gitops-drills
```

## Teardown (order matters)

Kubernetes creates AWS resources that Terraform does not know about (EBS volumes for
PVCs, load balancers for `type: LoadBalancer` Services). Destroying the cluster first
would orphan them and can block the VPC deletion. So:

```bash
kubectl -n argocd delete application platform-root   # cascades: ArgoCD prunes all apps, PVCs, EBS volumes
kubectl get pvc -A                                   # wait until empty
terraform destroy
```

## Production gaps (deliberate for a demo)

- One NAT gateway (one per AZ in production), no Karpenter or cluster autoscaler
- ArgoCD UI via port-forward, no ingress, TLS or SSO
- Grafana admin password in values; production would use an External Secrets source
- No separate AWS accounts per environment
