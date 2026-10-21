/** @vitest-environment jsdom */
import {afterEach,expect,it,vi} from "vitest";
import {act,cleanup,renderHook} from "@testing-library/react";
import {QueryClient,QueryClientProvider} from "@tanstack/react-query";
import type {ReactNode} from "react";
import {api,type SkillImprovementProposal} from "../api";
import {skillProposalKeys,skillProposalListOptions,useCreateSkillImprovementProposal} from "./proposals";
vi.mock("../api",()=>({api:{createSkillImprovementProposal:vi.fn(),listSkillImprovementProposals:vi.fn(),getSkillImprovementProposal:vi.fn()}}));
afterEach(()=>{cleanup();vi.resetAllMocks();});
const input={request_id:"request",baseline_revision_id:"baseline",candidate_revision_id:"candidate",category:"human_correction" as const,reason:"Correction",hypothesis:"Improve reports",applicable_scope:"Reports"};
const revision:SkillImprovementProposal={...input,id:"revision",workspace_id:"original",skill_id:"skill",evidence:{},created_by:"member",created_at:"2026-09-09"};
it.each(["success","uncertain"])("refreshes only the originating revision list after %s",async outcome=>{
 const client=new QueryClient({defaultOptions:{mutations:{retry:3,retryDelay:0}}});
 const origin=skillProposalKeys.list("original","skill",25),other=skillProposalKeys.list("other","skill",25);client.setQueryData(origin,{pages:[],pageParams:[]});client.setQueryData(other,{pages:[],pageParams:[]});
 if(outcome==="success")vi.mocked(api.createSkillImprovementProposal).mockResolvedValue(revision);else vi.mocked(api.createSkillImprovementProposal).mockRejectedValue(new Error("unreadable successful write"));
 const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>;
 const {result}=renderHook(()=>useCreateSkillImprovementProposal(),{wrapper});
 await act(async()=>{const promise=result.current.mutateAsync({workspaceId:"original",skillId:"skill",data:input});if(outcome==="success")await expect(promise).resolves.toEqual(revision);else await expect(promise).rejects.toThrow();});
 expect(api.createSkillImprovementProposal).toHaveBeenCalledExactlyOnceWith("skill",input,"original");expect(client.getQueryState(origin)?.isInvalidated).toBe(true);expect(client.getQueryState(other)?.isInvalidated).toBe(false);
 expect(client.getQueryData(skillProposalKeys.detail("original","skill","revision"))).toEqual(outcome==="success"?revision:undefined);client.clear();
});
it("passes cursor and workspace through the query options",async()=>{
 const client=new QueryClient();vi.mocked(api.listSkillImprovementProposals).mockResolvedValue({proposals:[],next_cursor:null});
 await client.fetchInfiniteQuery(skillProposalListOptions("original","skill",10));
 expect(api.listSkillImprovementProposals).toHaveBeenCalledExactlyOnceWith("skill","original",undefined,10);expect(skillProposalListOptions("original","skill").queryKey).not.toEqual(skillProposalListOptions("other","skill").queryKey);client.clear();
});
