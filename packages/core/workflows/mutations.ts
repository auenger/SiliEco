import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type {
  CreateWorkflowInstanceRequest,
  RecordWorkflowHumanDecisionRequest,
  RecoverWorkflowAgentReviewRequest,
  CorrectWorkflowExecutionInputsRequest,
  ReworkWorkflowExecutionRequest,
  CreateWorkflowRequest,
  CreateWorkflowVersionRequest,
  TransitionWorkflowInstanceRequest,
  UpdateWorkflowInstancePlanRequest,
  UpdateWorkflowRequest,
} from "../types";
import { issueKeys } from "../issues/queries";
import { workflowKeys } from "./queries";

export function useCreateWorkflow() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateWorkflowRequest) => api.createWorkflow(data),
    onSettled: () =>
      queryClient.invalidateQueries({
        queryKey: workflowKeys.all(workspaceId),
      }),
  });
}

export function useUpdateWorkflow() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      workflowId,
      data,
    }: {
      workflowId: string;
      data: UpdateWorkflowRequest;
    }) => api.updateWorkflow(workflowId, data),
    onSettled: (_data, _error, variables) => {
      queryClient.invalidateQueries({
        queryKey: workflowKeys.all(workspaceId),
      });
      queryClient.invalidateQueries({
        queryKey: workflowKeys.detail(workspaceId, variables.workflowId),
      });
    },
  });
}

export function useArchiveWorkflow(unusedOnly = false) {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: (workflowId: string) => api.archiveWorkflow(workflowId, unusedOnly),
    onSettled: () =>
      queryClient.invalidateQueries({
        queryKey: workflowKeys.all(workspaceId),
      }),
  });
}

export function useCreateWorkflowVersion() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      workflowId,
      data,
    }: {
      workflowId: string;
      data: CreateWorkflowVersionRequest;
    }) => api.createWorkflowVersion(workflowId, data),
    onSettled: (_data, _error, variables) => {
      queryClient.invalidateQueries({
        queryKey: workflowKeys.versions(workspaceId, variables.workflowId),
      });
      queryClient.invalidateQueries({
        queryKey: workflowKeys.all(workspaceId),
      });
    },
  });
}

export function usePublishWorkflowVersion() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      workflowId,
      versionId,
    }: {
      workflowId: string;
      versionId: string;
    }) => api.publishWorkflowVersion(workflowId, versionId),
    onSettled: (_data, _error, variables) => {
      queryClient.invalidateQueries({
        queryKey: workflowKeys.versions(workspaceId, variables.workflowId),
      });
      queryClient.invalidateQueries({
        queryKey: workflowKeys.list(workspaceId),
      });
    },
  });
}

export function useCreateWorkflowInstance() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      workflowId,
      data,
    }: {
      workflowId: string;
      data: CreateWorkflowInstanceRequest;
    }) => api.createWorkflowInstance(workflowId, data),
    onSettled: () =>
      queryClient.invalidateQueries({
        queryKey: [...workflowKeys.all(workspaceId), "instances"],
      }),
  });
}

export function useTransitionWorkflowInstance() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    retry: false,
    mutationFn: ({
      instanceId,
      data,
    }: {
      instanceId: string;
      data: TransitionWorkflowInstanceRequest;
    }) => api.transitionWorkflowInstance(instanceId, data),
    onSettled: () => Promise.all([
      queryClient.invalidateQueries({queryKey:workflowKeys.all(workspaceId)}),
      queryClient.invalidateQueries({queryKey:issueKeys.all(workspaceId)}),
    ]),
  });
}

export function useUpdateWorkflowInstancePlan() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      instanceId,
      data,
    }: {
      instanceId: string;
      data: UpdateWorkflowInstancePlanRequest;
    }) => api.updateWorkflowInstancePlan(instanceId, data),
    onSettled: (_data, _error, variables) => {
      queryClient.invalidateQueries({
        queryKey: workflowKeys.instance(workspaceId, variables.instanceId),
      });
      queryClient.invalidateQueries({
        queryKey: workflowKeys.instances(workspaceId),
      });
    },
  });
}

export function useArchiveWorkflowInstance() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: (instanceId: string) => api.archiveWorkflowInstance(instanceId),
    onSettled: (_data, _error, instanceId) => {
      queryClient.invalidateQueries({
        queryKey: workflowKeys.instance(workspaceId, instanceId),
      });
      queryClient.invalidateQueries({
        queryKey: workflowKeys.instances(workspaceId),
      });
    },
  });
}

