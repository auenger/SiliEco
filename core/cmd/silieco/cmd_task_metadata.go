package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/silieco-ai/silieco/core/internal/cli"
)

// buildMetadataFilterQueryParam converts repeated `--metadata key=value`
// flags into the canonical JSON object passed as the `metadata` query
// parameter to /api/tasks. Values are typed by the same rules as
// `metadata set --value` without --type — JSON-parsed when possible, else
// taken as a literal string. Duplicate keys are rejected; AND semantics
// require distinct keys.
func buildMetadataFilterQueryParam(pairs []string) (string, error) {
	if len(pairs) == 0 {
		return "", nil
	}
	out := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		idx := strings.IndexByte(pair, '=')
		if idx <= 0 {
			return "", fmt.Errorf("--metadata %q must be in key=value form", pair)
		}
		key := pair[:idx]
		raw := pair[idx+1:]
		if _, dup := out[key]; dup {
			return "", fmt.Errorf("--metadata key %q given more than once; combine into a single filter", key)
		}
		encoded, err := parseMetadataValue(raw, "")
		if err != nil {
			return "", fmt.Errorf("--metadata %s: %w", key, err)
		}
		var v any
		if err := json.Unmarshal(encoded, &v); err != nil {
			return "", fmt.Errorf("--metadata %s: encode value: %w", key, err)
		}
		out[key] = v
	}
	buf, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode metadata filter: %w", err)
	}
	return string(buf), nil
}

// silieco task metadata {list|get|set|delete} — KV map attached to each task
// for agent pipeline state. See core/internal/handler/task_metadata.go for
// the constraints (key regex, 50-key cap, primitive-only values, 8KB blob).

var taskMetadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Manage per-task metadata (KV)",
}

var taskMetadataListCmd = &cobra.Command{
	Use:   "list <task-id>",
	Short: "List all metadata keys on a task",
	Args:  exactArgs(1),
	RunE:  runTaskMetadataList,
}

var taskMetadataGetCmd = &cobra.Command{
	Use:   "get <task-id>",
	Short: "Get a single metadata key value",
	Args:  exactArgs(1),
	RunE:  runTaskMetadataGet,
}

var taskMetadataSetCmd = &cobra.Command{
	Use:   "set <task-id>",
	Short: "Set a single metadata key value",
	Long: `Set a single metadata key value. The value is JSON-parsed by default:
  --value true / --value false  → bool
  --value 3 / --value 3.14      → number
  --value waiting               → string
Use --type to force a specific type. Quote like '"42"' (JSON-escaped) to force
a string when the bare value would otherwise sniff as a number or bool.`,
	Args: exactArgs(1),
	RunE: runTaskMetadataSet,
}

var taskMetadataDeleteCmd = &cobra.Command{
	Use:   "delete <task-id>",
	Short: "Delete a single metadata key",
	Args:  exactArgs(1),
	RunE:  runTaskMetadataDelete,
}

func init() {
	taskMetadataCmd.AddCommand(taskMetadataListCmd)
	taskMetadataCmd.AddCommand(taskMetadataGetCmd)
	taskMetadataCmd.AddCommand(taskMetadataSetCmd)
	taskMetadataCmd.AddCommand(taskMetadataDeleteCmd)

	taskMetadataListCmd.Flags().String("output", "table", "Output format: table or json")
	taskMetadataGetCmd.Flags().String("output", "json", "Output format: table or json")
	taskMetadataGetCmd.Flags().String("key", "", "Metadata key (required)")
	taskMetadataSetCmd.Flags().String("output", "table", "Output format: table or json")
	taskMetadataSetCmd.Flags().String("key", "", "Metadata key (required)")
	taskMetadataSetCmd.Flags().String("value", "", "Metadata value (required)")
	taskMetadataSetCmd.Flags().String("type", "", "Force value type: string, number, or bool (default: auto-infer via JSON parsing)")
	taskMetadataDeleteCmd.Flags().String("output", "table", "Output format: table or json")
	taskMetadataDeleteCmd.Flags().String("key", "", "Metadata key (required)")

	taskCmd.AddCommand(taskMetadataCmd)
}

