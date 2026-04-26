package bootstrap

func (b *Bootstrap) ApplyNamespaces() error {
	return b.kubectl.Apply(b.manifestsDir())
}
