# Versioned SOP input binding foundation

The contract package defines `silieco.input-bindings/v1` for explicit projections
into declared input fields. Stage API requests/responses now carry it in
`input_bindings`; `{}` means no configured projection. Nonempty bindings require
`input_spec.contract`. Template versions and Run plans retain the configuration,
and new executions resolve `run_input` from the locked Run's `inputs`.

`stage_result` references must name a preceding stage in the plan. The internal
acceptance service now fixes the reviewed result identity/revision, rechecks its
evidence, and resolves it into the next execution in the same transaction.
The human decision API invokes acceptance atomically with approval. Background
advancement is connected for frozen `none` gates; agent/hybrid remain unfinished.
A submitted result or a human approval record alone is not sufficient.

```json
{
  "schema_version": "silieco.input-bindings/v1",
  "data": [
    {"target": "goal", "source": {"kind": "run_input", "pointer": "/goal"}},
    {"target": "requirements", "source": {"kind": "stage_result", "stage_key": "requirements", "pointer": ""}}
  ]
}
```

Each target names one top-level property declared by the input contract. Pointer
is explicit, including the empty string for the source root. Object keys use
`~0` for `~` and `~1` for `/`; arrays require canonical nonnegative indexes.
Missing values, duplicate targets, undeclared fields and incompatible values
fail binding. No fallback to comments, free text or a mutable latest result is
performed. Artifact and project-resource sources are not supported by this
resolver yet and must not be represented as ordinary unversioned URLs.

The server-side provider must lock and authorize the Run and dependencies,
reject future or invalidated stages, and return an exact accepted execution,
result identity and positive result revision. Client-provided `accepted` flags
cannot serve as that proof. The pure resolver calls the provider once per source
stage, so multiple projections cannot mix result revisions.

The generated `silieco.input-snapshot/v1` materializes input data and records Run
identity, plan revision, target/source pointers, source execution/result identity,
result revision and source checksum. Downstream execution consumes these frozen
values instead of resolving the stage key again. Contract validation runs after
projection. This snapshot format does not itself create acceptance or advance a
Run. Run-input projection now happens inside execution creation, with the frozen
binding configuration in `config_snapshot` and the materialized versioned
snapshot in `input_snapshot`. Existing executions retain their original snapshot
on repeated creation requests, even if current Run inputs changed or disappeared.
Missing or incompatible input persists a `blocked` execution with
`failure_code=input_unavailable` and a diagnostic `failure_detail`, without a
dispatch intent. Its empty input snapshot is not an accepted input. Repeated
creation returns the same blocked attempt. Configuration and database errors
still roll back; the member input-correction API described below can resume the blocked attempt. The internal
ResumeInputBlockedExecution service re-resolves current sources using the frozen
contract, preserving attempt identity and creating one dispatch intent atomically.
The standalone resume service does not modify Run inputs.
Internal human-reviewed handoff is tested, including rollback on dispatch-intent
failure and concurrent retries. Agent/hybrid review,
explicit rework, and blocked-input recovery remain unfinished. Ordinary
issue mutation paths reject edits to succeeded stages until rework is implemented.


The internal CorrectBlockedExecutionInputs service combines a compare-and-swap
of Run inputs with same-attempt recovery. It rejects changes to values already
consumed by other executions: explicit bindings protect their projected fields,
while unbound snapshots protect the complete input object. Validation or dispatch
failure rolls back the correction. The API below invokes this service; The shared Run panel and CLI provide input correction. Review evidence uses frozen execution input, not unrelated current
Run fields; consumed-input changes require explicit rework, which is unfinished.


Input correction API (Run initiator member identity, no agent/task headers):

`POST /api/workflow-instances/{run}/executions/{execution}/inputs`

```json
{"expected_inputs": {}, "inputs": {"goal": "Complete the delivery"}}
```

Use the Run's current full inputs as `expected_inputs` and send the complete
corrected object as `inputs`. This is replacement with an old-value check, not a
merge patch. The target must be the current input-blocked attempt. Successful
response contains `execution_id`, `instance_id`, `attempt` and `status=pending`.
A 409 requires reloading and reviewing the current state; never automatically
refresh old inputs and resubmit. A repeated correction after success also returns
409, without a second dispatch. The shared client rejects malformed or mismatched
response identities. No skill or agent may strip identity headers to impersonate
the Run initiator.


In the shared Web/desktop Run panel, an input-blocked execution displays its
failure detail. The Run initiator can choose **Correct inputs**, edit the full
JSON input object, then **Save and resume**. Invalid JSON cannot submit. The form
retains the originally displayed input as the expected value; failure disables
resubmission until reopening/reloading. A changed Run input remounts the form.
The shared mutation never retries, never writes optimistic input values, and
invalidates only workflow queries for its supplied workspace.


CLI input correction:

```bash
silieco workflow correct-inputs <run-id> <execution-id> \
  --expected-inputs previous-inputs.json --inputs corrected-inputs.json
```

Both files must contain complete JSON objects, each at most 1 MiB. Unknown old
inputs must not be replaced with an invented empty object. Read the current Run
and preserve its inputs before editing. The command rejects malformed JSON,
duplicate keys and non-object values before sending. It uses the configured
identity, sends one correction request, and returns 409 without refreshing or
retrying. Use the execution list to inspect the resulting status and diagnostics.
