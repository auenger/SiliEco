package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/silieco-ai/silieco/core/internal/cli"
)

// silieco task label {list|add|remove} — manages the labels attached to a
// specific task. The label itself is managed via `silieco label ...`.

var taskLabelCmd = &cobra.Command{
	Use:   "label",
	Short: "Manage labels on a task",
}

var taskLabelListCmd = &cobra.Command{
	Use:   "list <task-id>",
	Short: "List labels on a task",
	Args:  exactArgs(1),
	RunE:  runTaskLabelList,
}

var taskLabelAddCmd = &cobra.Command{
	Use:   "add <task-id> <label-id>",
	Short: "Attach a label to a task",
	Args:  exactArgs(2),
	RunE:  runTaskLabelAdd,
}

var taskLabelRemoveCmd = &cobra.Command{
	Use:   "remove <task-id> <label-id>",
	Short: "Remove a label from a task",
	Args:  exactArgs(2),
	RunE:  runTaskLabelRemove,
}

func init() {
	taskLabelCmd.AddCommand(taskLabelListCmd)
	taskLabelCmd.AddCommand(taskLabelAddCmd)
	taskLabelCmd.AddCommand(taskLabelRemoveCmd)

	taskLabelListCmd.Flags().String("output", "table", "Output format: table or json")
	taskLabelAddCmd.Flags().String("output", "table", "Output format: table or json")
	taskLabelRemoveCmd.Flags().String("output", "table", "Output format: table or json")
	taskLabelListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	taskLabelAddCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	taskLabelRemoveCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// Register under the top-level `task` command.
	taskCmd.AddCommand(taskLabelCmd)
}

func runTaskLabelList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/labels", &result); err != nil {
		return fmt.Errorf("list task labels: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func runTaskLabelAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	body := map[string]any{"label_id": labelRef.ID}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/tasks/"+taskRef.ID+"/labels", body, &result); err != nil {
		return fmt.Errorf("attach label: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func runTaskLabelRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	taskRef, err := resolveTaskRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve task: %w", err)
	}
	labelRef, err := resolveLabelID(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/tasks/"+taskRef.ID+"/labels/"+labelRef.ID); err != nil {
		return fmt.Errorf("detach label: %w", err)
	}

	// Follow up with the current label list so the user sees the result.
	// If the refresh fails, still print a clear success message — the
	// detach itself already succeeded.
	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/labels", &result); err != nil {
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"detached": true})
		}
		fmt.Fprintln(os.Stdout, "Label detached.")
		return nil
	}
	labelsRaw, _ := result["labels"].([]any)
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printLabelTable(labelsRaw, fullID)
	return nil
}

func printLabelTable(labels []any, fullID bool) {
	headers := []string{"ID", "NAME", "COLOR"}
	rows := make([][]string, 0, len(labels))
	for _, raw := range labels {
		l, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			displayID(strVal(l, "id"), fullID),
			strVal(l, "name"),
			strVal(l, "color"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}
