import {afterEach,expect,it,vi} from "vitest";
import {ApiClient} from "./client";
const id="11111111-1111-4111-8111-111111111111";const hash=`sha256:${"a".repeat(64)}`;
const revision={id,workspace_id:id,source_kind:"workspace",source_id:id,revision_hash:hash,bundle:{id,name:"builder",content:"Candidate",hash},manifest:{schema_version:"silieco.skill-revision/v1",hash,bundle_hash:hash,size_bytes:9,compatible_contracts:[],tool_dependencies:[]},created_by:id,created_at:"2026-09-09"};
afterEach(()=>vi.unstubAllGlobals());
function response(value:unknown) {const fetch=vi.fn().mockResolvedValue(new Response(JSON.stringify(value),{status:200,headers:{"Content-Type":"application/json"}}));vi.stubGlobal("fetch",fetch);return fetch;}
it("creates a candidate without supplying author or digest",async()=>{
 const fetch=response(revision);const result=await new ApiClient("https://api.example.test").createSkillRevision(id,{content:"Candidate"});expect(result.id).toBe(id);
 expect(fetch.mock.calls[0]?.[0]).toBe(`https://api.example.test/api/skills/${id}/revisions`);
 expect(fetch.mock.calls[0]?.[1]).toMatchObject({method:"POST",body:JSON.stringify({content:"Candidate"})});
});
it("rejects a revision returned for another source",async()=>{response(revision);await expect(new ApiClient("https://api.example.test").getSkillRevision("22222222-2222-4222-8222-222222222222",id)).rejects.toThrow("Invalid skill revision response");});
it("rejects an unreadable successful write without retrying it",async()=>{const fetch=response({});await expect(new ApiClient("https://api.example.test").createSkillRevision(id,{content:"Candidate"})).rejects.toThrow("Invalid skill revision response");expect(fetch).toHaveBeenCalledTimes(1);});

it("loads cursor pages with the requested source and page size",async()=>{const {bundle: _bundle,...summary}=revision;const fetch=response({revisions:[summary],next_cursor:null});const page=await new ApiClient("https://api.example.test").listSkillRevisions(id,undefined,10);expect(page.revisions[0]?.id).toBe(id);expect(fetch.mock.calls[0]?.[0]).toBe(`https://api.example.test/api/skills/${id}/revisions?page_size=10`);});
it("rejects a repeating pagination cursor",async()=>{const {bundle: _bundle,...summary}=revision;response({revisions:[summary],next_cursor:id});await expect(new ApiClient("https://api.example.test").listSkillRevisions(id,id)).rejects.toThrow("Invalid skill revision page");});

it("pins revision requests to an explicit workspace instead of ambient slug",async()=>{const fetch=response(revision);await new ApiClient("https://api.example.test").createSkillRevision(id,{content:"Candidate"},id);expect(fetch.mock.calls[0]?.[1].headers).toMatchObject({"X-Workspace-ID":id,"X-Workspace-Slug":""});});
it("rejects a response from a different workspace",async()=>{response(revision);await expect(new ApiClient("https://api.example.test").getSkillRevision(id,id,"22222222-2222-4222-8222-222222222222")).rejects.toThrow("Invalid skill revision response");});
