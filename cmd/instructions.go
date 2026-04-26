package cmd

import (
	"fmt"
	"io"

	"platformctl/internal/config"
)

func printAccessInstructions(w io.Writer, cfg *config.Config) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Platform environment is ready.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Check the cluster:")
	fmt.Fprintln(w, "  kubectl get nodes")
	fmt.Fprintln(w, "  kubectl get ns")
	fmt.Fprintln(w, "  kubectl get pods -A")
	fmt.Fprintln(w)
	if cfg.GitOps.Enabled {
		fmt.Fprintln(w, "Access ArgoCD:")
		fmt.Fprintf(w, "  kubectl -n %s port-forward svc/argocd-server 8080:443\n", cfg.GitOps.Namespace)
		fmt.Fprintf(w, "  kubectl -n %s get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d; echo\n", cfg.GitOps.Namespace)
		fmt.Fprintln(w, "  open https://localhost:8080")
		fmt.Fprintln(w)
	}
	if cfg.Monitoring.Enabled {
		fmt.Fprintln(w, "Access Grafana:")
		fmt.Fprintf(w, "  kubectl -n %s port-forward svc/monitoring-grafana 3000:80\n", cfg.Monitoring.Namespace)
		fmt.Fprintf(w, "  kubectl -n %s get secret monitoring-grafana -o jsonpath='{.data.admin-password}' | base64 -d; echo\n", cfg.Monitoring.Namespace)
		fmt.Fprintln(w, "  open http://localhost:3000")
		fmt.Fprintln(w)
	}
}
