package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"platformctl/internal/apperror"
	"platformctl/internal/state"
	"platformctl/internal/templateengine"
)

const bootstrapReceiptKey = "preflight.bootstrap.receipt.v1"
const bootstrapPendingKey = "preflight.bootstrap.pending.v1"

type bootstrapReceipt struct {
	Format             int    `json:"format"`
	OriginApprovedHash string `json:"origin_approved_hash"`
	PolicyAcceptedHash string `json:"policy_accepted_hash"`
	ObservedHash       string `json:"observed_hash"`
	Path               string `json:"path"`
	Marker             string `json:"marker"`
}

type bootstrapPolicy struct {
	path, marker, directory string
	resumed                 bool
}

func bootstrapError(message string) error {
	return apperror.WithRemediation(apperror.New(apperror.CategoryState, "PLATFORMCTL_BOOTSTRAP_INVALID", message), "Inspect the private bootstrap file and original approval. Do not adopt an existing file automatically; use an existing-target policy and review a new plan, or a fresh workspace.")
}

func bootstrapDefinition(resolved *templateengine.Resolved, plan *templateengine.ExecutionPlan) (*bootstrapPolicy, error) {
	target := resolved.Platform.Target
	if target == nil || !target.Bootstrap {
		return nil, nil
	}
	if target.Kubernetes == nil || target.ClusterName == "" || (target.Kubernetes.EndpointSource != "eks" && target.Kubernetes.EndpointSource != "kind") {
		return nil, bootstrapError("bootstrap requires cluster_name and an explicit EKS or fixed-loopback Kind endpoint policy")
	}
	path, err := privateBootstrapPath(target.Kubernetes.Kubeconfig)
	if err != nil {
		return nil, err
	}
	marker, directory, index, count := "", "", -1, 0
	for i, step := range plan.ApplySteps {
		if step.EstablishesClusterTarget {
			count++
			index = i
			marker = templateengine.StepID("apply", step.Index, step.ID)
			directory = step.Dir
			if clusterStep(step) || (step.Retry != nil && step.Retry.Attempts > 1) {
				return nil, bootstrapError("bootstrap marker must run once before cluster-targeting operations")
			}
		}
	}
	if count != 1 {
		return nil, bootstrapError("bootstrap requires exactly one establishes_cluster_target apply step")
	}
	for i, step := range plan.ApplySteps {
		if i < index && clusterStep(step) {
			return nil, bootstrapError("cluster-targeting step precedes bootstrap marker")
		}
	}
	for _, steps := range [][]templateengine.PlannedStep{plan.PlanSteps, plan.DestroySteps} {
		for _, step := range steps {
			if step.EstablishesClusterTarget {
				return nil, bootstrapError("bootstrap marker is supported only in apply")
			}
		}
	}
	return &bootstrapPolicy{path: path, marker: marker, directory: directory}, nil
}

func prepareBootstrap(resolved *templateengine.Resolved, plan *templateengine.ExecutionPlan, st *state.State, resume bool) (*bootstrapPolicy, error) {
	policy, err := bootstrapDefinition(resolved, plan)
	if err != nil || policy == nil {
		return policy, err
	}
	if st.Metadata[bootstrapPendingKey] != "" {
		return nil, bootstrapError("bootstrap marker was started without an atomic receipt; inspect and replan")
	}
	_, statErr := os.Lstat(policy.path)
	if resume && st.Metadata[bootstrapReceiptKey] != "" {
		var receipt bootstrapReceipt
		if json.Unmarshal([]byte(st.Metadata[bootstrapReceiptKey]), &receipt) != nil || receipt.Format != 1 || len(receipt.OriginApprovedHash) != 64 || receipt.Path != policy.path || receipt.Marker != policy.marker || receipt.PolicyAcceptedHash != st.ApprovedPlanHash || receipt.PolicyAcceptedHash != plan.Digest || !st.CompletedSteps[policy.marker] {
			return nil, bootstrapError("bootstrap receipt does not match the saved policy approval")
		}
		origin, originErr := plan.BootstrapOriginDigest(policy.path)
		if originErr != nil || origin != receipt.OriginApprovedHash {
			return nil, bootstrapError("bootstrap receipt has an unrelated original approval")
		}
		observed, err := privateBootstrapHash(policy.path)
		captured, captureErr := plan.TargetFileHash(policy.path)
		if err != nil || captureErr != nil || observed != receipt.ObservedHash || captured != observed {
			return nil, bootstrapError("observed bootstrap configuration changed after its receipt")
		}
		policy.resumed = true
		return policy, nil
	}
	if st.Metadata[bootstrapReceiptKey] != "" || st.CompletedSteps[policy.marker] || !os.IsNotExist(statErr) {
		return nil, bootstrapError("new bootstrap refuses an existing file or prior receipt")
	}
	return policy, nil
}

