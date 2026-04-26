package bootstrap

func (b *Bootstrap) InstallIngress() error {
	if !b.cfg.Ingress.Enabled {
		b.printSkipped("ingress")
		return nil
	}
	return b.helm.UpgradeInstall(
		"ingress-nginx",
		"ingress-nginx/ingress-nginx",
		b.cfg.Ingress.Namespace,
		b.valuesFile("ingress-nginx-values.yaml"),
	)
}
