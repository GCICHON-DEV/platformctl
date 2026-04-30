package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"platformctl/internal/templateengine"
)

func printExecutionPlan(w io.Writer, plan *templateengine.ExecutionPlan) {
	if options.JSON {
		writeJSON(w, map[string]interface{}{"ok": true, "plan": plan})
		return
	}
	if options.Quiet {
		return
	}
	fmt.Fprintf(w, "Template: %s\n", plan.TemplateName)
	fmt.Fprintf(w, "Source:   %s\n", plan.TemplateSource.Resolved)
	if plan.TemplateSource.Checksum != "" {
		fmt.Fprintf(w, "Checksum: %s\n", plan.TemplateSource.Checksum)
	}
	fmt.Fprintln(w)

	if len(plan.GeneratedFiles) > 0 {
		fmt.Fprintln(w, "Generated files:")
		for _, file := range plan.GeneratedFiles {
			fmt.Fprintf(w, "  - %s\n", file)
		}
		fmt.Fprintln(w)
	}

	if len(plan.RequiredTools) > 0 {
		fmt.Fprintln(w, "Required tools:")
		for _, tool := range plan.RequiredTools {
			if tool.Version != "" {
				fmt.Fprintf(w, "  - %s %s\n", tool.Name, tool.Version)
				continue
			}
			if tool.MinVersion != "" {
				fmt.Fprintf(w, "  - %s >= %s\n", tool.Name, tool.MinVersion)
				continue
			}
			fmt.Fprintf(w, "  - %s\n", tool.Name)
		}
		fmt.Fprintln(w)
	}

	if len(plan.Credentials) > 0 {
		fmt.Fprintln(w, "Credential checks:")
		for _, credential := range plan.Credentials {
			fmt.Fprintf(w, "  - %s: %s\n", credential.Name, credential.Description)
		}
		fmt.Fprintln(w)
	}

	if len(plan.Warnings) > 0 {
		fmt.Fprintln(w, "Warnings:")
		for _, warning := range plan.Warnings {
			fmt.Fprintf(w, "  - %s\n", warning)
		}
		fmt.Fprintln(w)
	}

	printSteps(w, "Plan steps", plan.PlanSteps)
	printSteps(w, "Apply steps", plan.ApplySteps)
	printSteps(w, "Destroy steps", plan.DestroySteps)
}

func printSteps(w io.Writer, title string, steps []templateengine.PlannedStep) {
	if len(steps) == 0 || options.JSON || options.Quiet {
		return
	}
	fmt.Fprintln(w, title+":")
	for _, step := range steps {
		dir := step.Dir
		if dir == "" || dir == "." {
			dir = "."
		}
		fmt.Fprintf(w, "  %d. %s\n", step.Index+1, step.Name)
		fmt.Fprintf(w, "     dir: %s\n", dir)
		fmt.Fprintf(w, "     id:  %s\n", step.ID)
		fmt.Fprintf(w, "     run: %s %s\n", step.Command, strings.Join(step.Args, " "))
		if step.TimeoutSeconds > 0 {
			fmt.Fprintf(w, "     timeout: %ds\n", step.TimeoutSeconds)
		}
	}
	fmt.Fprintln(w)
}

func writeJSON(w io.Writer, payload interface{}) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(payload)
}

func printf(cmd interface{ OutOrStdout() io.Writer }, format string, args ...interface{}) {
	if options.Quiet || options.JSON {
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), format, args...)
}

func println(cmd interface{ OutOrStdout() io.Writer }, args ...interface{}) {
	if options.Quiet || options.JSON {
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), args...)
}
