package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/silieco-ai/silieco/core/internal/cli"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/silieco-ai/silieco/core/pkg/workflowcontract"
	"github.com/spf13/cobra"
)

func newWorkflowCmd() *cobra.Command {
	command := &cobra.Command{Use: "workflow", GroupID: groupCore, Short: "Work with SOP contracts and execution"}
	preflight := &cobra.Command{
		Use: "preflight", Short: "Validate a local stage result against a v1 contract (no server submission)",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			contractPath, _ := cmd.Flags().GetString("contract")
			resultPath, _ := cmd.Flags().GetString("result")
			rawContract, err := readWorkflowJSON(contractPath)
			if err != nil {
				return fmt.Errorf("read contract: %w", err)
			}
			contract, err := workflowcontract.Parse(rawContract)
			if err != nil {
				return fmt.Errorf("invalid contract: %w", err)
			}
			rawResult, err := readWorkflowJSON(resultPath)
			if err != nil {
				return fmt.Errorf("read result: %w", err)
			}
			violations, err := contract.ValidateSubmission(rawResult)
			if err != nil {
				return err
			}
			report := struct {
				Valid      bool                         `json:"valid"`
				Scope      string                       `json:"scope"`
				Violations []workflowcontract.Violation `json:"violations"`
			}{len(violations) == 0, "local_preflight", violations}
			if err := json.NewEncoder(cmd.OutOrStdout()).Encode(report); err != nil {
				return err
			}
			if !report.Valid {
				return fmt.Errorf("stage result has %d contract violation(s)", len(violations))
			}
			return nil
		},
	}
	preflight.Flags().String("contract", "", "Path to silieco.stage/v1 contract JSON")
	preflight.Flags().String("result", "", "Path to result JSON containing data and artifacts")
	_ = preflight.MarkFlagRequired("contract")
	_ = preflight.MarkFlagRequired("result")
	command.AddCommand(preflight)
	addWorkflowResultCommands(command)
	return command
}

func readWorkflowJSON(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const maxBytes = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("JSON exceeds 1 MiB limit")
	}
	return raw, nil
}

func addWorkflowResultCommands(parent *cobra.Command) {
	for _, spec := range []struct {
		name, usage, description string
		args                     int
	}{
		{"executions", "executions <run-id>", "List execution identities and frozen stage configuration", 1},
		{"correct-inputs", "correct-inputs <run-id> <execution-id>", "Correct blocked stage inputs and resume the same execution", 2},
		{"agent-review", "agent-review <run-id> <execution-id> <request-id>", "Read the exact request bound to this reviewer call", 3},
		{"agent-decision", "agent-decision <run-id> <execution-id> <request-id>", "Record agent review; acceptance waits for successful call completion", 3},
		{"review", "review <run-id> <execution-id>", "Read the current evidence for human review", 2},
		{"decision", "decision <run-id> <execution-id>", "Review exact delivery evidence; approval accepts and advances the Run", 2},
		{"readiness", "readiness <run-id> <execution-id>", "Inspect incomplete delivery requirements (not stage acceptance)", 2},
		{"submit", "submit <run-id> <execution-id>", "Submit a versioned stage result for server validation", 2},
		{"result", "result <run-id> <execution-id>", "Read one exact result revision and its artifact manifest", 2},
		{"artifact", "artifact <run-id> <execution-id> <artifact-revision-id>", "Download a frozen artifact from one exact result revision", 3},
	} {
		command := &cobra.Command{Use: spec.usage, Short: spec.description, Args: cobra.ExactArgs(spec.args), RunE: runWorkflowResultCommand}
		switch spec.name {
		case "correct-inputs":
			command.Flags().String("expected-inputs", "", "File containing the full Run input object previously read")
			command.Flags().String("inputs", "", "File containing the full corrected Run input object")
			_ = command.MarkFlagRequired("expected-inputs")
			_ = command.MarkFlagRequired("inputs")
		case "decision", "agent-decision":
			command.Flags().Int32("result-revision", 0, "Exact result revision reviewed")
			command.Flags().String("basis-hash", "", "Exact lowercase evidence hash returned by the matching review command")
			command.Flags().String("outcome", "", "Review decision: approved or rejected")
			command.Flags().String("note", "", "Review explanation")
			_ = command.MarkFlagRequired("result-revision")
			_ = command.MarkFlagRequired("basis-hash")
			_ = command.MarkFlagRequired("outcome")
		case "submit":
			command.Flags().String("result", "", "Local result JSON containing data and artifacts")
			command.Flags().Int32("expected-revision", 0, "Last observed result revision; zero for the first result")
			_ = command.MarkFlagRequired("result")
			_ = command.MarkFlagRequired("expected-revision")
		case "result", "artifact":
			command.Flags().Int32("revision", 0, "Exact result revision to read")
			_ = command.MarkFlagRequired("revision")
			if spec.name == "artifact" {
				command.Flags().String("file", "", "Destination file (must not already exist)")
				_ = command.MarkFlagRequired("file")
			}
		}
		parent.AddCommand(command)
	}
}

