"use client";

import { workflowOperationDialogClassName } from "./workflow-dialog";
import { StageExecutionFields, validReworkPolicyDraft, validRetryPolicyDraft } from "./stage-execution-fields";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowLeft,
  ArrowUp,
  Archive,
  Bot,
  CalendarClock,
  Check,
  ChevronDown,
  ChevronRight,
  CircleDot,
  Clock3,
  Copy,
  FileCheck2,
  FileInput,
  FileOutput,
  GitBranch,
  History,
  LayoutTemplate,
  LoaderCircle,
  Play,
  Plus,
  ShieldCheck,
  SlidersHorizontal,
  Sparkles,
  Trash2,
  UserCheck,
  UserRound,
  Wrench,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@silieco/core/hooks";
import { workspaceMCPServersOptions } from "@silieco/core/mcp";
import { useWorkspacePaths } from "@silieco/core/paths";
import type {
  Agent,
  MemberWithUser,
  SkillSummary,
  Workflow,
  WorkflowGateType,
  WorkflowInstance,
} from "@silieco/core/types";
import {
  agentListOptions,
  memberListOptions,
  skillListOptions,
} from "@silieco/core/workspace/queries";
import {
  useCreateWorkflow,
  useCreateWorkflowInstance,
  useArchiveWorkflowInstance,
  workflowInstancesOptions,
  workflowListOptions,
} from "@silieco/core/workflows";
import { Badge } from "@silieco/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@silieco/ui/components/ui/alert-dialog";
import { Button } from "@silieco/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@silieco/ui/components/ui/dialog";
import { Input } from "@silieco/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@silieco/ui/components/ui/select";
import { Switch } from "@silieco/ui/components/ui/switch";
import { Textarea } from "@silieco/ui/components/ui/textarea";
import { cn } from "@silieco/ui/lib/utils";
import { SkillPickerList } from "../../agents/components/skill-picker-list";
import { ActorAvatar } from "../../common/actor-avatar";
import { ContentEditor, ReadonlyContent } from "../../editor";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import {
  PickerEmpty,
  PickerItem,
  PickerSection,
  PropertyPicker,
} from "../../issues/components/pickers/property-picker";
import { WorkflowRunPlanDialog } from "./workflow-run-plan-dialog";
import { StageMCPSelector } from "./stage-mcp-selector";

type StageStatus = string;
type CanonicalStageStatus = "backlog" | "todo" | "in_progress" | "in_review" | "done" | "blocked";
type DeciderType = "human" | "self_agent" | "agent";
type EvaluationMode = "on_task_change" | "manual" | "scheduled";
interface SopStageDraft {
  executionMode?: string;
  executorId?: string | null;
  inputSpec?: Record<string, unknown>;
  inputBindings?: Record<string, unknown>;
  stableKey?: string;
  rollbackStageKey?: string | null;
  outputSpec?: Record<string, unknown>;
  reworkPolicy?: Record<string, unknown>;
  retryPolicy?: Record<string, unknown>;
  completionRule?: Record<string, unknown>;
  id: string;
  name: string;
  description: string;
  statuses: StageStatus[];
  evaluationMode: EvaluationMode;
  schedule: string;
  input: string;
  output: string;
  skills: string[];
  allowedMcpServerIds: string[] | null;
  gateEnabled: boolean;
  deciderType: DeciderType;
  decider: string;
  requireHuman: boolean;
  humanDecider: string;
}
interface SopTemplate {
  id: string;
  name: string;
  description: string;
  source: "preset" | "blank";
  stages: Array<Omit<SopStageDraft, "id">>;
}

const DEFAULT_STATUSES: StageStatus[] = ["todo", "in_progress", "done"];
const ALL_STATUSES: CanonicalStageStatus[] = ["backlog", "todo", "in_progress", "in_review", "done", "blocked"];
function stage(
  name: string,
  input: string,
  output: string,
  skills: string,
  gate: Partial<Omit<SopStageDraft, "id" | "name" | "input" | "output" | "skills">> = {},
): Omit<SopStageDraft, "id"> {
  return {
    name,
    description: "",
    statuses: gate.statuses ?? DEFAULT_STATUSES,
    evaluationMode: "on_task_change",
    schedule: "",
    input,
    output,
    skills: skills.split(",").map((value) => value.trim()).filter(Boolean),
    allowedMcpServerIds: null,
    gateEnabled: gate.gateEnabled ?? false,
    deciderType: gate.deciderType ?? "human",
    decider: gate.decider ?? "",
    requireHuman: gate.requireHuman ?? false,
    humanDecider: "",
  };
}

export const SOP_TEMPLATES: SopTemplate[] = [
  {
    id: "thermal-simulation-poc",
    name: "散热仿真 POC（Mock）",
    description: "用内置 skill 和确定性 Mock Runner 演练需求提取、模型预检、基线失败、候选确认、复算与报告交付。",
    source: "preset",
    stages: [
      {
        ...stage(
          "散热需求提取",
          "当前 Task / 用户说明 / 已上传的规格书、表格、图片或模型文件",
          "requirements-draft.json + missing-information.md",
          "silieco-thermal-requirements",
        ),
        description: "从附件和上下文提取工况、目标温度、关键器件、功耗与约束；不确定信息保留来源和待确认项。",
      },
      {
        ...stage(
          "需求与验收确认",
          "requirements-draft.json + missing-information.md + 用户补充信息",
          "requirements-snapshot.json + acceptance-profile.json",
          "silieco-thermal-requirements-review",
          {
            statuses: ["todo", "in_review", "done", "blocked"],
            gateEnabled: true,
            deciderType: "human",
            requireHuman: true,
          },
        ),
        description: "冻结带单位和来源的需求快照；最高温度目标、工况和关键器件未确认时必须阻止后续仿真。",
      },
      {
        ...stage(
          "模型预检（Mock）",
          "已上传模型文件 + requirements-snapshot.json",
          "model-validation.json（VALID 或 INVALID）",
          "silieco-thermal-model-precheck, silieco-thermal-mock-runner",
          { statuses: ["todo", "in_progress", "in_review", "done", "blocked"] },
        ),
        description: "只验证文件可读性、扩展名、哈希和 POC 元数据；明确声明未启动 AEDT，也不把 Mock 预检当成真实模型校验。",
      },
      {
        ...stage(
          "基线仿真（Mock）",
          "requirements-snapshot.json + acceptance-profile.json + model-validation.json",
          "baseline-job.json + baseline-result.json",
          "silieco-thermal-baseline, silieco-thermal-mock-runner",
          { statuses: ["todo", "in_progress", "in_review", "done", "blocked"] },
        ),
        description: "模拟 MCP accepted/running/succeeded 状态，并确定性生成一组未达标的基线指标以演练完整 FAIL 分支。",
      },
      {
        ...stage(
          "结果判定与候选方案",
          "requirements-snapshot.json + acceptance-profile.json + baseline-result.json",
          "baseline-verdict.json + candidates.json",
          "silieco-thermal-result-review, silieco-thermal-mock-runner",
          { statuses: ["todo", "in_progress", "in_review", "done", "blocked"] },
        ),
        description: "用确定性规则生成 PASS/FAIL/INVALID/DIVERGED 判定；Mock 基线 FAIL 时给出受限候选方案，不由 skill 自行实施。",
      },
      {
        ...stage(
          "候选方案确认",
          "baseline-verdict.json + candidates.json",
          "approved-candidate.json 或 rejection.json",
          "silieco-thermal-candidate-review",
          {
            statuses: ["todo", "in_review", "done", "blocked"],
            gateEnabled: true,
            deciderType: "human",
            requireHuman: true,
          },
        ),
        description: "工程师选择一个候选方案或全部驳回；没有结构化批准记录时禁止进入候选仿真。",
      },
      {
        ...stage(
          "候选仿真（Mock）",
          "candidates.json + approved-candidate.json + baseline-result.json + requirements-snapshot.json + model-validation.json",
          "candidate-job.json + candidate-result.json + candidate-verdict.json",
          "silieco-thermal-candidate-simulation, silieco-thermal-mock-runner",
          { statuses: ["todo", "in_progress", "in_review", "done", "blocked"] },
        ),
        description: "仅对已批准候选方案执行 Mock 复算，保留基线对比、热判定和回退建议。",
      },
      {
        ...stage(
          "报告交付",
          "全部需求快照、模型预检、基线与候选结果、判定和批准记录",
          "thermal-poc-report.md + artifact-index.json",
          "silieco-thermal-report, silieco-thermal-mock-runner",
          {
            statuses: ["todo", "in_review", "done", "blocked"],
            gateEnabled: true,
            deciderType: "human",
            requireHuman: true,
          },
        ),
        description: "生成清楚区分执行成功与热设计判定的 POC 报告，并经人工 Gate 完成交付。",
      },
    ],
  },
  {
    id: "feature-development",
    name: "Feature 研发工作流",
    description: "面向第一版交付的轻量 Feature 流程：用 Task 和 Markdown 产物串联需求、拆分、开发、验证与归档。",
    source: "preset",
    stages: [
      {
        ...stage(
          "需求输入与分析",
          "当前 Task / 原始需求 / 用户反馈 / 业务目标",
          "feature-brief.md 或 Task 内的等价需求说明",
          "silieco-new-feature",
        ),
        description: "把当前大 Task 收敛为目标、范围、约束、验收条件和待确认问题。",
      },
      {
        ...stage(
          "PRD 产出",
          "已确认的 feature-brief.md / Task 需求说明",
          "PRD.md（含验收条件与候选开发切片）",
          "silieco-enrich-feature",
        ),
        description: "输出足够支撑第一版开发的轻量 PRD，不强制额外生成 task.md 和 checklist.md。",
      },
      {
        ...stage(
          "PRD 校验",
          "PRD.md / 项目与仓库上下文",
          "spec-review.md（pass 或 block）",
          "silieco-review-spec",
          {
            statuses: ["todo", "in_review", "done", "blocked"],
            gateEnabled: true,
            deciderType: "human",
            requireHuman: true,
          },
        ),
        description: "只检查会阻碍第一版实现或验收的问题，通过人工 Gate 确认范围。",
      },
      {
        ...stage(
          "原型或 MVP",
          "已通过校验的 PRD.md",
          "prototype-decision.md + 可选 Prototype / MVP",
          "silieco-prototype-feature",
          {
            statuses: ["todo", "in_progress", "in_review", "done", "blocked"],
            gateEnabled: true,
            deciderType: "human",
            requireHuman: true,
          },
        ),
        description: "先判断是否需要原型；仅在存在关键不确定性时产出最小 Demo 或技术 Spike。",
      },
      {
        ...stage(
          "Feature 拆分与开发准备",
          "当前大 Task / PRD.md / 原型结论",
          "已关联的开发子 Task / 分组顺序 / 覆盖摘要",
          "silieco-split-feature",
        ),
        description: "把大 Task 拆成父子关联的小 Task，用子任务 Stage 屏障表达并行组与先后顺序。",
      },
      {
        ...stage(
          "开发实现",
          "当前开发子 Task / PRD.md / 前置子 Task 结果",
          "代码 / 聚焦测试 / 实现交接说明",
          "silieco-run-feature",
          {
            statuses: ["todo", "in_progress", "in_review", "done", "blocked"],
          },
        ),
        description: "每个子 Task 独立执行开发前检查和实现；不在本 Stage 重复执行最终验收。",
      },
      {
        ...stage(
          "本地化测试与验收",
          "待验收实现 / PRD 验收条件 / 子 Task 结果",
          "verification-report.md 或 Task 验收记录",
          "silieco-verify-feature",
          {
            statuses: ["todo", "in_progress", "in_review", "done", "blocked"],
            gateEnabled: true,
            deciderType: "human",
            requireHuman: true,
          },
        ),
        description: "按改动范围执行本地检查并验证验收条件；失败保持 block，不进入完成阶段。",
      },
      {
        ...stage(
          "完成与归档",
          "通过验收的实现 / 子 Task 状态 / verification-report.md",
          "completion-summary.md / Release Notes / 后续项",
          "silieco-complete-feature",
          {
            statuses: ["todo", "in_review", "done", "blocked"],
            gateEnabled: true,
            deciderType: "human",
            requireHuman: true,
          },
        ),
        description: "汇总子 Task 和验证结果，准备交付记录；合并、推送与发布仍按授权执行。",
      },
    ],
  },
  {
    id: "bid",
    name: "投标模板",
    description: "从招标文件分析到审核、得分点比较的完整交付流程。",
    source: "preset",
    stages: [
      stage("招标文件分析", "招标公告.pdf", "分析报告.md", "招标文件解析, 得分点提取"),
      stage("投标方案制定", "分析报告.md", "投标方案.md", "方案撰写"),
      stage("投标标书编写", "投标方案.md", "技术标.md / 商务标.md", "标书生成"),
      stage("审核校验", "技术标.md / 商务标.md", "审核意见.md", "合规校验", {
        statuses: ["todo", "in_review", "done"],
        gateEnabled: true,
        deciderType: "human",
        decider: "@bid-lead",
        requireHuman: true,
      }),
      stage("得分点比较", "全部产出物", "得分分析.md", "得分点提取"),
    ],
  },
  {
    id: "software",
    name: "软件开发模板",
    description: "覆盖需求、架构、开发、测试和发布验收。",
    source: "preset",
    stages: [
      stage("需求设计", "产品需求", "PRD.md", "需求拆解"),
      stage("架构设计", "PRD.md", "架构文档.md / API 设计.md", "架构设计, 接口设计"),
      stage("开发实现", "架构文档.md", "代码 + 单元测试", "编码, TDD", {
        statuses: ["todo", "in_progress", "in_review", "done"],
        gateEnabled: true,
        deciderType: "self_agent",
        decider: "@self",
      }),
      stage("E2E 测试", "代码", "测试报告.md", "E2E 测试"),
      stage("发布验收", "测试报告.md", "发布说明.md", "发布", {
        gateEnabled: true,
        deciderType: "human",
        decider: "@pm",
        requireHuman: true,
      }),
    ],
  },
  {
    id: "bugfix",
    name: "Bug 修复模板",
    description: "用最短路径完成问题定位、修复实现和验证确认。",
    source: "preset",
    stages: [
      stage("问题复现与定位", "Bug 描述", "根因分析.md", "调试"),
      stage("修复实现", "根因分析.md", "修复代码 + 单元测试", "编码"),
      stage("验证确认", "修复代码", "验证报告.md", "测试", {
        statuses: ["todo", "in_review", "done"],
        gateEnabled: true,
        deciderType: "self_agent",
        decider: "@self",
      }),
    ],
  },
  {
    id: "document",
    name: "通用文档协作模板",
    description: "适用于多人和 Agent 共同完成文档编写与评审。",
    source: "preset",
    stages: [
      stage("大纲设计", "需求说明", "大纲.md", ""),
      stage("内容撰写", "大纲.md", "正文.md", ""),
      stage("评审修订", "正文.md", "修订版.md", "", {
        statuses: ["todo", "in_review", "done"],
        gateEnabled: true,
        deciderType: "agent",
        decider: "@review-agent",
      }),
      stage("审核定稿", "修订版.md", "终稿.md", ""),
    ],
  },
  {
    id: "blank",
    name: "空白 SOP",
    description: "从一个空白 Stage 开始设计当前 Project 的流程。",
    source: "blank",
    stages: [stage("", "", "", "")],
  },
];