func validateBootstrapDestroy(resolved *templateengine.Resolved, plan *templateengine.ExecutionPlan, st *state.State) error {
	policy, err := bootstrapDefinition(resolved, plan)
	if err != nil || policy == nil {
		return err
	}
	var receipt bootstrapReceipt
	if st.Metadata[bootstrapPendingKey] != "" || json.Unmarshal([]byte(st.Metadata[bootstrapReceiptKey]), &receipt) != nil || receipt.Format != 1 || len(receipt.OriginApprovedHash) != 64 || receipt.Marker != policy.marker || receipt.Path != policy.path || receipt.PolicyAcceptedHash != st.ApprovedPlanHash || receipt.PolicyAcceptedHash != plan.Digest {
		return bootstrapError("destroy requires the original successful bootstrap receipt or an explicitly replanned existing-target policy")
	}
	origin, originErr := plan.BootstrapOriginDigest(policy.path)
	if originErr != nil || origin != receipt.OriginApprovedHash {
		return bootstrapError("bootstrap receipt has an unrelated original approval")
	}
	observed, err := privateBootstrapHash(policy.path)
	if err != nil || observed != receipt.ObservedHash {
		return bootstrapError("bootstrap configuration differs from its receipt")
	}
	return nil
}

// Only this private namespace is eligible for the one policy transition. All
// other files retain ordinary snapshot protection and are never adopted.
func privateBootstrapPath(path string) (string, error) {
	if path == "" {
		return "", bootstrapError("bootstrap requires an explicit private kubeconfig path")
	}
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, absolute)
	if err != nil || !strings.HasPrefix(rel, filepath.Join(state.DirName, "targets")+string(os.PathSeparator)) || filepath.Ext(rel) != ".kubeconfig" {
		return "", bootstrapError("bootstrap kubeconfig must be a .platformctl/targets/*.kubeconfig workspace file")
	}
	current := root
	parts := strings.Split(rel, string(os.PathSeparator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", bootstrapError("bootstrap parent must be an owned real directory")
		}
		if strings.Contains(current, filepath.Join(state.DirName, "targets")) && !privateOwned(info, true) {
			return "", bootstrapError("bootstrap target directory must be private and owned")
		}
	}
	return absolute, nil
}

func (policy *bootstrapPolicy) before(ctx context.Context, pathEnv string, resolved *templateengine.Resolved, st *state.State, step templateengine.PlannedStep) error {
	if err := requireTargetAt(ctx, pathEnv, resolved, clusterStep(step), step.Dir); err != nil {
		return err
	}
	if !step.EstablishesClusterTarget {
		return nil
	}
	if policy.resumed || st.Metadata[bootstrapPendingKey] != "" {
		return bootstrapError("bootstrap marker cannot be repeated")
	}
	if _, err := os.Lstat(policy.path); !os.IsNotExist(err) {
		return bootstrapError("bootstrap target appeared before its approved marker")
	}
	if _, err := privateBootstrapPath(policy.path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(policy.path), 0700); err != nil {
		return err
	}
	if _, err := privateBootstrapPath(policy.path); err != nil {
		return err
	}
	st.Metadata[bootstrapPendingKey] = st.ApprovedPlanHash
	return state.Save(st)
}

func (policy *bootstrapPolicy) after(ctx context.Context, pathEnv string, resolved *templateengine.Resolved, plan *templateengine.ExecutionPlan, st *state.State, stepID string) error {
	if stepID != policy.marker {
		return nil
	}
	observed, err := privateBootstrapHash(policy.path)
	if err != nil {
		return bootstrapError("bootstrap marker did not produce one owned private regular kubeconfig")
	}
	if err := requireTargetAt(ctx, pathEnv, resolved, true, policy.directory); err != nil {
		return err
	}
	if _, _, err := scopedClusterCA(resolved.Platform.Target); err != nil {
		return bootstrapError("bootstrap kubeconfig requires a valid inline CA without external references")
	}
	current, err := templateengine.LoadWithMode(defaultConfigFile, options.Offline)
	if err != nil {
		return err
	}
	if err = current.Validate(); err != nil {
		return err
	}
	generated, hashErr := current.GeneratedHash()
	if hashErr != nil || generated != st.GeneratedHash || generated != plan.GeneratedDigest {
		return bootstrapError("bootstrap changed approved generated bytes")
	}
	next, err := current.BuildPlan()
	if err != nil {
		return err
	}
	origin := plan.Digest
	accepted, err := plan.AcceptBootstrapSnapshot(next, policy.path)
	if err != nil || accepted != observed {
		return bootstrapError("bootstrap changed another approved input or raced with identity verification")
	}
	receipt := bootstrapReceipt{Format: 1, OriginApprovedHash: origin, PolicyAcceptedHash: plan.Digest, ObservedHash: observed, Path: policy.path, Marker: policy.marker}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	st.Metadata[bootstrapReceiptKey] = string(encoded)
	delete(st.Metadata, bootstrapPendingKey)
	st.LastPlanHash, st.ApprovedPlanHash = plan.Digest, plan.Digest
	// The caller records the marker's completed checkpoint and state.Save in
	// the same atomic replacement. This is one original policy approval,
	// never a claim of a new human approval of the observed bytes.
	return nil
}

func privateBootstrapHash(path string) (string, error) {
	if _, err := privateBootstrapPath(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !privateOwned(info, false) {
		return "", fmt.Errorf("bootstrap file is not private/owned/regular")
	}
	return privateFileHash(path)
}
