import {infiniteQueryOptions,queryOptions,useMutation,useQueryClient} from "@tanstack/react-query";
import {api,type CreateSkillRevisionInput} from "../api";
import {workspaceKeys} from "../workspace/queries";

export const skillRevisionKeys={
 all:(workspaceId:string,skillId:string)=>[...workspaceKeys.skills(workspaceId),skillId,"revisions"] as const,
 lists:(workspaceId:string,skillId:string)=>[...skillRevisionKeys.all(workspaceId,skillId),"list"] as const,
 list:(workspaceId:string,skillId:string,pageSize:number)=>[...skillRevisionKeys.lists(workspaceId,skillId),pageSize] as const,
 detail:(workspaceId:string,skillId:string,revisionId:string)=>[...skillRevisionKeys.all(workspaceId,skillId),"detail",revisionId] as const,
};

export function skillRevisionListOptions(workspaceId:string,skillId:string,pageSize=25) {
 return infiniteQueryOptions({
  queryKey:skillRevisionKeys.list(workspaceId,skillId,pageSize),
  initialPageParam:undefined as string|undefined,
  queryFn:({pageParam})=>api.listSkillRevisions(skillId,pageParam,pageSize,workspaceId),
  getNextPageParam:page=>page.next_cursor ?? undefined,
  enabled:!!workspaceId && !!skillId,
 });
}

export function skillRevisionDetailOptions(workspaceId:string,skillId:string,revisionId:string) {
 return queryOptions({queryKey:skillRevisionKeys.detail(workspaceId,skillId,revisionId),queryFn:()=>api.getSkillRevision(skillId,revisionId,workspaceId),enabled:!!workspaceId && !!skillId && !!revisionId});
}

// Workspace/source coordinates travel with each mutation, so navigation during
// a request cannot redirect its write or refresh another workspace's cache.
export function useCreateSkillRevision() {
 const client=useQueryClient();
 return useMutation({
  mutationFn:({workspaceId,skillId,data}:{workspaceId:string;skillId:string;data:CreateSkillRevisionInput})=>{
   if(!workspaceId || !skillId) throw new Error("A workspace and skill are required");
   return api.createSkillRevision(skillId,data,workspaceId);
  },
  retry:false,
  onSuccess:(revision,variables)=>{client.setQueryData(skillRevisionKeys.detail(variables.workspaceId,variables.skillId,revision.id),revision);},
  onSettled:(_data,_error,variables)=>client.invalidateQueries({queryKey:skillRevisionKeys.lists(variables.workspaceId,variables.skillId)}),
 });
}
