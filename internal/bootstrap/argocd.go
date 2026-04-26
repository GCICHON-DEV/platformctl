package bootstrap

func (b *Bootstrap) InstallArgoCD() error {
	if !b.cfg.GitOps.Enabled {
		b.printSkipped("ArgoCD")
		return nil
	}
	return b.helm.UpgradeInstall(
		"argocd",
		"argo/argo-cd",
		b.cfg.GitOps.Namespace,
		b.valuesFile("argocd-values.yaml"),
	)
}
