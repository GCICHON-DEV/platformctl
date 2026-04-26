package bootstrap

func (b *Bootstrap) InstallMonitoring() error {
	if !b.cfg.Monitoring.Enabled {
		b.printSkipped("monitoring")
		return nil
	}
	return b.helm.UpgradeInstall(
		"monitoring",
		"prometheus-community/kube-prometheus-stack",
		b.cfg.Monitoring.Namespace,
		b.valuesFile("kube-prometheus-stack-values.yaml"),
	)
}
