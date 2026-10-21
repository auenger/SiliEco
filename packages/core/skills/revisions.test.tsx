/** @vitest-environment jsdom */
import {afterEach,expect,it,vi} from "vitest";
import {act,cleanup,renderHook} from "@testing-library/react";
import {QueryClient,QueryClientProvider} from "@tanstack/react-query";
import type {ReactNode} from "react";
import {api,type SkillRevision} from "../api";
import {skillRevisionKeys,skillRevisionListOptions,useCreateSkillRevision} from "./revisions";
vi.mock("../api",()=>({api:{createSkillRevision:vi.fn(),listSkillRevisions:vi.fn(),getSkillRevision:vi.fn()}}));
afterEach(()=>{cleanup();vi.resetAllMocks();});
const hash=`sha256:${"a".repeat(64)}`;
const revision:SkillRevision={id:"revision",workspace_id:"original",source_kind:"workspace",source_id:"skill",revision_hash:hash,bundle:{id:"skill",name:"builder",content:"candidate",hash},manifest:{schema_version:"silieco.skill-revision/v1",hash,bundle_hash:hash,size_bytes:9,compatible_contracts:[],tool_dependencies:[]},created_by:"member",created_at:"2026-09-09"};
it.each(["success","uncertain"])("refreshes only the originating revision list after %s",async outcome=>{
 const client=new QueryClient({defaultOptions:{mutations:{retry:3,retryDelay:0}}});
 const origin=skillRevisionKeys.list("original","skill",25),other=skillRevisionKeys.list("other","skill",25);client.setQueryData(origin,{pages:[],pageParams:[]});client.setQueryData(other,{pages:[],pageParams:[]});
 if(outcome==="success")vi.mocked(api.createSkillRevision).mockResolvedValue(revision);else vi.mocked(api.createSkillRevision).mockRejectedValue(new Error("unreadable successful write"));
 const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={client}>{children}</QueryClientProvider>;
 const {result}=renderHook(()=>useCreateSkillRevision(),{wrapper});
 await act(async()=>{const promise=result.current.mutateAsync({workspaceId:"original",skillId:"skill",data:{content:"candidate"}});if(outcome==="success")await expect(promise).resolves.toEqual(revision);else await expect(promise).rejects.toThrow();});
 expect(api.createSkillRevision).toHaveBeenCalledExactlyOnceWith("skill",{content:"candidate"},"original");expect(client.getQueryState(origin)?.isInvalidated).toBe(true);expect(client.getQueryState(other)?.isInvalidated).toBe(false);
 expect(client.getQueryData(skillRevisionKeys.detail("original","skill","revision"))).toEqual(outcome==="success"?revision:undefined);client.clear();
});
it("passes cursor and workspace through the query options",async()=>{
 const client=new QueryClient();vi.mocked(api.listSkillRevisions).mockResolvedValue({revisions:[],next_cursor:null});
 await client.fetchInfiniteQuery(skillRevisionListOptions("original","skill",10));
 expect(api.listSkillRevisions).toHaveBeenCalledExactlyOnceWith("skill",undefined,10,"original");expect(skillRevisionListOptions("original","skill").queryKey).not.toEqual(skillRevisionListOptions("other","skill").queryKey);client.clear();
});
