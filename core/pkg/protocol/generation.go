package protocol

// Generation identifies the Task / AgentRun contract. Earlier clients use
// task_id for executions and cannot safely exchange frames with this schema.
const Generation = "task-run-v1"
const GenerationHeader = "X-Silieco-Protocol"