function blankSopTemplate(): SopTemplate {
  const template = SOP_TEMPLATES.find((item) => item.id === "blank");
  if (!template) throw new Error("blank SOP template is missing");
  return template;
}

let stageSequence = 0;
function cloneStages(template: SopTemplate) {
  return template.stages.map((item, index) => ({
    ...item,
    statuses: [...item.statuses],
    id: `sop-stage-${++stageSequence}`,
    stableKey: `stage_${index + 1}`,
    rollbackStageKey: index > 0 ? `stage_${index}` : null,
  }));
}
function resolvedGateType(item: SopStageDraft): WorkflowGateType {
  if (!item.gateEnabled) return "none";
  if (item.deciderType === "human") return "human";
  return item.requireHuman ? "hybrid" : "agent";
}

function objectString(value: Record<string, unknown>, key: string): string {
  return typeof value[key] === "string" ? value[key] : "";
}

function normalizeSkillRefs(
  refs: readonly string[],
  workspaceSkills: readonly SkillSummary[],
): string[] {
  return refs.map((ref) => {
    const match = workspaceSkills.find(
      (skill) => skill.id === ref || skill.name === ref,
    );
    return match?.id ?? ref;
  });
}

function resolveStageActors(
  drafts: SopStageDraft[],
  members: readonly MemberWithUser[],
  agents: readonly Agent[],
): SopStageDraft[] {
  const defaultHuman =
    members.find((member) => member.role === "owner") ?? members[0];
  const activeAgents = agents.filter((agent) => !agent.archived_at);
  const defaultAgent = activeAgents[0];

  return drafts.map((draft) => {
    const humanExists = members.some(
      (member) => member.user_id === draft.decider,
    );
    const agentExists = activeAgents.some((agent) => agent.id === draft.decider);
    const finalHumanExists = members.some(
      (member) => member.user_id === draft.humanDecider,
    );
    return {
      ...draft,
      decider:
        draft.deciderType === "human"
          ? humanExists
            ? draft.decider
            : (defaultHuman?.user_id ?? "")
          : draft.deciderType === "agent"
            ? agentExists
              ? draft.decider
              : (defaultAgent?.id ?? "")
            : "@self",
      requireHuman:
        draft.deciderType === "human" ? true : draft.requireHuman,
      humanDecider:
        draft.requireHuman && draft.deciderType !== "human"
          ? finalHumanExists
            ? draft.humanDecider
            : (defaultHuman?.user_id ?? "")
          : draft.humanDecider,
    };
  });
}

function gateIsConfigured(
  draft: SopStageDraft,
  members: readonly MemberWithUser[],
  agents: readonly Agent[],
): boolean {
  if (!draft.gateEnabled) return true;
  if (
    draft.deciderType === "human" &&
    !members.some((member) => member.user_id === draft.decider)
  ) {
    return false;
  }
  if (
    draft.deciderType === "agent" &&
    !agents.some((agent) => !agent.archived_at && agent.id === draft.decider)
  ) {
    return false;
  }
  return !(
    draft.deciderType !== "human" &&
    draft.requireHuman &&
    !members.some((member) => member.user_id === draft.humanDecider)
  );
}