func runWorkflowResultCommand(cmd *cobra.Command, args []string) error {
	for _, id := range args {
		if parsed, err := uuid.Parse(id); err != nil || parsed == uuid.Nil || parsed.String() != id {
			return fmt.Errorf("expected canonical UUID, got %q", id)
		}
	}
	path := "/api/workflow-instances/" + args[0] + "/executions"
	var payload any
	switch cmd.Name() {
	case "correct-inputs":
		documents := make(map[string]json.RawMessage, 2)
		for _, flag := range []string{"expected-inputs", "inputs"} {
			file, _ := cmd.Flags().GetString(flag)
			raw, err := readWorkflowJSON(file)
			if err != nil {
				return err
			}
			if err := workflowcontract.CheckRunInputCorrection(nil, raw, raw); err != nil {
				return fmt.Errorf("%s: %w", flag, err)
			}
			documents[strings.ReplaceAll(flag, "-", "_")] = json.RawMessage(raw)
		}
		payload = documents
		path += "/" + args[1] + "/inputs"

	case "agent-review":
		path += "/" + args[1] + "/reviews/" + args[2]
	case "review", "readiness":
		path += "/" + args[1] + "/" + cmd.Name()
	case "decision", "agent-decision":
		revision, _ := cmd.Flags().GetInt32("result-revision")
		basis, _ := cmd.Flags().GetString("basis-hash")
		outcome, _ := cmd.Flags().GetString("outcome")
		note, _ := cmd.Flags().GetString("note")
		digest, err := hex.DecodeString(basis)
		if revision < 1 || err != nil || len(digest) != 32 || basis != strings.ToLower(basis) {
			return fmt.Errorf("a positive result-revision and the exact lowercase 64-digit basis-hash are required")
		}
		if outcome != "approved" && outcome != "rejected" {
			return fmt.Errorf("outcome must be approved or rejected")
		}
		payload = map[string]any{"expected_result_revision": revision, "expected_basis_hash": basis, "outcome": outcome, "note": note}
		path += "/" + args[1]
		if cmd.Name() == "agent-decision" {
			path += "/reviews/" + args[2]
		}
		path += "/decisions"
	case "submit":
		expected, _ := cmd.Flags().GetInt32("expected-revision")
		if expected < 0 {
			return fmt.Errorf("expected-revision must be nonnegative")
		}
		file, _ := cmd.Flags().GetString("result")
		raw, err := readWorkflowJSON(file)
		if err != nil {
			return err
		}
		if !json.Valid(raw) {
			return fmt.Errorf("result is not valid JSON")
		}
		payload = map[string]any{"expected_revision": expected, "submission": json.RawMessage(raw)}
		path += "/" + args[1] + "/results"
	case "result", "artifact":
		revision, _ := cmd.Flags().GetInt32("revision")
		if revision < 1 {
			return fmt.Errorf("revision must be positive")
		}
		path += "/" + args[1] + "/results/" + strconv.Itoa(int(revision))
		if cmd.Name() == "artifact" {
			path += "/artifacts/" + args[2]
		}
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if (cmd.Name() == "agent-review" || cmd.Name() == "agent-decision") && (client.AgentID == "" || client.TaskID == "") {
		return fmt.Errorf("agent review requires the bound reviewer call identity (SILIECO_AGENT_ID and SILIECO_TASK_ID)")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), cli.AtLeastAPITimeout(2*time.Minute))
	defer cancel()
	if cmd.Name() == "artifact" {
		filename, _ := cmd.Flags().GetString("file")
		file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		success := false
		defer func() {
			_ = file.Close()
			if !success {
				_ = os.Remove(filename)
			}
		}()
		content, err := client.DownloadFile(ctx, path)
		if err != nil {
			return err
		}
		if _, err := file.Write(content); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		success = true
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"path": filename, "size_bytes": len(content)})
	}
	var response json.RawMessage
	if cmd.Name() == "submit" || cmd.Name() == "decision" || cmd.Name() == "agent-decision" || cmd.Name() == "correct-inputs" {
		err = client.PostJSON(ctx, path, payload, &response)
	} else {
		err = client.GetJSON(ctx, path, &response)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(response)
}