export function useAttachWorkflowTask() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      instanceId,
      taskId,
      stageId,
    }: {
      instanceId: string;
      taskId: string;
      stageId: string;
    }) => api.attachWorkflowTask(instanceId, taskId, stageId),
    onSettled: (_data, _error, variables) =>
      queryClient.invalidateQueries({
        queryKey: workflowKeys.instance(workspaceId, variables.instanceId),
      }),
  });
}

export function useDetachWorkflowTask() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceId();
  return useMutation({
    mutationFn: ({
      instanceId,
      taskId,
    }: {
      instanceId: string;
      taskId: string;
    }) => api.detachWorkflowTask(instanceId, taskId),
    onSettled: (_data, _error, variables) =>
      queryClient.invalidateQueries({
        queryKey: workflowKeys.instance(workspaceId, variables.instanceId),
      }),
  });
}


// A conflict requires a person to review the new evidence. Never replace the
// supplied result revision/hash or retry the decision automatically.
export function useRecordWorkflowHumanDecision(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({instanceId, executionId, data}: {
      instanceId: string;
      executionId: string;
      data: RecordWorkflowHumanDecisionRequest;
    }) => {
      const decision = await api.recordWorkflowHumanDecision(instanceId, executionId, data);
      if (!decision || decision.workflow_instance_id !== instanceId || decision.execution_id !== executionId
        || decision.result_revision !== data.expected_result_revision || decision.basis_hash !== data.expected_basis_hash
        || decision.review_step !== "human" || decision.outcome !== data.outcome) {
        throw new Error("Invalid workflow decision response");
      }
      return decision;
    },
    retry: false,
    onSettled: () => queryClient.invalidateQueries({queryKey: workflowKeys.all(workspaceId)}),
  });
}


// Recovery replaces exactly the caller-reviewed input document. Conflicts and
// ambiguous responses require another explicit review, never an automatic retry.
export function useCorrectWorkflowExecutionInputs(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({instanceId, executionId, data}: {
      instanceId: string;
      executionId: string;
      data: CorrectWorkflowExecutionInputsRequest;
    }) => {
      if (!workspaceId) throw new Error("Workspace is required for input recovery");
      const result = await api.correctWorkflowExecutionInputs(instanceId, executionId, data);
      if (!result || result.instance_id !== instanceId || result.execution_id !== executionId || result.status !== "pending") {
        throw new Error("Invalid workflow input recovery response");
      }
      return result;
    },
    retry: false,
    onSettled: () => queryClient.invalidateQueries({queryKey: workflowKeys.all(workspaceId)}),
  });
}


// Rework must use the exact source and target reviewed by the user. Even an
// uncertain response requires a new explicit action, never optimistic advance.
export function useReworkWorkflowExecution(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({instanceId, sourceExecutionId, data}: {
      instanceId: string; sourceExecutionId: string; data: ReworkWorkflowExecutionRequest;
    }) => {
      if (!workspaceId) throw new Error("Workspace is required for rework");
      const result = await api.reworkWorkflowExecution(instanceId, sourceExecutionId, data);
      if (!result || result.instance_id !== instanceId || result.source_execution_id !== sourceExecutionId || result.execution_id === sourceExecutionId || result.stage_id !== data.target_stage_id) {
        throw new Error("Invalid workflow rework response");
      }
      return result;
    },
    retry: false,
    onSettled: () => Promise.all([queryClient.invalidateQueries({queryKey: workflowKeys.all(workspaceId)}), queryClient.invalidateQueries({queryKey: issueKeys.all(workspaceId)})]),
  });
}

export function useRecoverWorkflowAgentReview(workspaceId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    retry: false,
    mutationFn: async ({instanceId, executionId, requestId, data}: {
      instanceId: string; executionId: string; requestId: string; data: RecoverWorkflowAgentReviewRequest;
    }) => {
      if (!workspaceId) throw new Error("Workspace is required for review recovery");
      const result = await api.recoverWorkflowAgentReview(instanceId, executionId, requestId, data);
      if (!result || result.instance_id !== instanceId || result.execution_id !== executionId || result.request_id !== requestId
        || result.failed_task_id !== data.failed_task_id || result.task_id === data.failed_task_id) {
        throw new Error("Invalid workflow review recovery response");
      }
      return result;
    },
    onSettled: () => queryClient.invalidateQueries({queryKey: workflowKeys.all(workspaceId)}),
  });
}
