import { infiniteQueryOptions, queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, type CreateSkillImprovementProposalInput } from "../api";
import { workspaceKeys } from "../workspace/queries";

export const skillProposalKeys = {
  all: (workspaceId: string, skillId: string) => [...workspaceKeys.skills(workspaceId), skillId, "proposals"] as const,
  lists: (workspaceId: string, skillId: string) => [...skillProposalKeys.all(workspaceId, skillId), "list"] as const,
  list: (workspaceId: string, skillId: string, pageSize: number) => [...skillProposalKeys.lists(workspaceId, skillId), pageSize] as const,
  detail: (workspaceId: string, skillId: string, proposalId: string) => [...skillProposalKeys.all(workspaceId, skillId), "detail", proposalId] as const,
};

export function skillProposalListOptions(workspaceId: string, skillId: string, pageSize = 25) {
  return infiniteQueryOptions({
    queryKey: skillProposalKeys.list(workspaceId, skillId, pageSize),
    initialPageParam: undefined as string | undefined,
    queryFn: ({pageParam}) => api.listSkillImprovementProposals(skillId, workspaceId, pageParam, pageSize),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: !!workspaceId && !!skillId,
  });
}

export function skillProposalDetailOptions(workspaceId: string, skillId: string, proposalId: string) {
  return queryOptions({
    queryKey: skillProposalKeys.detail(workspaceId, skillId, proposalId),
    queryFn: () => api.getSkillImprovementProposal(skillId, proposalId, workspaceId),
    enabled: !!workspaceId && !!skillId && !!proposalId,
  });
}

export function useCreateSkillImprovementProposal() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({workspaceId, skillId, data}: {workspaceId: string; skillId: string; data: CreateSkillImprovementProposalInput}) => {
      if (!workspaceId || !skillId) throw new Error("A workspace and skill are required");
      return api.createSkillImprovementProposal(skillId, data, workspaceId);
    },
    retry: false,
    onSuccess: (proposal, variables) => {
      client.setQueryData(skillProposalKeys.detail(variables.workspaceId, variables.skillId, proposal.id), proposal);
    },
    onSettled: (_data, _error, variables) => client.invalidateQueries({queryKey: skillProposalKeys.lists(variables.workspaceId, variables.skillId)}),
  });
}