function ArtifactMarkdownEditor({
  editorKey,
  icon,
  label,
  helper,
  placeholder,
  value,
  onChange,
}: {
  editorKey: string;
  icon: React.ReactNode;
  label: string;
  helper: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useT("workflows");
  return (
    <div className="min-w-0">
      <div className="mb-1.5 flex items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 text-caption font-medium">
          {icon}
          {label}
        </span>
        <Badge variant="outline" className="font-mono text-micro">
          {t(($) => $.designer.markdown)}
        </Badge>
      </div>
      <div className="overflow-hidden rounded-lg border border-border/80 bg-card focus-within:border-brand/40">
        <ContentEditor
          key={editorKey}
          defaultValue={value}
          onUpdate={onChange}
          placeholder={placeholder}
          className="min-h-28 max-h-60 overflow-y-auto px-3 py-2.5 text-body"
          showBubbleMenu
          disableMentions
        />
      </div>
      <p className="mt-1 text-micro leading-4 text-muted-foreground">{helper}</p>
    </div>
  );
}

function StageSkillSelector({
  skills,
  selectedRefs,
  loading,
  onChange,
}: {
  skills: readonly SkillSummary[];
  selectedRefs: readonly string[];
  loading: boolean;
  onChange: (next: string[]) => void;
}) {
  const { t } = useT("workflows");
  const [expanded, setExpanded] = useState(false);
  const selectedIds = useMemo(
    () =>
      new Set(
        skills
          .filter((skill) =>
            selectedRefs.some(
              (ref) => ref === skill.id || ref === skill.name,
            ),
          )
          .map((skill) => skill.id),
      ),
    [selectedRefs, skills],
  );
  const unresolved = selectedRefs.filter(
    (ref) => !skills.some((skill) => skill.id === ref || skill.name === ref),
  );

  const toggle = (skill: SkillSummary) => {
    const withoutSkill = selectedRefs.filter(
      (ref) => ref !== skill.id && ref !== skill.name,
    );
    onChange(
      selectedIds.has(skill.id)
        ? withoutSkill
        : [...withoutSkill, skill.id],
    );
  };

  const originLabel = (skill: SkillSummary) => {
    const origin = skill.config.origin;
    if (!origin || typeof origin !== "object" || Array.isArray(origin)) return null;
    const data = origin as Record<string, unknown>;
    const source = typeof data.type === "string" ? data.type : null;
    const revision = typeof data.revision === "string" ? data.revision.slice(0, 8) : null;
    return [source, revision].filter(Boolean).join(" · ") || null;
  };

  return (
    <div>
      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 text-caption font-medium">
          <Wrench className="size-3.5 text-muted-foreground" />
          {t(($) => $.designer.required_skills)}
        </span>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-7 gap-1.5 px-2 text-caption"
          onClick={() => setExpanded((value) => !value)}
        >
          {expanded ? <X className="size-3" /> : <ChevronDown className="size-3" />}
          {expanded
            ? t(($) => $.designer.collapse_skills)
            : t(($) => $.designer.choose_skills)}
        </Button>
      </div>
      <div className="mt-1.5 rounded-lg border border-border/80 bg-muted/10 p-2.5">
        {selectedIds.size === 0 && unresolved.length === 0 ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.designer.no_skills_selected)}
          </p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {skills
              .filter((skill) => selectedIds.has(skill.id))
              .map((skill) => {
                const origin = originLabel(skill);
                return (
                  <Badge key={skill.id} variant="secondary" className="gap-1.5">
                    <FileCheck2 className="size-3" />
                    {skill.name}
                    {origin && <span className="font-mono text-micro text-muted-foreground">{origin}</span>}
                  </Badge>
                );
              })}
            {unresolved.map((ref) => (
              <Badge key={ref} variant="outline" className="gap-1.5">
                {ref}
                <button
                  type="button"
                  className="rounded-sm text-muted-foreground hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  aria-label={t(($) => $.designer.remove_skill, { name: ref })}
                  onClick={() => onChange(selectedRefs.filter((value) => value !== ref))}
                >
                  <X className="size-3" />
                </button>
              </Badge>
            ))}
          </div>
        )}
        {expanded && (
          <div className="mt-2.5 border-t border-border/70 pt-2.5">
            <SkillPickerList
              skills={skills}
              selectedIds={selectedIds}
              onToggle={toggle}
              loading={loading}
              emptyMessage={t(($) => $.designer.no_workspace_skills)}
              noMatchMessage={t(($) => $.designer.no_skill_matches)}
            />
          </div>
        )}
        <p className="mt-2 text-micro text-muted-foreground">
          {t(($) => $.designer.skill_snapshot_hint)}
        </p>
      </div>
    </div>
  );
}

function GateActorPicker({
  kind,
  value,
  members,
  agents,
  onChange,
}: {
  kind: "human" | "agent";
  value: string;
  members: readonly MemberWithUser[];
  agents: readonly Agent[];
  onChange: (id: string) => void;
}) {
  const { t } = useT("workflows");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const query = filter.trim().toLowerCase();
  const matches = (name: string) =>
    !query || name.toLowerCase().includes(query) || matchesPinyin(name, query);
  const activeAgents = agents.filter((agent) => !agent.archived_at);
  const filteredMembers = members.filter((member) => matches(member.name));
  const filteredAgents = activeAgents.filter((agent) => matches(agent.name));
  const selectedMember = members.find((member) => member.user_id === value);
  const selectedAgent = activeAgents.find((agent) => agent.id === value);
  const selected = kind === "human" ? selectedMember : selectedAgent;

  return (
    <PropertyPicker
      open={open}
      onOpenChange={setOpen}
      width="w-72"
      align="start"
      searchable
      searchPlaceholder={
        kind === "human"
          ? t(($) => $.designer.search_human)
          : t(($) => $.designer.search_agent)
      }
      onSearchChange={setFilter}
      triggerRender={
        <button
          type="button"
          className="flex min-h-10 w-full items-center gap-2 rounded-lg border border-border/80 bg-background px-3 py-2 text-left transition-colors hover:border-brand/35 hover:bg-muted/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
      }
      trigger={
        selected ? (
          <>
            <ActorAvatar
              actorType={kind === "human" ? "member" : "agent"}
              actorId={kind === "human" ? selectedMember!.user_id : selectedAgent!.id}
              size="sm"
              showStatusDot={kind === "agent"}
            />
            <span className="min-w-0 flex-1 truncate text-body font-medium">
              {selected.name}
            </span>
            <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
          </>
        ) : (
          <>
            {kind === "human" ? (
              <UserRound className="size-4 text-muted-foreground" />
            ) : (
              <Bot className="size-4 text-muted-foreground" />
            )}
            <span className="min-w-0 flex-1 text-body text-muted-foreground">
              {kind === "human"
                ? t(($) => $.designer.select_human)
                : t(($) => $.designer.select_agent)}
            </span>
            <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
          </>
        )
      }
    >
      {kind === "human" ? (
        filteredMembers.length > 0 ? (
          <PickerSection label={t(($) => $.designer.space_members)}>
            {filteredMembers.map((member) => (
              <PickerItem
                key={member.user_id}
                selected={member.user_id === value}
                onClick={() => {
                  onChange(member.user_id);
                  setOpen(false);
                }}
              >
                <ActorAvatar actorType="member" actorId={member.user_id} size="sm" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-medium">{member.name}</span>
                  <span className="block truncate text-caption text-muted-foreground">
                    {member.email}
                  </span>
                </span>
              </PickerItem>
            ))}
          </PickerSection>
        ) : (
          <PickerEmpty />
        )
      ) : filteredAgents.length > 0 ? (
        <PickerSection label={t(($) => $.designer.space_agents)}>
          {filteredAgents.map((agent) => (
            <PickerItem
              key={agent.id}
              selected={agent.id === value}
              onClick={() => {
                onChange(agent.id);
                setOpen(false);
              }}
            >
              <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
              <span className="min-w-0 flex-1">
                <span className="block truncate font-medium">{agent.name}</span>
                {agent.description && (
                  <span className="block truncate text-caption text-muted-foreground">
                    {agent.description}
                  </span>
                )}
              </span>
            </PickerItem>
          ))}
        </PickerSection>
      ) : (
        <PickerEmpty />
      )}
    </PropertyPicker>
  );
}

function updateArtifactDescription(spec: Record<string, unknown> | undefined, value: string) {
  const updated = { ...spec };
  if (value.trim()) updated.default_artifact = value.trim();
  else delete updated.default_artifact;
  return updated;
}

function stagesFromWorkflow(workflow: Workflow): SopStageDraft[] {
  return (workflow.current_version?.stages ?? []).map((item) => {
    const completionRule = item.completion_rule ?? {};
    const inputSpec = item.input_spec ?? {};
    const outputSpec = item.output_spec ?? {};
    const gate = item.gate ?? {};
    const mode = completionRule.evaluation_mode;
    const allowedStatuses = completionRule.allowed_task_statuses;
    const gateType = gate.type;
    const deciderType = gate.decider_type;
    return {
      id: `sop-stage-${++stageSequence}`,
      executionMode: item.execution_mode ?? "manual",
      executorId: item.executor_id ?? null,
      inputSpec: { ...inputSpec },
      inputBindings: { ...item.input_bindings },
      stableKey: item.stable_key,
      rollbackStageKey: item.rollback_stage_key ?? null,
      outputSpec: { ...outputSpec },
      completionRule: { ...completionRule }, reworkPolicy:{...item.rework_policy}, retryPolicy:{...item.retry_policy},
      name: item.name,
      description: item.description ?? "",
      statuses: Array.isArray(allowedStatuses)
        ? allowedStatuses.filter((value): value is string => typeof value === "string")
        : [...DEFAULT_STATUSES],
      evaluationMode:
        mode === "manual" || mode === "scheduled"
          ? mode
          : "on_task_change",
      schedule: objectString(completionRule, "schedule"),
      input: objectString(inputSpec, "default_artifact"),
      output: objectString(outputSpec, "default_artifact"),
      skills: [...item.required_skills],
      allowedMcpServerIds: item.allowed_mcp_server_ids ?? null,
      gateEnabled: gateType !== undefined && gateType !== "none",
      deciderType:
        deciderType === "self_agent" || deciderType === "agent"
          ? deciderType
          : "human",
      decider: objectString(gate, "decider"),
      requireHuman: gate.require_human === true,
      humanDecider: objectString(gate, "human_decider"),
    };
  });
}