// parseMetadataValue converts a CLI --value flag (and optional --type) into
// the JSON-encoded payload sent to the server. Default sniffing: if the
// raw text parses as a JSON bool / number / string, that type is used; any
// other JSON shape (null, array, object) is rejected even under default
// because the server would reject it too.
//
// forcedType non-empty overrides sniffing: "string" wraps verbatim as a
// JSON string; "number" parses strictly as a number; "bool" requires the
// literal text "true" or "false".
func parseMetadataValue(raw, forcedType string) (json.RawMessage, error) {
	switch forcedType {
	case "string":
		buf, err := json.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("encode string value: %w", err)
		}
		return buf, nil
	case "number":
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return nil, fmt.Errorf("value %q is not a valid number", raw)
		}
		return json.RawMessage(raw), nil
	case "bool":
		if raw != "true" && raw != "false" {
			return nil, fmt.Errorf("value %q is not a valid bool (expected true or false)", raw)
		}
		return json.RawMessage(raw), nil
	case "":
		// auto-infer below
	default:
		return nil, fmt.Errorf("unknown --type %q (expected string, number, or bool)", forcedType)
	}

	// Auto-infer: try JSON parse, fall back to string.
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		switch v.(type) {
		case string, bool, float64:
			return json.RawMessage(raw), nil
		}
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("encode string value: %w", err)
	}
	return buf, nil
}

func runTaskMetadataList(cmd *cobra.Command, args []string) error {
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
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/metadata", &result); err != nil {
		// Best-effort degradation: when the server does not expose the
		// per-task metadata endpoint (self-hosted backends running an
		// older build, missing migration, or routing tasks that surface
		// as 404), an agent's bootstrap "metadata list" must not fail
		// the entire run. Emit an empty map and exit 0 so the agent
		// still gets the empty-metadata signal it would have gotten on
		// a fresh task. Other status codes (auth, server errors) keep
		// real error semantics, and metadata get/set/delete are
		// unaffected — those callers still need to know when something
		// went wrong.
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			output, _ := cmd.Flags().GetString("output")
			empty := map[string]any{}
			if output == "json" {
				return cli.PrintJSON(os.Stdout, empty)
			}
			printMetadataTable(empty)
			return nil
		}
		return fmt.Errorf("list metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func runTaskMetadataGet(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
	}

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
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/metadata", &result); err != nil {
		return fmt.Errorf("get metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)
	value, present := metadata[key]
	if !present {
		return fmt.Errorf("key %q not found on task", key)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, value)
	}
	headers := []string{"KEY", "VALUE", "TYPE"}
	rows := [][]string{{key, formatMetadataValue(value), metadataValueType(value)}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runTaskMetadataSet(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
	}
	if !cmd.Flags().Changed("value") {
		return fmt.Errorf("--value is required")
	}
	rawValue, _ := cmd.Flags().GetString("value")
	forcedType, _ := cmd.Flags().GetString("type")
	value, err := parseMetadataValue(rawValue, forcedType)
	if err != nil {
		return err
	}

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

	body := map[string]any{"value": value}
	var result map[string]any
	path := "/api/tasks/" + taskRef.ID + "/metadata/" + key
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("set metadata: %w", err)
	}
	metadata, _ := result["metadata"].(map[string]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func runTaskMetadataDelete(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString("key")
	if key == "" {
		return fmt.Errorf("--key is required")
	}

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

	path := "/api/tasks/" + taskRef.ID + "/metadata/" + key
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete metadata: %w", err)
	}

	// Refresh the metadata so the user sees the result.
	var result map[string]any
	output, _ := cmd.Flags().GetString("output")
	if err := client.GetJSON(ctx, "/api/tasks/"+taskRef.ID+"/metadata", &result); err != nil {
		if output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"deleted": true})
		}
		fmt.Fprintln(os.Stdout, "Key deleted.")
		return nil
	}
	metadata, _ := result["metadata"].(map[string]any)
	if output == "json" {
		return cli.PrintJSON(os.Stdout, metadata)
	}
	printMetadataTable(metadata)
	return nil
}

func printMetadataTable(metadata map[string]any) {
	headers := []string{"KEY", "VALUE", "TYPE"}
	keys := make([]string, 0, len(metadata))
	for k := range metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		v := metadata[k]
		rows = append(rows, []string{k, formatMetadataValue(v), metadataValueType(v)})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func formatMetadataValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// JSON numbers come back as float64; render integers without a
		// trailing ".0" for ergonomics.
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		buf, _ := json.Marshal(v)
		return string(buf)
	}
}

func metadataValueType(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case float64:
		return "number"
	default:
		return "unknown"
	}
}