export function ProjectSopDesigner({
  projectId,
  workflowId,
}: {
  projectId?: string;
  workflowId?: string;
}) {
  const { t } = useT("workflows");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { data: availableWorkflows = [], isLoading } = useQuery(
    workflowListOptions(workspaceId, projectId),
  );
  const workflows = useMemo(
    () =>
      projectId
        ? availableWorkflows
        : availableWorkflows.filter((workflow) => !workflow.project_id),
    [availableWorkflows, projectId],
  );
  const { data: runs = [] } = useQuery(
    workflowInstancesOptions(workspaceId, undefined, projectId),
  );
  const { data: workspaceSkills = [], isLoading: skillsLoading } = useQuery(
    skillListOptions(workspaceId),
  );
  const { data: workspaceMCP } = useQuery(workspaceMCPServersOptions(workspaceId));
  const { data: members = [] } = useQuery(memberListOptions(workspaceId));
  const { data: agents = [] } = useQuery(agentListOptions(workspaceId));
  const createWorkflow = useCreateWorkflow();
  const createInstance = useCreateWorkflowInstance();
  const archiveInstance = useArchiveWorkflowInstance();
  const [startingWorkflowId, setStartingWorkflowId] = useState<string>();
  const [workflowToStart, setWorkflowToStart] = useState<{
    id: string;
    name: string;
  }>();
  const [runName, setRunName] = useState("");
  const [runInputsText, setRunInputsText] = useState("{}");
  const startingRun = useRef(false);
  const runInputs = useMemo(() => {
    try {
      const parsed: unknown = JSON.parse(runInputsText);
      return parsed !== null && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : null;
    } catch { return null; }
  }, [runInputsText]);
  const [runToArchive, setRunToArchive] = useState<WorkflowInstance>();
  const [runToAdjust, setRunToAdjust] = useState<WorkflowInstance>();
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string>();
  const [editing, setEditing] = useState(false);
  const [editingScope, setEditingScope] = useState<"space" | "project">("space");
  const [sourceWorkflowName, setSourceWorkflowName] = useState<string>();
  const [templateName, setTemplateName] = useState("");
  const [templateSource, setTemplateSource] = useState<SopTemplate["source"]>("blank");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [stages, setStages] = useState<SopStageDraft[]>([]);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());

  const selectedWorkflow = workflows.find(
    (workflow) => workflow.id === selectedWorkflowId,
  );
  const spaceWorkflows = workflows.filter((workflow) => !workflow.project_id);
  const projectWorkflows = workflows.filter(
    (workflow) => workflow.project_id === projectId,
  );
  const activeRuns = runs.filter((run) => !run.archived_at);
  const archivedRuns = runs.filter((run) => Boolean(run.archived_at));
  const invalidRollback = stages.some((stage) => !validReworkPolicyDraft(stage.reworkPolicy) || !validRetryPolicyDraft(stage.retryPolicy)) || stages.some((item, index) => item.rollbackStageKey && !stages.slice(0, index).some((previous) => previous.stableKey === item.rollbackStageKey));
  const invalidGateCount = stages.filter(
    (stage) => !gateIsConfigured(stage, members, agents),
  ).length;

  useEffect(() => {
    const preferred = workflowId ?? selectedWorkflowId;
    if (preferred && workflows.some((workflow) => workflow.id === preferred)) {
      if (preferred !== selectedWorkflowId) setSelectedWorkflowId(preferred);
      return;
    }
    setSelectedWorkflowId(workflows[0]?.id);
  }, [selectedWorkflowId, workflowId, workflows]);

  const begin = (
    template: SopTemplate,
    scope: "space" | "project" = "space",
  ) => {
    const nextStages = resolveStageActors(
      cloneStages(template),
      members,
      agents,
    );
    setTemplateName(template.name);
    setTemplateSource(template.source);
    setName(template.source === "blank" ? "" : template.name);
    setDescription(template.source === "blank" ? "" : template.description);
    setEditingScope(scope);
    setSourceWorkflowName(undefined);
    setStages(nextStages);
    setExpanded(new Set(nextStages.map((item) => item.id)));
    setEditing(true);
  };
  const customize = (workflow: Workflow) => {
    const nextStages = resolveStageActors(
      stagesFromWorkflow(workflow),
      members,
      agents,
    );
    if (!projectId || nextStages.length === 0) return;
    setTemplateName(workflow.name);
    setTemplateSource("blank");
    setName(`${workflow.name} Project 版`);
    setDescription(workflow.description ?? "");
    setEditingScope("project");
    setSourceWorkflowName(workflow.name);
    setStages(nextStages);
    setExpanded(new Set(nextStages.map((item) => item.id)));
    setEditing(true);
  };
  const updateStage = (id: string, patch: Partial<Omit<SopStageDraft, "id">>) =>
    setStages((current) => current.map((item) => item.id === id ? { ...item, ...patch } : item));
  const moveStage = (index: number, delta: -1 | 1) =>
    setStages((current) => {
      const target = index + delta;
      if (target < 0 || target >= current.length) return current;
      const next = [...current];
      [next[index], next[target]] = [next[target]!, next[index]!];
      return next;
    });
  const addStage = () => {
    const item = cloneStages(blankSopTemplate())[0]!;
    let number = 1;
    while (stages.some((candidate) => candidate.stableKey === `stage_${number}`)) number++;
    item.stableKey = `stage_${number}`;
    setStages((current) => [...current, item]);
    setExpanded((current) => new Set(current).add(item.id));
  };
  const addCustomStatus = (event: KeyboardEvent<HTMLInputElement>, item: SopStageDraft) => {
    if (event.key !== "Enter") return;
    event.preventDefault();
    const value = event.currentTarget.value.trim();
    if (!value || item.statuses.includes(value)) return;
    updateStage(item.id, { statuses: [...item.statuses, value] });
    event.currentTarget.value = "";
  };
  const publish = () => {
    const validStages = stages.filter((item) => item.name.trim());
    if (!name.trim() || validStages.length === 0 || invalidRollback || invalidGateCount > 0 || validStages.some((stage) => stage.executionMode === "agent" && !agents.some((agent) => agent.id === stage.executorId && !agent.archived_at))) return;
    const reservedKeys = new Set(validStages.flatMap((item) => item.stableKey ? [item.stableKey] : []));
    const stageKeys = validStages.map((item, index) => {
      if (item.stableKey) return item.stableKey;
      let number = index + 1;
      while (reservedKeys.has(`stage_${number}`)) number++;
      const key = `stage_${number}`;
      reservedKeys.add(key);
      return key;
    });
    createWorkflow.mutate(
      {
        ...(editingScope === "project" && projectId
          ? { project_id: projectId }
          : {}),
        name: name.trim(),
        description: description.trim() || null,
        publish: true,
        stages: validStages.map((item, index) => ({
          stable_key: stageKeys[index]!,
          name: item.name.trim(),
          description: item.description.trim() || null,
          execution_mode: item.executionMode ?? "manual",
          executor_id: item.executorId ?? null,
          rework_policy: item.reworkPolicy ?? {}, retry_policy: item.retryPolicy ?? {},
          completion_rule: item.executionMode === "agent" ? (item.completionRule ?? {type:"validated_result"}) : {
            type: "all_tasks_terminal",
            evaluation_mode: item.evaluationMode,
            ...(item.evaluationMode === "scheduled" && item.schedule.trim()
              ? { schedule: item.schedule.trim() }
              : {}),
            allowed_task_statuses: item.statuses,
          },
          input_bindings: item.inputBindings ?? {},
          input_spec: updateArtifactDescription(item.inputSpec, item.input),
          output_spec: updateArtifactDescription(item.outputSpec, item.output),
          required_skills: normalizeSkillRefs(item.skills, workspaceSkills),
          allowed_mcp_server_ids: item.allowedMcpServerIds,
          gate: {
            type: resolvedGateType(item),
            decider_type: item.deciderType,
            decider: item.decider.trim() || null,
            require_human:
              item.deciderType === "human" ? true : item.requireHuman,
            human_decider:
              item.deciderType === "human"
                ? item.decider.trim() || null
                : item.requireHuman
                  ? item.humanDecider.trim() || null
                  : null,
          },
          rollback_stage_key: item.rollbackStageKey ?? null,
        })),
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.designer.created));
          setEditing(false);
          setSourceWorkflowName(undefined);
        },
        onError: (error) => toast.error(error instanceof Error ? error.message : t(($) => $.designer.create_failed)),
      },
    );
  };
  const prepareStartWorkflow = (workflowId: string, workflowName: string) => {
    if (!projectId) return;
    const sequence =
      runs.filter((run) => run.workflow_id === workflowId).length + 1;
    setRunInputsText("{}");
    setWorkflowToStart({ id: workflowId, name: workflowName });
    setRunName(
      t(($) => $.designer.run_default_name, {
        name: workflowName,
        sequence,
      }),
    );
  };
  const startWorkflow = () => {
    const title = runName.trim();
    if (!workflowToStart || !title || !projectId || !runInputs || startingRun.current || createInstance.isPending) return;
    startingRun.current = true;
    setStartingWorkflowId(workflowToStart.id);
    createInstance.mutate(
      {
        workflowId: workflowToStart.id,
        data: {
          title,
          project_id: projectId,
          inputs: runInputs,
          start: true,
        },
      },
      {
        onSuccess: () => {
          toast.success(t(($) => $.designer.run_started));
          setWorkflowToStart(undefined);
          setRunName("");
        },
        onError: (error) =>
          toast.error(
            error instanceof Error
              ? error.message
              : t(($) => $.designer.run_failed),
          ),
        onSettled: () => { startingRun.current = false; setStartingWorkflowId(undefined); },
      },
    );
  };

  const archiveRun = () => {
    if (!runToArchive) return;
    archiveInstance.mutate(runToArchive.id, {
      onSuccess: () => {
        toast.success(t(($) => $.designer.run_archived));
        setRunToArchive(undefined);
      },
      onError: (error) =>
        toast.error(
          error instanceof Error
            ? error.message
            : t(($) => $.designer.archive_failed),
        ),
    });
  };

  if (!editing) {
    return (
      <>
        <div className="min-h-0 flex-1 overflow-y-auto bg-background">
          <div className="mx-auto w-full max-w-5xl px-5 py-5">
          <header className="flex items-start justify-between gap-4">
            <div>
              <div className="flex items-center gap-2">
                <GitBranch className="size-4 text-brand" />
                <h2 className="text-title-sm font-semibold">{t(($) => $.designer.library_title)}</h2>
                <Badge variant="secondary">{workflows.length}</Badge>
              </div>
              <p className="mt-1 max-w-2xl text-body text-muted-foreground">
                {projectId
                  ? t(($) => $.designer.library_description)
                  : t(($) => $.page.description)}
              </p>
            </div>
            <Button size="sm" onClick={() => begin(blankSopTemplate())}>
              <Plus className="size-3.5" />
              {t(($) => $.page.create)}
            </Button>
          </header>

          {projectId && (
            <section className="mt-6 rounded-xl border border-border/80 bg-muted/10 p-3.5">
              <div className="flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                  <Play className="size-4 text-brand" />
                  <h3 className="text-body font-semibold">
                    {t(($) => $.designer.project_runs)}
                  </h3>
                </div>
                <Badge variant="secondary">{activeRuns.length}</Badge>
              </div>
              <p className="mt-1 text-caption text-muted-foreground">
                {t(($) => $.designer.project_runs_hint)}
              </p>
              {activeRuns.length === 0 ? (
                <div className="mt-3 rounded-lg border border-dashed border-border px-3 py-5 text-center text-caption text-muted-foreground">
                  {t(($) => $.designer.no_project_runs)}
                </div>
              ) : (
                <div className="mt-3 grid gap-2 sm:grid-cols-2">
                  {activeRuns.map((run: WorkflowInstance) => {
                    const workflow = workflows.find(
                      (item) => item.id === run.workflow_id,
                    );
                    const runProjectId = run.project_id ?? projectId;
                    const canOpenRun =
                      Boolean(runProjectId) &&
                      (run.status === "active" || run.status === "waiting");
                    const currentStageName =
                      run.current_stage_name ??
                      workflow?.current_version?.stages.find(
                        (stage) => stage.id === run.current_stage_id,
                      )?.name;
                    return (
                      <article
                        key={run.id}
                        className="flex min-h-20 items-start gap-3 rounded-lg border border-border/80 bg-background p-3"
                      >
                        <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-brand/8 text-brand">
                          {run.status === "completed" ? (
                            <Check className="size-4" />
                          ) : (
                            <Clock3 className="size-4" />
                          )}
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-start justify-between gap-2">
                            <div className="min-w-0">
                              <p className="truncate text-body font-semibold">
                                {run.title}
                              </p>
                              <p className="truncate text-caption text-muted-foreground">
                                {workflow?.name ?? t(($) => $.designer.unknown_sop)}
                              </p>
                            </div>
                            <div className="flex shrink-0 items-center gap-1">
                              <Badge variant="outline">
                                {t(($) => $.designer.run_status[run.status])}
                              </Badge>
                              {run.can_edit && (
                                <Button
                                  type="button"
                                  variant="ghost"
                                  size="icon-sm"
                                  title={t(($) => $.designer.run_plan_action)}
                                  aria-label={t(($) => $.designer.run_plan_action)}
                                  onClick={() => setRunToAdjust(run)}
                                >
                                  <SlidersHorizontal className="size-3.5" />
                                </Button>
                              )}
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-xs"
                                disabled={run.task_count > 0}
                                title={
                                  run.task_count > 0
                                    ? t(($) => $.designer.archive_blocked, {
                                        count: run.task_count,
                                      })
                                    : t(($) => $.designer.archive_run)
                                }
                                aria-label={t(($) => $.designer.archive_run)}
                                onClick={() => setRunToArchive(run)}
                              >
                                <Archive className="size-3.5" />
                              </Button>
                            </div>
                          </div>
                          <p className="mt-1.5 flex items-center gap-1.5 text-caption text-muted-foreground">
                            <CircleDot className="size-3" />
                            {currentStageName ?? t(($) => $.designer.no_current_stage)}
                          </p>
                          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-micro text-muted-foreground">
                            <span>
                              {t(($) => $.designer.run_task_count, {
                                count: run.task_count,
                              })}
                            </span>
                            {run.stage_count > 0 && run.current_stage_index !== null && (
                              <span>
                                {t(($) => $.designer.run_plan_progress, {
                                  current: run.current_stage_index + 1,
                                  total: run.stage_count,
                                })}
                              </span>
                            )}
                            {run.source_version > 0 && (
                              <span>
                                {t(($) => $.designer.run_plan_source_version, {
                                  version: run.source_version,
                                })}
                              </span>
                            )}
                            {run.revision > 1 && (
                              <span>
                                {t(($) => $.designer.run_plan_revision, {
                                  revision: run.revision,
                                })}
                              </span>
                            )}
                          </div>
                          {canOpenRun && runProjectId && (
                            <Button
                              type="button"
                              variant="link"
                              size="sm"
                              className="mt-1 h-auto px-0 text-caption"
                              onClick={() =>
                                navigation.push(
                                  paths.projectWorkflowRun(runProjectId, run.id),
                                )
                              }
                            >
                              {t(($) => $.designer.open_run_board)}
                              <ChevronRight className="size-3.5" />
                            </Button>
                          )}
                        </div>
                      </article>
                    );
                  })}
                </div>
              )}
              {archivedRuns.length > 0 && (
                <details className="mt-3 rounded-lg border border-border/70 bg-background">
                  <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2.5 text-caption font-medium text-muted-foreground hover:text-foreground">
                    <History className="size-3.5" />
                    <span>{t(($) => $.designer.archived_runs)}</span>
                    <Badge variant="secondary" className="ml-auto">
                      {archivedRuns.length}
                    </Badge>
                    <ChevronDown className="size-3.5" />
                  </summary>
                  <div className="grid gap-2 border-t border-border/70 p-2 sm:grid-cols-2">
                    {archivedRuns.map((run) => {
                      const workflow = workflows.find(
                        (item) => item.id === run.workflow_id,
                      );
                      const archivedBy = members.find(
                        (member) => member.user_id === run.archived_by,
                      );
                      return (
                        <article
                          key={run.id}
                          className="rounded-lg bg-muted/25 px-3 py-2.5"
                        >
                          <div className="flex items-start gap-2">
                            <Archive className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                            <div className="min-w-0 flex-1">
                              <p className="truncate text-caption font-semibold">
                                {run.title}
                              </p>
                              <p className="truncate text-micro text-muted-foreground">
                                {workflow?.name ??
                                  t(($) => $.designer.unknown_sop)}
                              </p>
                              <p className="mt-1 text-micro text-muted-foreground">
                                {t(($) => $.designer.archived_record, {
                                  name:
                                    archivedBy?.name ??
                                    t(($) => $.designer.unknown_member),
                                  time: run.archived_at
                                    ? new Date(run.archived_at).toLocaleString()
                                    : "—",
                                })}
                              </p>
                            </div>
                          </div>
                        </article>
                      );
                    })}
                  </div>
                </details>
              )}
            </section>
          )}

          <section className="mt-6">
            <div className="mb-2 flex items-center gap-2">
              <LayoutTemplate className="size-4 text-muted-foreground" />
              <h3 className="text-body font-semibold">{t(($) => $.designer.templates)}</h3>
            </div>
            <div className="grid gap-2 md:grid-cols-2">
              {SOP_TEMPLATES.map((template) => (
                <button
                  key={template.id}
                  type="button"
                  className="group flex min-h-24 items-start gap-3 rounded-xl border border-border/80 p-3 text-left hover:border-brand/35 hover:bg-brand/5"
                  onClick={() => begin(template)}
                >
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground group-hover:bg-brand/10 group-hover:text-brand">
                    {template.source === "blank" ? <Plus className="size-4" /> : <GitBranch className="size-4" />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="flex items-center gap-2">
                      <span className="text-body font-semibold">{template.name}</span>
                      <span className="text-micro text-muted-foreground">{t(($) => $.designer.stage_count, { count: template.stages.length })}</span>
                    </span>
                    <span className="mt-1 block text-caption leading-5 text-muted-foreground">{template.description}</span>
                  </span>
                </button>
              ))}
            </div>
          </section>

          <section className="mt-7 pb-8">
            <div className="mb-3 flex items-center justify-between gap-3">
              <div className="flex items-center gap-2">
                <GitBranch className="size-4 text-muted-foreground" />
                <h3 className="text-body font-semibold">
                  {projectId
                    ? t(($) => $.designer.existing)
                    : t(($) => $.designer.space_assets)}
                </h3>
              </div>
              <span className="text-caption text-muted-foreground">
                {projectId
                  ? t(($) => $.designer.project_available_hint)
                  : t(($) => $.designer.space_asset_hint)}
              </span>
            </div>
            {isLoading ? (
              <div className="grid gap-3 lg:grid-cols-[minmax(220px,0.8fr)_minmax(0,1.7fr)]">
                <div className="h-64 animate-pulse rounded-xl bg-muted/50" />
                <div className="h-64 animate-pulse rounded-xl bg-muted/50" />
              </div>
            ) : workflows.length === 0 ? (
              <div className="rounded-xl border border-dashed border-border px-4 py-7 text-center">
                <p className="text-body font-medium">{t(($) => $.page.empty_title)}</p>
                <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.designer.empty_existing)}</p>
              </div>
            ) : (
              <div className="grid min-h-[360px] overflow-hidden rounded-xl border border-border/80 lg:grid-cols-[minmax(220px,0.8fr)_minmax(0,1.7fr)]">
                <aside className="border-b border-border/70 bg-muted/15 p-2 lg:border-b-0 lg:border-r">
                  {spaceWorkflows.length > 0 && (
                    <div>
                      <div className="flex h-8 items-center justify-between px-2 text-caption font-medium text-muted-foreground">
                        <span>{t(($) => $.designer.space_assets)}</span>
                        <Badge variant="outline">{spaceWorkflows.length}</Badge>
                      </div>
                      <div className="space-y-1">
                        {spaceWorkflows.map((workflow) => (
                          <button
                            key={workflow.id}
                            type="button"
                            className={cn(
                              "flex min-h-12 w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left transition-colors",
                              selectedWorkflowId === workflow.id
                                ? "bg-background text-foreground shadow-sm ring-1 ring-border/70"
                                : "text-muted-foreground hover:bg-background/70 hover:text-foreground",
                            )}
                            onClick={() => setSelectedWorkflowId(workflow.id)}
                          >
                            <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-brand/8 text-brand">
                              <GitBranch className="size-3.5" />
                            </span>
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-body font-medium">{workflow.name}</span>
                              <span className="block text-micro text-muted-foreground">
                                {t(($) => $.designer.stage_count, {
                                  count: workflow.current_version?.stages.length ?? 0,
                                })}
                              </span>
                            </span>
                            <ChevronRight className="size-3.5 shrink-0 opacity-50" />
                          </button>
                        ))}
                      </div>
                    </div>
                  )}

                  {projectId && projectWorkflows.length > 0 && (
                    <div className={cn(spaceWorkflows.length > 0 && "mt-3 border-t border-border/70 pt-2")}>
                      <div className="flex h-8 items-center justify-between px-2 text-caption font-medium text-muted-foreground">
                        <span>{t(($) => $.designer.project_variants)}</span>
                        <Badge variant="outline">{projectWorkflows.length}</Badge>
                      </div>
                      <div className="space-y-1">
                        {projectWorkflows.map((workflow) => (
                          <button
                            key={workflow.id}
                            type="button"
                            className={cn(
                              "flex min-h-12 w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left transition-colors",
                              selectedWorkflowId === workflow.id
                                ? "bg-background text-foreground shadow-sm ring-1 ring-border/70"
                                : "text-muted-foreground hover:bg-background/70 hover:text-foreground",
                            )}
                            onClick={() => setSelectedWorkflowId(workflow.id)}
                          >
                            <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-amber-500/10 text-amber-700 dark:text-amber-400">
                              <Copy className="size-3.5" />
                            </span>
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-body font-medium">{workflow.name}</span>
                              <span className="block text-micro text-muted-foreground">
                                {t(($) => $.designer.project_variant)}
                              </span>
                            </span>
                            <ChevronRight className="size-3.5 shrink-0 opacity-50" />
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                </aside>

                <div className="min-w-0 bg-background p-4">
                  {selectedWorkflow ? (
                    <div>
                      <header className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="text-title-sm font-semibold">{selectedWorkflow.name}</h3>
                            <Badge variant={selectedWorkflow.project_id ? "outline" : "secondary"}>
                              {selectedWorkflow.project_id
                                ? t(($) => $.designer.project_variant)
                                : t(($) => $.designer.space_asset)}
                            </Badge>
                            <Badge variant="outline">
                              {t(($) => $.detail.version, {
                                version: selectedWorkflow.current_version?.version ?? 1,
                              })}
                            </Badge>
                          </div>
                          <p className="mt-1 max-w-2xl text-body leading-6 text-muted-foreground">
                            {selectedWorkflow.description || t(($) => $.designer.no_description)}
                          </p>
                        </div>
                        <div className="flex flex-wrap items-center gap-2">
                          {projectId && (
                            <Button
                              size="sm"
                              variant="outline"
                              disabled={!selectedWorkflow.current_version?.stages.length}
                              onClick={() => customize(selectedWorkflow)}
                            >
                              <Copy className="size-3.5" />
                              {t(($) => $.designer.customize_for_project)}
                            </Button>
                          )}
                          {projectId && (
                            <Button
                              size="sm"
                              disabled={
                                selectedWorkflow.status !== "published" ||
                                createInstance.isPending
                              }
                              onClick={() =>
                                prepareStartWorkflow(
                                  selectedWorkflow.id,
                                  selectedWorkflow.name,
                                )
                              }
                            >
                              {startingWorkflowId === selectedWorkflow.id ? (
                                <LoaderCircle className="size-3.5 animate-spin" />
                              ) : (
                                <Play className="size-3.5" />
                              )}
                              {t(($) => $.designer.start_run)}
                            </Button>
                          )}
                        </div>
                      </header>

                      <div className="mt-4 flex flex-wrap gap-x-5 gap-y-2 rounded-lg bg-muted/30 px-3 py-2 text-caption text-muted-foreground">
                        <span>{t(($) => $.designer.stage_count, { count: selectedWorkflow.current_version?.stages.length ?? 0 })}</span>
                        <span>{t(($) => $.designer.active_runs, {
                          count: runs.filter(
                            (run) =>
                              !run.archived_at &&
                              run.workflow_id === selectedWorkflow.id &&
                              run.status !== "completed" &&
                              run.status !== "cancelled",
                          ).length,
                        })}</span>
                        <span>{t(($) => $.detail[selectedWorkflow.status as keyof typeof $.detail])}</span>
                      </div>

                      <div className="mt-5 space-y-2">
                        {(selectedWorkflow.current_version?.stages ?? []).map((stage, index) => {
                          const completionRule = stage.completion_rule ?? {};
                          const gate = stage.gate ?? {};
                          const inputSpec = stage.input_spec ?? {};
                          const outputSpec = stage.output_spec ?? {};
                          const evaluationMode = completionRule.evaluation_mode;
                          const schedule = objectString(completionRule, "schedule");
                          const gateType = gate.type ?? "none";
                          const input = objectString(inputSpec, "default_artifact");
                          const output = objectString(outputSpec, "default_artifact");
                          const deciderType = objectString(gate, "decider_type");
                          const deciderId = objectString(gate, "decider");
                          const humanDeciderId = objectString(gate, "human_decider");
                          const deciderName =
                            deciderType === "human"
                              ? members.find((member) => member.user_id === deciderId)?.name
                              : deciderType === "agent"
                                ? agents.find((agent) => agent.id === deciderId)?.name
                                : deciderType === "self_agent"
                                  ? t(($) => $.designer.current_agent)
                                  : undefined;
                          const humanDeciderName = members.find(
                            (member) => member.user_id === humanDeciderId,
                          )?.name;
                          const skillNames = (stage.required_skills ?? []).map(
                            (ref) =>
                              workspaceSkills.find(
                                (skill) => skill.id === ref || skill.name === ref,
                              )?.name ?? ref,
                          );
                          return (
                            <section key={stage.id} className="rounded-lg border border-border/80 bg-muted/10 px-3 py-3">
                              <div className="flex items-start gap-3">
                                <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-brand text-caption font-semibold text-brand-foreground">
                                  {index + 1}
                                </span>
                                <div className="min-w-0 flex-1">
                                  <div className="flex flex-wrap items-center gap-2">
                                    <h4 className="text-body font-semibold">{stage.name}</h4>
                                    <Badge variant="outline">
                                      <CircleDot className="size-3" />
                                      {t(($) => $.designer.evaluation[evaluationMode === "manual" || evaluationMode === "scheduled" ? evaluationMode : "on_task_change"])}
                                    </Badge>
                                    {schedule && (
                                      <Badge variant="outline">
                                        <CalendarClock className="size-3" />
                                        {schedule}
                                      </Badge>
                                    )}
                                    <Badge variant="outline">
                                      <ShieldCheck className="size-3" />
                                      {t(($) => $.stage[`gate_${gateType}` as keyof typeof $.stage])}
                                    </Badge>
                                  </div>
                                  {stage.description && (
                                    <p className="mt-1 text-caption leading-5 text-muted-foreground">{stage.description}</p>
                                  )}
                                  <div className="mt-2 grid gap-2 text-caption sm:grid-cols-2">
                                    <div className="min-w-0 rounded-md bg-background px-2.5 py-2">
                                      <span className="text-micro font-medium text-muted-foreground">{t(($) => $.designer.default_input)}</span>
                                      {input ? (
                                        <ReadonlyContent content={input} className="mt-1 text-caption" />
                                      ) : (
                                        <p className="mt-0.5 font-medium">{t(($) => $.designer.not_configured)}</p>
                                      )}
                                    </div>
                                    <div className="min-w-0 rounded-md bg-background px-2.5 py-2">
                                      <span className="text-micro font-medium text-muted-foreground">{t(($) => $.designer.default_output)}</span>
                                      {output ? (
                                        <ReadonlyContent content={output} className="mt-1 text-caption" />
                                      ) : (
                                        <p className="mt-0.5 font-medium">{t(($) => $.designer.not_configured)}</p>
                                      )}
                                    </div>
                                  </div>
                                  <div className="mt-2 flex flex-wrap gap-1.5">
                                    {skillNames.length > 0 ? (
                                      skillNames.map((skill) => (
                                        <Badge key={skill} variant="secondary" className="gap-1">
                                          <Wrench className="size-3" />
                                          {skill}
                                        </Badge>
                                      ))
                                    ) : (
                                      <Badge variant="outline">
                                        {t(($) => $.designer.no_skills_selected)}
                                      </Badge>
                                    )}
                                    {gateType !== "none" && deciderName && (
                                      <Badge variant="outline" className="gap-1">
                                        <UserCheck className="size-3" />
                                        {deciderName}
                                      </Badge>
                                    )}
                                    {gate.require_human === true &&
                                      humanDeciderName &&
                                      humanDeciderName !== deciderName && (
                                        <Badge variant="outline" className="gap-1">
                                          <UserRound className="size-3" />
                                          {t(($) => $.designer.final_confirmation_by, {
                                            name: humanDeciderName,
                                          })}
                                        </Badge>
                                      )}
                                  </div>
                                </div>
                              </div>
                            </section>
                          );
                        })}
                      </div>
                    </div>
                  ) : null}
                </div>
              </div>
            )}
          </section>
          </div>
        </div>
        <Dialog
          open={Boolean(workflowToStart)}
          onOpenChange={(open) => {
            if (open || createInstance.isPending) return;
            setWorkflowToStart(undefined);
            setRunName("");
          }}
        >
          <DialogContent className={workflowOperationDialogClassName}>
            <DialogHeader>
              <DialogTitle>{t(($) => $.designer.run_dialog_title)}</DialogTitle>
              <DialogDescription>
                {t(($) => $.designer.run_dialog_description, {
                  name: workflowToStart?.name ?? "",
                })}
              </DialogDescription>
            </DialogHeader>
            <label className="space-y-1.5 text-caption font-medium">
              <span>{t(($) => $.designer.run_name)}</span>
              <Input
                autoFocus
                value={runName}
                disabled={createInstance.isPending}
                placeholder={t(($) => $.designer.run_name_placeholder)}
                onChange={(event) => setRunName(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    startWorkflow();
                  }
                }}
              />
            </label>
            <label className="space-y-1.5 text-caption font-medium">
              <span>{t(($) => $.execution_config.run_inputs)}</span>
              <Textarea className="min-h-32 font-mono" value={runInputsText} disabled={createInstance.isPending} onChange={(event) => setRunInputsText(event.target.value)} />
              <span className="block text-micro font-normal text-muted-foreground">{t(($) => $.execution_config.run_inputs_hint)}</span>
            </label>
            {!runInputs && <p role="alert" className="text-caption text-destructive">{t(($) => $.input_recovery.invalid)}</p>}
            <DialogFooter className="border-t pt-4">
              <Button
                variant="outline"
                disabled={createInstance.isPending}
                onClick={() => {
                  setWorkflowToStart(undefined);
                  setRunName("");
                }}
              >
                {t(($) => $.designer.cancel_run)}
              </Button>
              <Button
                disabled={createInstance.isPending || !runName.trim() || !runInputs}
                onClick={startWorkflow}
              >
                {createInstance.isPending ? (
                  <LoaderCircle className="size-3.5 animate-spin" />
                ) : (
                  <Play className="size-3.5" />
                )}
                {createInstance.isPending
                  ? t(($) => $.designer.starting_run)
                  : t(($) => $.designer.confirm_start)}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
        {runToAdjust && (
          <WorkflowRunPlanDialog
            instanceId={runToAdjust.id}
            sourceName={
              workflows.find((workflow) => workflow.id === runToAdjust.workflow_id)?.name ??
              t(($) => $.designer.unknown_sop)
            }
            open
            onOpenChange={(open) => {
              if (!open) setRunToAdjust(undefined);
            }}
          />
        )}
        <AlertDialog
          open={Boolean(runToArchive)}
          onOpenChange={(open) => {
            if (!open && !archiveInstance.isPending) setRunToArchive(undefined);
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t(($) => $.designer.archive_dialog_title)}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t(($) => $.designer.archive_dialog_description, {
                  name: runToArchive?.title ?? "",
                })}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={archiveInstance.isPending}>
                {t(($) => $.designer.archive_cancel)}
              </AlertDialogCancel>
              <AlertDialogAction
                disabled={archiveInstance.isPending}
                onClick={archiveRun}
              >
                {archiveInstance.isPending && (
                  <LoaderCircle className="size-3.5 animate-spin" />
                )}
                {t(($) => $.designer.archive_confirm)}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-background">
      <header className="flex min-h-14 items-center gap-3 border-b border-border/70 px-5 py-2">
        <Button variant="ghost" size="icon-sm" onClick={() => setEditing(false)}><ArrowLeft /></Button>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-body font-semibold">{t(($) => $.designer.editor_title)}</h2>
            <Badge variant="secondary">{templateName}</Badge>
          </div>
          <p className="text-caption text-muted-foreground">{t(($) => $.designer.editor_hint)}</p>
        </div>
        <Button
          size="sm"
          disabled={
            createWorkflow.isPending ||
            !name.trim() ||
            !stages.some((item) => item.name.trim()) ||
            stages.some((item) => item.name.trim() && item.executionMode === "agent" && !agents.some((agent) => agent.id === item.executorId && !agent.archived_at)) ||
            invalidRollback || invalidGateCount > 0
          }
          onClick={publish}
        >
          <Sparkles className="size-3.5" />
          {createWorkflow.isPending ? t(($) => $.designer.publishing) : t(($) => $.designer.publish)}
        </Button>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-4xl px-5 py-5">
          <div className="space-y-3">
            <label className="block space-y-1.5 text-caption font-medium">
              <span>{t(($) => $.create.name)}</span>
              <Input value={name} onChange={(event) => setName(event.target.value)} placeholder={t(($) => $.create.name_placeholder)} autoFocus />
            </label>
            <label className="block space-y-1.5 text-caption font-medium">
              <span>{t(($) => $.create.description)}</span>
              <Textarea value={description} onChange={(event) => setDescription(event.target.value)} placeholder={t(($) => $.create.description_placeholder)} rows={2} />
            </label>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-lg border border-border/80 bg-muted/20 px-3 py-2">
                <span className="block text-micro font-medium text-muted-foreground">{t(($) => $.designer.source)}</span>
                <span className="mt-0.5 block text-caption font-medium">
                  {sourceWorkflowName
                    ? t(($) => $.designer.source_inherited, {
                        name: sourceWorkflowName,
                      })
                    : templateSource === "preset"
                      ? t(($) => $.designer.source_preset)
                      : t(($) => $.designer.source_user)}
                </span>
              </div>
              <div className="rounded-lg border border-border/80 bg-muted/20 px-3 py-2">
                <span className="block text-micro font-medium text-muted-foreground">{t(($) => $.designer.scope)}</span>
                <span className="mt-0.5 block text-caption font-medium">
                  {editingScope === "project"
                    ? t(($) => $.designer.scope_project)
                    : t(($) => $.designer.scope_space)}
                </span>
              </div>
            </div>
          </div>

          <div className="mb-2 mt-6">
            <h3 className="text-body font-semibold">{t(($) => $.designer.stage_list, { count: stages.length })}</h3>
            <p className="text-caption text-muted-foreground">{t(($) => $.designer.stage_list_hint)}</p>
            {invalidGateCount > 0 && (
              <p className="mt-2 flex items-center gap-1.5 rounded-lg border border-amber-500/30 bg-amber-500/8 px-3 py-2 text-caption text-amber-800 dark:text-amber-300">
                <ShieldCheck className="size-3.5 shrink-0" />
                {t(($) => $.designer.incomplete_gates, {
                  count: invalidGateCount,
                })}
              </p>
            )}
          </div>
          <div className="space-y-2.5">
            {stages.map((item, index) => {
              const open = expanded.has(item.id);
              const defaultHumanId =
                (members.find((member) => member.role === "owner") ?? members[0])
                  ?.user_id ?? "";
              const defaultAgentId =
                agents.find((agent) => !agent.archived_at)?.id ?? "";
              const gateConfigured = gateIsConfigured(item, members, agents);
              return (
                <section key={item.id} className={cn("overflow-hidden rounded-xl border bg-muted/20", item.gateEnabled ? "border-amber-500/35 bg-amber-500/5" : "border-border/80")}>
                  <div className="flex min-h-11 items-center gap-2 px-3">
                    <button
                      type="button"
                      className={cn("flex size-6 shrink-0 items-center justify-center rounded-md text-micro font-semibold text-white", item.gateEnabled ? "bg-amber-600" : "bg-brand")}
                      onClick={() => setExpanded((current) => {
                        const next = new Set(current);
                        if (next.has(item.id)) next.delete(item.id);
                        else next.add(item.id);
                        return next;
                      })}
                    >{index + 1}</button>
                    <Input
                      value={item.name}
                      onChange={(event) => updateStage(item.id, { name: event.target.value })}
                      placeholder={t(($) => $.create.stage_placeholder)}
                      className="h-8 border-transparent bg-transparent px-1 text-body font-semibold shadow-none focus-visible:border-input focus-visible:bg-background"
                    />
                    {item.gateEnabled && <ShieldCheck className="size-3.5 shrink-0 text-amber-600" />}
                    <Button type="button" variant="ghost" size="icon-xs" disabled={index === 0} title={t(($) => $.designer.move_up)} onClick={() => moveStage(index, -1)}><ArrowUp /></Button>
                    <Button type="button" variant="ghost" size="icon-xs" disabled={index === stages.length - 1} title={t(($) => $.designer.move_down)} onClick={() => moveStage(index, 1)}><ArrowDown /></Button>
                    <Button type="button" variant="ghost" size="icon-xs" disabled={stages.length === 1} title={t(($) => $.designer.delete_stage)} className="text-muted-foreground hover:text-destructive" onClick={() => setStages((current) => current.filter((value) => value.id !== item.id))}><Trash2 /></Button>
                  </div>

                  {open && (
                    <div className="space-y-4 border-t border-border/70 bg-background/70 px-3 py-3">
                      <StageExecutionFields value={item} rollbackStages={stages.slice(0, index).filter((stage) => stage.stableKey).map((stage) => ({stableKey: stage.stableKey!, name: stage.name}))} precedingStages={stages.slice(0, index).filter((stage) => stage.executionMode === "agent" && stage.stableKey).map((stage) => ({stableKey: stage.stableKey!, name: stage.name}))} agents={agents} disabled={createWorkflow.isPending} onChange={(patch) => updateStage(item.id, patch)} />
                      <label className="block space-y-1.5 text-caption font-medium">
                        <span>{t(($) => $.designer.stage_description)}</span>
                        <Textarea
                          value={item.description}
                          rows={2}
                          placeholder={t(($) => $.designer.stage_description_placeholder)}
                          onChange={(event) =>
                            updateStage(item.id, {
                              description: event.target.value,
                            })
                          }
                        />
                      </label>

                      {item.executionMode !== "agent" && <>
                      <div className="grid gap-3 sm:grid-cols-2">
                        <label className="space-y-1.5 text-caption font-medium">
                          <span>{t(($) => $.designer.evaluation_label)}</span>
                          <Select
                            items={([
                              "on_task_change",
                              "manual",
                              "scheduled",
                            ] as EvaluationMode[]).map((mode) => ({
                              value: mode,
                              label: t(($) => $.designer.evaluation[mode]),
                            }))}
                            value={item.evaluationMode}
                            onValueChange={(value) => {
                              if (value) {
                                updateStage(item.id, {
                                  evaluationMode: value as EvaluationMode,
                                });
                              }
                            }}
                          >
                            <SelectTrigger>
                              <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                              {(["on_task_change", "manual", "scheduled"] as EvaluationMode[]).map((mode) => (
                                <SelectItem key={mode} value={mode}>
                                  {t(($) => $.designer.evaluation[mode])}
                                </SelectItem>
                              ))}
                            </SelectContent>
                          </Select>
                          <span className="block text-micro font-normal leading-4 text-muted-foreground">
                            {t(($) => $.designer.evaluation_hint[item.evaluationMode])}
                          </span>
                          {item.evaluationMode === "scheduled" && (
                            <span className="block space-y-1 pt-1">
                              <span className="block text-micro font-medium text-muted-foreground">
                                {t(($) => $.designer.schedule_label)}
                              </span>
                              <Input
                                value={item.schedule}
                                placeholder={t(($) => $.designer.schedule_placeholder)}
                                onChange={(event) =>
                                  updateStage(item.id, {
                                    schedule: event.target.value,
                                  })
                                }
                              />
                            </span>
                          )}
                        </label>
                        <div className="rounded-lg border border-border/80 bg-muted/20 px-3 py-2.5">
                          <span className="block text-caption font-medium">{t(($) => $.designer.completion_rule)}</span>
                          <span className="mt-1 block text-caption leading-5 text-muted-foreground">
                            {t(($) => $.designer.completion_all_tasks)}
                          </span>
                        </div>
                      </div>

                      <div>
                        <label className="mb-1.5 block text-caption font-medium">{t(($) => $.designer.statuses)}</label>
                        <div className="flex flex-wrap gap-1.5">
                          {ALL_STATUSES.map((status) => {
                            const active = item.statuses.includes(status);
                            return (
                              <button
                                key={status}
                                type="button"
                                className={cn("flex h-7 items-center gap-1.5 rounded-md border px-2 text-caption", active ? "border-brand/40 bg-brand/8 text-foreground" : "border-border text-muted-foreground hover:bg-muted")}
                                onClick={() => updateStage(item.id, { statuses: active ? item.statuses.filter((value) => value !== status) : [...item.statuses, status] })}
                              >
                                {active && <Check className="size-3" />}
                                {t(($) => $.designer.status[status])}
                              </button>
                            );
                          })}
                          {item.statuses
                            .filter((status) => !ALL_STATUSES.includes(status as CanonicalStageStatus))
                            .map((status) => (
                              <button
                                key={status}
                                type="button"
                                className="flex h-7 items-center gap-1.5 rounded-md border border-brand/40 bg-brand/8 px-2 text-caption text-foreground"
                                title={t(($) => $.designer.remove_status)}
                                onClick={() => updateStage(item.id, {
                                  statuses: item.statuses.filter((value) => value !== status),
                                })}
                              >
                                <Check className="size-3" />
                                {status}
                              </button>
                            ))}
                          <Input className="h-7 w-36 text-caption" placeholder={t(($) => $.designer.custom_status)} onKeyDown={(event) => addCustomStatus(event, item)} />
                        </div>
                      </div>

                      </>}

                      <div className="grid gap-4 lg:grid-cols-2">
                        <ArtifactMarkdownEditor
                          editorKey={`${item.id}-input`}
                          icon={<FileInput className="size-3.5 text-muted-foreground" />}
                          label={t(($) => $.designer.default_input)}
                          helper={t(($) => $.designer.input_markdown_hint)}
                          placeholder={t(($) => $.designer.input_placeholder)}
                          value={item.input}
                          onChange={(input) => updateStage(item.id, { input })}
                        />
                        <ArtifactMarkdownEditor
                          editorKey={`${item.id}-output`}
                          icon={<FileOutput className="size-3.5 text-muted-foreground" />}
                          label={t(($) => $.designer.default_output)}
                          helper={t(($) => $.designer.output_markdown_hint)}
                          placeholder={t(($) => $.designer.output_placeholder)}
                          value={item.output}
                          onChange={(output) => updateStage(item.id, { output })}
                        />
                      </div>

                      <StageSkillSelector
                        skills={workspaceSkills}
                        selectedRefs={item.skills}
                        loading={skillsLoading}
                        onChange={(skills) => updateStage(item.id, { skills })}
                      />

                      <StageMCPSelector
                        servers={workspaceMCP?.servers ?? []}
                        selectedIds={item.allowedMcpServerIds}
                        onChange={(allowedMcpServerIds) => updateStage(item.id, { allowedMcpServerIds })}
                      />

                      <div
                        className={cn(
                          "rounded-xl border p-3",
                          item.gateEnabled
                            ? gateConfigured
                              ? "border-amber-500/35 bg-amber-500/5"
                              : "border-destructive/40 bg-destructive/5"
                            : "border-dashed border-border bg-muted/10",
                        )}
                      >
                        <div className="flex items-start justify-between gap-4">
                          <div className="min-w-0">
                            <div className="flex items-center gap-2">
                              <ShieldCheck className="size-4 text-muted-foreground" />
                              <span className="text-body font-semibold">
                                {t(($) => $.designer.gate)}
                              </span>
                            </div>
                            <p className="mt-1 text-caption leading-5 text-muted-foreground">
                              {t(($) => $.designer.gate_hint)}
                            </p>
                          </div>
                          <Switch
                            checked={item.gateEnabled}
                            aria-label={t(($) => $.designer.gate)}
                            onCheckedChange={(checked) =>
                              updateStage(item.id, { gateEnabled: checked })
                            }
                          />
                        </div>
                        {item.gateEnabled && (
                          <div className="mt-3 space-y-3 border-t border-border/70 pt-3">
                            <div
                              role="radiogroup"
                              aria-label={t(($) => $.designer.decision_mode)}
                              className="grid gap-2 md:grid-cols-3"
                            >
                              {(["human", "self_agent", "agent"] as DeciderType[]).map((type) => {
                                const active = item.deciderType === type;
                                return (
                                  <button
                                    key={type}
                                    type="button"
                                    role="radio"
                                    aria-checked={active}
                                    className={cn(
                                      "min-h-20 rounded-lg border p-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                                      active
                                        ? "border-brand/45 bg-brand/8"
                                        : "border-border/80 bg-background hover:border-brand/25 hover:bg-muted/30",
                                    )}
                                    onClick={() =>
                                      updateStage(item.id, {
                                        deciderType: type,
                                        decider:
                                          type === "human"
                                            ? defaultHumanId
                                            : type === "agent"
                                              ? defaultAgentId
                                              : "@self",
                                        requireHuman: type === "human",
                                        humanDecider:
                                          type === "human"
                                            ? defaultHumanId
                                            : item.humanDecider || defaultHumanId,
                                      })
                                    }
                                  >
                                    <span className="flex items-center gap-2">
                                      <span
                                        className={cn(
                                          "flex size-7 items-center justify-center rounded-md",
                                          active
                                            ? "bg-brand text-brand-foreground"
                                            : "bg-muted text-muted-foreground",
                                        )}
                                      >
                                        {type === "human" ? (
                                          <UserRound className="size-3.5" />
                                        ) : type === "self_agent" ? (
                                          <Bot className="size-3.5" />
                                        ) : (
                                          <UserCheck className="size-3.5" />
                                        )}
                                      </span>
                                      <span className="text-caption font-semibold">
                                        {t(($) => $.designer.decider[type])}
                                      </span>
                                    </span>
                                    <span className="mt-1.5 block text-micro leading-4 text-muted-foreground">
                                      {t(($) => $.designer.decider_hint[type])}
                                    </span>
                                  </button>
                                );
                              })}
                            </div>

                            {item.deciderType === "human" && (
                              <div>
                                <label className="mb-1.5 block text-caption font-medium">
                                  {t(($) => $.designer.human_decider)}
                                </label>
                                <GateActorPicker
                                  kind="human"
                                  value={item.decider}
                                  members={members}
                                  agents={agents}
                                  onChange={(decider) =>
                                    updateStage(item.id, {
                                      decider,
                                      humanDecider: decider,
                                    })
                                  }
                                />
                              </div>
                            )}

                            {item.deciderType === "agent" && (
                              <div>
                                <label className="mb-1.5 block text-caption font-medium">
                                  {t(($) => $.designer.agent_decider)}
                                </label>
                                <GateActorPicker
                                  kind="agent"
                                  value={item.decider}
                                  members={members}
                                  agents={agents}
                                  onChange={(decider) =>
                                    updateStage(item.id, { decider })
                                  }
                                />
                              </div>
                            )}

                            {item.deciderType === "self_agent" && (
                              <div className="flex items-start gap-2 rounded-lg border border-border/70 bg-background px-3 py-2.5">
                                <Bot className="mt-0.5 size-4 shrink-0 text-brand" />
                                <p className="text-caption leading-5 text-muted-foreground">
                                  {t(($) => $.designer.self_agent_hint)}
                                </p>
                              </div>
                            )}

                            {item.deciderType !== "human" && (
                              <div className="rounded-lg border border-border/80 bg-background p-3">
                                <div className="flex items-start justify-between gap-3">
                                  <div className="min-w-0">
                                    <p className="text-caption font-medium">
                                      {t(($) => $.designer.require_human)}
                                    </p>
                                    <p className="mt-1 text-micro leading-4 text-muted-foreground">
                                      {t(($) => $.designer.require_human_hint)}
                                    </p>
                                  </div>
                                  <Switch
                                    checked={item.requireHuman}
                                    aria-label={t(($) => $.designer.require_human)}
                                    onCheckedChange={(requireHuman) =>
                                      updateStage(item.id, {
                                        requireHuman,
                                        humanDecider:
                                          requireHuman && !item.humanDecider
                                            ? defaultHumanId
                                            : item.humanDecider,
                                      })
                                    }
                                  />
                                </div>
                                {item.requireHuman && (
                                  <div className="mt-3 border-t border-border/70 pt-3">
                                    <label className="mb-1.5 block text-caption font-medium">
                                      {t(($) => $.designer.final_human_decider)}
                                    </label>
                                    <GateActorPicker
                                      kind="human"
                                      value={item.humanDecider}
                                      members={members}
                                      agents={agents}
                                      onChange={(humanDecider) =>
                                        updateStage(item.id, { humanDecider })
                                      }
                                    />
                                  </div>
                                )}
                              </div>
                            )}

                            {!gateConfigured && (
                              <p role="alert" className="text-caption text-destructive">
                                {t(($) => $.designer.gate_actor_required)}
                              </p>
                            )}
                          </div>
                        )}
                      </div>
                    </div>
                  )}
                </section>
              );
            })}
          </div>
          <button type="button" className="mt-3 flex h-10 w-full items-center justify-center gap-2 rounded-xl border border-dashed border-border text-caption font-medium text-brand hover:border-brand/40 hover:bg-brand/5" onClick={addStage}>
            <Plus className="size-3.5" />
            {t(($) => $.create.add_stage)}
          </button>
        </div>
      </div>
    </div>
  );
}
