"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { AlertCircle, Loader2 } from "lucide-react";
import { api } from "@silieco/core/api";
import { useAuthStore } from "@silieco/core/auth";
import { useWelcomeStore } from "@silieco/core/onboarding";
import { paths, useCurrentWorkspace } from "@silieco/core/paths";
import { resolvePublicFileUrl } from "@silieco/core/workspace/avatar-url";
import { taskKeys } from "@silieco/core/tasks/queries";
import { workspaceKeys } from "@silieco/core/workspace/queries";
import type { Agent, CreateTaskRequest, Task } from "@silieco/core/types";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@silieco/ui/components/ui/dialog";
import { Button } from "@silieco/ui/components/ui/button";
import { cn } from "@silieco/ui/lib/utils";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import {
  buildUserContextSection,
  CREATE_AGENT_GUIDE_TASK_TITLE,
  FOLLOWUP_COMMENT_PREFIX,
  getCreateAgentGuideBody,
  HELPER_DESCRIPTION,
  HELPER_INSTRUCTIONS,
  HELPER_STARTER_PROMPTS,
  INSTALL_RUNTIME_TASK_BODY,
  INSTALL_RUNTIME_TASK_TITLE,
  pickContentLang,
  STARTER_CARD_IDS,
  type StarterCardId,
  type UserContextLabels,
} from "../onboarding/templates";

/**
 * Workspace welcome experience for users who just finished onboarding.
 *
 * Mounted in the workspace shell on both web and desktop. Subscribes to
 * the Zustand welcome-store signal that `OnboardingFlow.handleRuntimeNext`
 * parks before navigating. Renders null when there is no signal or when
 * the signal has been dismissed.
 *
 * Two sub-templates driven by `signal.choice`:
 *
 *   "runtime":
 *     1. Full-screen loading veil ("Preparing Sili Agent…")
 *     2. Find-or-create the "Sili Agent" on the picked runtime
 *        — `listAgents` first to dedupe against re-entries, then
 *        `createAgent` with the localized instructions from
 *        `onboarding/templates/helper-instructions.ts`.
 *     3. Blocking Dialog (no close button, no Escape, no outside-click).
 *        Renders the agent's name + description and 3 starter cards.
 *     4. User picks a card → `createTask` with the card's prompt as
 *        body, assigned to Sili Agent → `dismiss()` → navigate.
 *     5. Failure surfaces a Retry UI; the modal stays put.
 *
 *   "skip":
 *     1. Dialog opens immediately (closable).
 *     2. Background: `createTask × 2` with the install-runtime and
 *        create-agent-guide bodies, assigned to the user themselves.
 *     3. Cards render with their static titles + subtitles; per-card
 *        spinner switches to "Open task" once the task id arrives.
 *     4. Click a card → `dismiss()` → navigate. Close the modal →
 *        `dismiss()` → stay on tasks list; seeded tasks remain.
 *     5. Per-card failure surfaces a Retry button on that card only.
 *
 * Why subscribe-not-consume: React 18+ StrictMode dev mounts components
 * twice. A consume-on-init pattern would empty the store on the first
 * mount, then render null on the second. Subscribing + recording
 * dismissal in the store sidesteps both mounts seeing the same state.
 * Async work (Sili Agent setup, skip-path seed tasks) is deduped at
 * module level via `findOrCreateSiliAgent` / `seedTaskDeduped` so the
 * double-mount can't race two API calls for the same workspace.
 */
export function WelcomeAfterOnboarding() {
  const me = useAuthStore((s) => s.user);
  const currentWorkspace = useCurrentWorkspace();
  const signal = useWelcomeStore((s) => s.signal);
  const dismissed = useWelcomeStore((s) => s.dismissed);
  const dismiss = useWelcomeStore((s) => s.dismiss);

  // Cross-workspace safety: signal lives in a global store, but this
  // component mounts inside a workspace-scoped layout. If the user is
  // currently viewing ws-B while signal points at ws-A (back/forward,
  // deep-link, desktop multi-tab where stores are shared across tabs in
  // one renderer), DON'T fire here — would create Sili Agent /
  // seed tasks in ws-A while the user looks at ws-B, then navigate them
  // away. Render null until the user lands back in the workspace the
  // signal was parked for.
  if (
    !me ||
    !signal ||
    dismissed ||
    !currentWorkspace ||
    currentWorkspace.id !== signal.workspaceId
  ) {
    return null;
  }

  if (signal.choice === "runtime" && signal.runtimeId) {
    return (
      <RuntimeWelcome
        workspaceId={signal.workspaceId}
        runtimeId={signal.runtimeId}
        onAbandon={dismiss}
        onComplete={dismiss}
      />
    );
  }
  if (signal.choice === "skip") {
    return (
      <SkipWelcome
        workspaceId={signal.workspaceId}
        onDismiss={dismiss}
      />
    );
  }
  // Malformed signal (e.g. choice === "runtime" with no runtimeId) is
  // a programming error in the producer side; render nothing to avoid
  // putting the user in a stuck modal.
  return null;
}

// ---------------------------------------------------------------------------
// Runtime sub-template
// ---------------------------------------------------------------------------

const SILI_AGENT_NAME = "Sili Agent";
const LEGACY_HELPER_AGENT_NAME = "Silieco Helper";

// Self-contained copy of the canonical Silieco mark. A data URL keeps the
// avatar available in web, desktop, and self-hosted deployments without
// depending on an external CDN or a deployment-specific public origin.
const SILI_AGENT_AVATAR_URL =
  "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 64 64'%3E%3Crect width='64' height='64' rx='14' fill='%230b0d12'/%3E%3Cpath d='M18 18h12v12H18zM34 18h12v12H34zM18 34h12v12H18zM34 34h12v12H34z' fill='%23f3f6fb'/%3E%3Cpath d='M34 18h12v12H34zM18 34h12v12H18z' fill='%233978f6'/%3E%3C/svg%3E";

/**
 * Module-level dedupe for in-flight Sili Agent setup. Keyed on
 * workspaceId+runtimeId so unrelated welcome flows (different workspaces /
 * runtimes) don't collide, but React 18+ StrictMode dev-mode double-mount
 * of the same flow shares one promise — both mounts await the same
 * listAgents/createAgent round-trip, so we never race-create two Sili Agents
 * (server-side UNIQUE (workspace_id, name) would 409 the second attempt
 * anyway, but the rejected promise would surface a misleading "agent
 * already exists" error UI in dev).
 *
 * `useRef` cannot solve this because each StrictMode mount gets a fresh
 * React component instance — and therefore a fresh ref.
 */
const pendingSiliAgentSetup = new Map<string, Promise<Agent>>();

async function findOrCreateSiliAgent(
  workspaceId: string,
  runtimeId: string,
  language: string | null,
): Promise<Agent> {
  const key = `${workspaceId}:${runtimeId}`;
  const existing = pendingSiliAgentSetup.get(key);
  if (existing) return existing;

  const promise = (async (): Promise<Agent> => {
    const agents = await api.listAgents({ workspace_id: workspaceId });
    const found =
      agents.find(
        (a) =>
          a.name === SILI_AGENT_NAME &&
          a.visibility === "workspace" &&
          !a.archived_at,
      ) ??
      agents.find(
        (a) =>
          a.name === LEGACY_HELPER_AGENT_NAME &&
          a.visibility === "workspace" &&
          !a.archived_at,
      );
    const lang = pickContentLang(language);
    if (found) {
      if (
        found.name === SILI_AGENT_NAME &&
        found.avatar_url === SILI_AGENT_AVATAR_URL
      ) {
        return found;
      }
      return api.updateAgent(found.id, {
        name: SILI_AGENT_NAME,
        avatar_url: SILI_AGENT_AVATAR_URL,
      });
    }
    return api.createAgent({
      name: SILI_AGENT_NAME,
      description: HELPER_DESCRIPTION[lang],
      instructions: HELPER_INSTRUCTIONS[lang],
      avatar_url: SILI_AGENT_AVATAR_URL,
      runtime_id: runtimeId,
      visibility: "workspace",
      max_concurrent_runs: 6,
      template: "silieco_helper",
    });
  })();

  pendingSiliAgentSetup.set(key, promise);
  // Clear on settle so a manual Retry after a failure runs a fresh probe
  // rather than re-awaiting the rejected promise. .catch on the cleanup
  // chain silences unhandled-rejection noise — the original `promise` we
  // return still propagates rejections to the caller.
  promise
    .finally(() => {
      if (pendingSiliAgentSetup.get(key) === promise) {
        pendingSiliAgentSetup.delete(key);
      }
    })
    .catch(() => {});
  return promise;
}

/**
 * Module-level dedupe for skip-path seed tasks. Same StrictMode rationale
 * as `findOrCreateSiliAgent`: each mount has its own useRef, so without a
 * cross-mount cache the dev double-mount would race two CreateTask calls
 * for the same title — the server-side LockAndFindActiveDuplicate gate
 * returns 409 on the second one, but a 409 in the catch block populates
 * the per-card error state with a misleading message ("an task with this
 * title already exists") even though the task WAS created.
 *
 * Keyed on `${workspaceId}:${title}` because the seed identity is the
 * task title (server's duplicate dedupe also keys on title).
 */
const pendingTaskSeed = new Map<string, Promise<Task>>();

function seedTaskDeduped(
  cacheKey: string,
  body: CreateTaskRequest,
): Promise<Task> {
  const existing = pendingTaskSeed.get(cacheKey);
  if (existing) return existing;
  const promise = api.createTask(body);
  pendingTaskSeed.set(cacheKey, promise);
  // .finally() returns a NEW promise that mirrors the rejection of the
  // original. Silence it with .catch so the cleanup chain never surfaces
  // as an unhandled rejection — the original `promise` we return still
  // carries the rejection to the caller's await.
  promise
    .finally(() => {
      if (pendingTaskSeed.get(cacheKey) === promise) {
        pendingTaskSeed.delete(cacheKey);
      }
    })
    .catch(() => {});
  return promise;
}

/**
 * Same module-level dedup pattern for the follow-up comment on the
 * skip-path install-runtime task (linking to the create-agent-guide
 * task). Keeps the two StrictMode mounts from posting the comment twice.
 */
const pendingCommentSeed = new Map<string, Promise<unknown>>();

function postCommentDeduped(
  cacheKey: string,
  taskId: string,
  content: string,
): Promise<unknown> {
  const existing = pendingCommentSeed.get(cacheKey);
  if (existing) return existing;
  const promise = api.createComment(taskId, content);
  pendingCommentSeed.set(cacheKey, promise);
  promise
    .finally(() => {
      if (pendingCommentSeed.get(cacheKey) === promise) {
        pendingCommentSeed.delete(cacheKey);
      }
    })
    .catch(() => {});
  return promise;
}

interface RuntimeWelcomeProps {
  workspaceId: string;
  runtimeId: string;
  /** User explicitly gave up after a fatal failure — clear the welcome
   *  signal so we don't re-show the error veil. */
  onAbandon: () => void;
  /** Happy-path completion: user picked a starter card, task is created,
   *  we're about to navigate. Clear the welcome signal so navigating back
   *  to /tasks doesn't re-trigger the modal. */
  onComplete: () => void;
}

function RuntimeWelcome({
  workspaceId,
  runtimeId,
  onAbandon,
  onComplete,
}: RuntimeWelcomeProps) {
  // i18n.language is the LIVE runtime locale, kept in sync with both the
  // browser fallback and the user's saved `me.language` preference (via
  // user-locale-sync). Reading me.language directly would miss anonymous
  // new users whose preference field is still null but whose browser is
  // already using another supported locale — the agent instructions and
  // seeded task body should follow what they're already reading.
  const { t, i18n } = useT("onboarding");
  const navigation = useNavigation();
  const qc = useQueryClient();
  // The parent `WelcomeAfterOnboarding` already gated on `me` being
  // non-null, but we re-read the store here instead of threading it
  // through props so the questionnaire stays in sync if the user
  // refreshes their profile mid-flow (PATCH /api/me returns the updated
  // row and the store is reactive). Used to enrich starter task
  // descriptions with a `> About me` block.
  const me = useAuthStore((s) => s.user);

  // Phase machine: "preparing" (loading veil) → "ready" (modal with agent
  // + selectable cards) → submitting (cards locked, bottom CTA spins) →
  // success path navigates away; failure path surfaces an error string.
  const [agent, setAgent] = useState<Agent | null>(null);
  const [prepError, setPrepError] = useState<Error | null>(null);
  const [attemptKey, setAttemptKey] = useState(0);

  // Multi-select of starter cards. Empty Set = nothing selected → CTA
  // disabled. User toggles by clicking; no checkmark icon — we lean on
  // the border + ring pattern already used by `compact-runtime-row.tsx`
  // for visual selection state.
  const [selected, setSelected] = useState<Set<StarterCardId>>(
    () => new Set(),
  );
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const submitInFlightRef = useRef(false);
  // After Promise.all(createTask × N) resolves we DON'T navigate
  // immediately. We park the first task id here and switch the Modal to
  // a success state ("you're all set, agent is on it, here's how to
  // check in via Inbox / chat") — gives the user a moment to register
  // what just happened + surfaces two features they're likely to miss.
  // Got it on the success view is what finally dismisses + navigates.
  const [successTaskId, setSuccessTaskId] = useState<string | null>(null);

  // Resolve the role / use_case enum slugs to human-readable labels in
  // the user's current locale, then build the markdown block that gets
  // appended to every starter task description. Memoized on t +
  // i18n.language so a language switch refreshes everything in one
  // re-render; bundle is rebuilt whenever the questionnaire row changes.
  const userContextLabels: UserContextLabels = useMemo(() => {
    const lang = pickContentLang(i18n.language);
    return {
      heading: t(($) => $.welcome_after_onboarding.user_context_heading),
      roleLabel: t(($) => $.welcome_after_onboarding.user_context_role_label),
      useCaseLabel: t(
        ($) => $.welcome_after_onboarding.user_context_use_case_label,
      ),
      listSeparator: lang === "zh" || lang === "ja" ? "、" : ", ",
      role: {
        engineer: t(($) => $.questions.role.engineer),
        product: t(($) => $.questions.role.product),
        designer: t(($) => $.questions.role.designer),
        founder: t(($) => $.questions.role.founder),
        marketing: t(($) => $.questions.role.marketing),
        writer: t(($) => $.questions.role.writer),
        research: t(($) => $.questions.role.research),
        ops: t(($) => $.questions.role.ops),
        student: t(($) => $.questions.role.student),
        other: t(($) => $.questions.role.other),
      },
      useCase: {
        ship_code: t(($) => $.questions.use_case.ship_code),
        manage_team: t(($) => $.questions.use_case.manage_team),
        personal_tasks: t(($) => $.questions.use_case.personal_tasks),
        plan_research: t(($) => $.questions.use_case.plan_research),
        write_publish: t(($) => $.questions.use_case.write_publish),
        automate_ops: t(($) => $.questions.use_case.automate_ops),
        evaluate: t(($) => $.questions.use_case.evaluate),
        other: t(($) => $.questions.use_case.other),
      },
    };
  }, [t, i18n.language]);
  const toggle = (id: StarterCardId) => {
    if (submitting) return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // Find or create Sili Agent. Dedupe lives in `findOrCreateSiliAgent`
  // (module-level promise cache) so StrictMode double-mounts share one
  // in-flight request. `attemptKey` increments on manual Retry to force
  // the effect to re-run.
  useEffect(() => {
    if (agent) return;
    let cancelled = false;
    (async () => {
      try {
        const a = await findOrCreateSiliAgent(
          workspaceId,
          runtimeId,
          i18n.language,
        );
        if (!cancelled) {
          setAgent(a);
          qc.invalidateQueries({
            queryKey: workspaceKeys.agents(workspaceId),
          });
        }
      } catch (err) {
        if (!cancelled) {
          setPrepError(err instanceof Error ? err : new Error(String(err)));
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [agent, attemptKey, i18n.language, qc, runtimeId, workspaceId]);

  // Phase 1: error surface for the prepare step.
  if (prepError) {
    return (
      <FullScreenError
        title={t(($) => $.welcome_after_onboarding.error_title)}
        message={
          prepError.message ||
          t(($) => $.welcome_after_onboarding.error_generic)
        }
        retryLabel={t(($) => $.welcome_after_onboarding.retry)}
        closeLabel={t(($) => $.welcome_after_onboarding.error_close)}
        onRetry={() => {
          setPrepError(null);
          setAttemptKey((n) => n + 1);
        }}
        onClose={onAbandon}
      />
    );
  }

  // Phase 1: loading veil while we set up the agent.
  if (!agent) {
    return (
      <FullScreenLoading
        label={t(($) => $.welcome_after_onboarding.loading_helper)}
      />
    );
  }

  // Phase 2: blocking modal with starter cards.
  const lang = pickContentLang(i18n.language);

  const handleAssign = async () => {
    if (submitInFlightRef.current || selected.size === 0) return;
    submitInFlightRef.current = true;
    setSubmitError(null);
    setSubmitting(true);
    try {
      // Build the per-task "about me" block once and append it to every
      // starter task. The block is empty markdown ("") when the user
      // skipped both questions, so unconditional concat is safe — Sili Agent
      // sees the original prompt verbatim with no trailing context.
      const userContext = buildUserContextSection(
        me?.onboarding_questionnaire,
        userContextLabels,
      );
      // Create tasks in declared order (STARTER_CARD_IDS), so the user
      // lands on the most foundational task first when there's a tie. We
      // run them in parallel — Sili Agent's max_concurrent_runs lets it pick
      // them up immediately, and one failure of N shouldn't gate the
      // others.
      const orderedIds = STARTER_CARD_IDS.filter((id) => selected.has(id));
      const tasks = await Promise.all(
        orderedIds.map((id) => {
          const card = HELPER_STARTER_PROMPTS[id];
          return api.createTask({
            title: card.title[lang],
            description: card.prompt[lang] + userContext,
            status: "todo",
            priority: "high",
            assignee_type: "agent",
            assignee_id: agent.id,
          });
        }),
      );
      await Promise.all([
        qc.invalidateQueries({ queryKey: taskKeys.all(workspaceId) }),
        qc.invalidateQueries({
          queryKey: workspaceKeys.agents(workspaceId),
        }),
      ]);
      // Tasks created — switch the Modal to its success state. We
      // navigate only when the user explicitly clicks Got it (handled in
      // handleGotIt below) so they see the inbox / chat hint.
      submitInFlightRef.current = false;
      setSubmitting(false);
      setSuccessTaskId(tasks[0]!.id);
    } catch (err) {
      submitInFlightRef.current = false;
      setSubmitting(false);
      setSubmitError(
        err instanceof Error
          ? err.message
          : t(($) => $.welcome_after_onboarding.error_generic),
      );
    }
  };

  const handleGotIt = async () => {
    if (!successTaskId) return;
    onComplete();
    const slug = await resolveWorkspaceSlug(qc, workspaceId);
    navigation.push(paths.workspace(slug).taskDetail(successTaskId));
  };

  if (successTaskId) {
    return (
      <Dialog
        open={true}
        modal={true}
        disablePointerDismissal={true}
        onOpenChange={() => {
          /* blocking until Got it */
        }}
      >
        <DialogContent
          showCloseButton={false}
          className="max-w-md sm:max-w-md"
          aria-describedby="welcome-after-onboarding-runtime-success-subtitle"
        >
          <div className="flex flex-col items-center gap-3 pt-4">
            <div
              className="text-4xl animate-welcome-emoji-pop"
              aria-hidden
            >
              🎉
            </div>
            <DialogTitle className="text-center text-display-sm font-semibold">
              {t(($) => $.welcome_after_onboarding.runtime.success.title)}
            </DialogTitle>
            <DialogDescription
              id="welcome-after-onboarding-runtime-success-subtitle"
              className="text-center text-body text-muted-foreground"
            >
              {t(
                ($) => $.welcome_after_onboarding.runtime.success.subtitle,
                { agentName: agent.name },
              )}
            </DialogDescription>
            <div className="mt-1 flex flex-col gap-1.5 max-w-sm">
              <p className="text-center text-caption text-muted-foreground/80 leading-relaxed">
                {t(
                  ($) => $.welcome_after_onboarding.runtime.success.tip_inbox,
                )}
              </p>
              <p className="text-center text-caption text-muted-foreground/80 leading-relaxed">
                {t(
                  ($) => $.welcome_after_onboarding.runtime.success.tip_chat,
                )}
              </p>
            </div>
          </div>
          <div className="mt-4 flex justify-end">
            <Button size="lg" onClick={handleGotIt}>
              {t(($) => $.welcome_after_onboarding.runtime.success.got_it)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  return (
    <Dialog
      open={true}
      modal={true}
      disablePointerDismissal={true}
      onOpenChange={() => {
        /* runtime path is blocking by design */
      }}
    >
      <DialogContent
        showCloseButton={false}
        className="max-w-xl sm:max-w-xl"
        aria-describedby="welcome-after-onboarding-runtime-subtitle"
      >
        <div className="flex flex-col items-center gap-3 pt-4 animate-onboarding-enter">
          <img
            src={resolvePublicFileUrl(agent.avatar_url) ?? SILI_AGENT_AVATAR_URL}
            alt=""
            aria-hidden
            className="h-14 w-14 rounded-xl ring-1 ring-foreground/10"
          />
          <DialogTitle className="text-center text-title-lg font-semibold">
            {t(($) => $.welcome_after_onboarding.runtime.greeting)}
          </DialogTitle>
          <DialogDescription
            id="welcome-after-onboarding-runtime-subtitle"
            className="text-center text-body text-muted-foreground"
          >
            {t(($) => $.welcome_after_onboarding.runtime.subtitle)}
          </DialogDescription>
          <p className="text-center text-body text-muted-foreground max-w-md leading-relaxed">
            {t(($) => $.welcome_after_onboarding.runtime.capabilities)}
          </p>
        </div>

        <div className="mt-4 border-t pt-4">
          <p className="mb-3 text-body font-medium text-foreground">
            {t(($) => $.welcome_after_onboarding.runtime.section_label)}
          </p>
          <div className="flex flex-col gap-2">
            {STARTER_CARD_IDS.map((id) => {
              const isSelected = selected.has(id);
              return (
                <button
                  key={id}
                  type="button"
                  onClick={() => toggle(id)}
                  disabled={submitting}
                  aria-pressed={isSelected}
                  className={cn(
                    "flex items-start gap-3 rounded-lg border bg-card px-3 py-2.5 text-left transition-colors",
                    isSelected
                      ? "border-primary ring-1 ring-primary"
                      : "hover:border-foreground/20",
                    "disabled:cursor-not-allowed disabled:opacity-60",
                  )}
                >
                  <div className="flex-1 min-w-0">
                    <p className="text-body font-medium leading-tight">
                      {HELPER_STARTER_PROMPTS[id].title[lang]}
                    </p>
                    <p className="mt-0.5 text-caption text-muted-foreground leading-snug">
                      {t(
                        ($) =>
                          $.welcome_after_onboarding.runtime.cards[id]
                            .subtitle,
                      )}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>

          <div className="mt-4 flex justify-end">
            <Button
              size="lg"
              disabled={selected.size === 0 || submitting}
              onClick={handleAssign}
            >
              {submitting && (
                <Loader2 className="h-4 w-4 animate-spin" />
              )}
              {selected.size === 0
                ? t(($) => $.welcome_after_onboarding.runtime.assign_empty)
                : t(($) => $.welcome_after_onboarding.runtime.assign_count, {
                    count: selected.size,
                  })}
            </Button>
          </div>
        </div>

        {submitError ? (
          <div
            role="alert"
            className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-caption text-destructive"
          >
            <p>{submitError}</p>
            <Button
              variant="ghost"
              size="sm"
              className="mt-1 h-6 px-2 text-caption"
              onClick={() => setSubmitError(null)}
            >
              {t(($) => $.welcome_after_onboarding.dismiss_error)}
            </Button>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Skip sub-template
// ---------------------------------------------------------------------------

interface SkipBundle {
  installTaskId: string;
  agentGuideId: string;
}

interface SkipWelcomeProps {
  workspaceId: string;
  onDismiss: () => void;
}

/**
 * Skip-path welcome. v3 product decision (see /Users/qingnaiyuan/.claude/plans):
 *
 *   1. Full-screen loading veil while we provision EVERYTHING in a fixed
 *      sequence — install-runtime task → create-agent-guide task →
 *      follow-up comment on the install-runtime task linking to the
 *      create-agent-guide identifier.
 *   2. Only after the whole chain succeeds do we mount the celebration
 *      Modal. Cards inside the Modal are pure display — no interactive
 *      per-card state, no spinners, no error boundaries.
 *   3. If ANY step fails we silently dismiss; the user lands on the
 *      tasks list and finds whatever subset of tasks did get created.
 *      Onboarded_at is already set by Step 3, so this isn't blocking.
 *   4. The Modal has a single primary action [Got it] which dismisses and
 *      navigates to the install-runtime task. Closing via X or outside
 *      click dismisses but doesn't navigate.
 *
 * Why provision-then-show instead of show-with-loading: the previous
 * design had cards with their own spinners and retry buttons, which made
 * the Modal feel like a busy task list rather than a celebration of
 * finishing onboarding. The product brief asked for emotional value.
 */
function SkipWelcome({ workspaceId, onDismiss }: SkipWelcomeProps) {
  // i18n.language is the LIVE runtime locale (browser fallback + saved
  // me.language). Reading me.language directly would miss new users
  // whose preference field is still null but who are browsing in another
  // supported locale.
  const { t, i18n } = useT("onboarding");
  const navigation = useNavigation();
  const qc = useQueryClient();
  const me = useAuthStore((s) => s.user);

  const [bundle, setBundle] = useState<SkipBundle | null>(null);
  const [failed, setFailed] = useState(false);

  // Provision sequence — runs once on mount. StrictMode double-mount is
  // safe because each step uses module-level dedup (seedTaskDeduped /
  // postCommentDeduped) keyed on `${workspaceId}:<purpose>`.
  useEffect(() => {
    if (!me) return;
    if (bundle || failed) return;
    let cancelled = false;
    (async () => {
      try {
        const lang = pickContentLang(i18n.language);
        // Order matters — each step's input depends on the previous step's
        // output (task identifier / uuid for cross-reference mention
        // chips). Module-level dedupes (seedTaskDeduped /
        // postCommentDeduped) make StrictMode double-mounts share the same
        // in-flight promise per step.
        //
        // 1. install-runtime first (body is a static const, no deps).
        //    Step 1 of the bundle, in_progress so the user sees it's the
        //    active next step.
        const installRuntime = await seedTaskDeduped(
          `${workspaceId}:install-runtime`,
          {
            title: INSTALL_RUNTIME_TASK_TITLE[lang],
            description: INSTALL_RUNTIME_TASK_BODY[lang],
            status: "in_progress",
            priority: "high",
            assignee_type: "member",
            assignee_id: me.id,
          },
        );
        // 2. agent-guide. Body is composed at call-time so it can embed
        //    a mention chip pointing back at install-runtime — the user
        //    reads agent-guide and can jump to its prerequisite with one
        //    click.
        const agentGuide = await seedTaskDeduped(
          `${workspaceId}:create-agent-guide`,
          {
            title: CREATE_AGENT_GUIDE_TASK_TITLE[lang],
            description: getCreateAgentGuideBody({
              lang,
              installRuntimeIdentifier: installRuntime.identifier,
              installRuntimeId: installRuntime.id,
            }),
            status: "todo",
            priority: "medium",
            assignee_type: "member",
            assignee_id: me.id,
          },
        );
        // 3. follow-up comment on install-runtime pointing at agent-guide
        //    via the same mention-chip protocol (renders as the styled
        //    TaskChip pill). Prefix is a TS const — anything that gets
        //    persisted to DB must not depend on the i18n bundle (stale
        //    bundle would write raw key text into comment.content
        //    permanently).
        const prefix = FOLLOWUP_COMMENT_PREFIX[lang];
        const commentText = `${prefix} [${agentGuide.identifier}](mention://task/${agentGuide.id})`;
        await postCommentDeduped(
          `${workspaceId}:install-runtime-followup`,
          installRuntime.id,
          commentText,
        );
        qc.invalidateQueries({ queryKey: taskKeys.all(workspaceId) });
        if (!cancelled) {
          setBundle({
            installTaskId: installRuntime.id,
            agentGuideId: agentGuide.id,
          });
        }
      } catch {
        if (!cancelled) setFailed(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [bundle, failed, i18n.language, me, qc, t, workspaceId]);

  // Failure path: dismiss the welcome signal so the next render returns
  // null. We don't surface an error UI — the user is already onboarded;
  // anything that did get created is visible in the tasks list.
  useEffect(() => {
    if (failed) onDismiss();
  }, [failed, onDismiss]);

  if (failed || !me) return null;

  if (!bundle) {
    return (
      <FullScreenLoading
        label={t(($) => $.welcome_after_onboarding.skip.loading)}
      />
    );
  }

  const handleGotIt = async () => {
    // Dismiss BEFORE navigating so the destination doesn't re-render
    // the Modal from the (still-set) welcome-store signal.
    onDismiss();
    const slug = await resolveWorkspaceSlug(qc, workspaceId);
    navigation.push(paths.workspace(slug).taskDetail(bundle.installTaskId));
  };

  return (
    <Dialog
      open={true}
      modal={true}
      onOpenChange={(open) => {
        if (!open) onDismiss();
      }}
    >
      <DialogContent
        className="max-w-xl sm:max-w-xl"
        aria-describedby="welcome-after-onboarding-skip-subtitle"
      >
        <div className="flex flex-col items-center gap-4 pt-6">
          <div className="text-6xl animate-welcome-emoji-pop" aria-hidden>
            🎉
          </div>
          <DialogTitle className="text-center text-display-sm font-semibold">
            {t(($) => $.welcome_after_onboarding.skip.title)}
          </DialogTitle>
          <DialogDescription
            id="welcome-after-onboarding-skip-subtitle"
            className="text-center text-body text-muted-foreground max-w-md"
          >
            {t(($) => $.welcome_after_onboarding.skip.subtitle)}
          </DialogDescription>
        </div>

        <div className="mt-6 flex flex-col gap-2">
          <SkipPreviewCard
            cardKey="install_runtime"
            statusLabel={t(
              ($) => $.welcome_after_onboarding.skip.status_in_progress,
            )}
            statusTone="active"
          />
          <SkipPreviewCard
            cardKey="create_agent"
            statusLabel={t(($) => $.welcome_after_onboarding.skip.status_todo)}
            statusTone="todo"
          />
        </div>

        <div className="mt-6 flex justify-end">
          <Button size="lg" onClick={handleGotIt}>
            {t(($) => $.welcome_after_onboarding.skip.got_it)}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Pure display card for the celebration Modal. No interaction — the
 * single CTA at the bottom of the Modal is the only way to advance.
 */
function SkipPreviewCard({
  cardKey,
  statusLabel,
  statusTone,
}: {
  cardKey: "install_runtime" | "create_agent";
  statusLabel: string;
  statusTone: "active" | "todo";
}) {
  const { t } = useT("onboarding");
  return (
    <div className="flex items-start gap-3 rounded-lg border bg-background px-3 py-2.5">
      <div className="flex-1 min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-body font-medium leading-tight">
            {t(($) => $.welcome_after_onboarding.skip.cards[cardKey].title)}
          </p>
          <span
            className={cn(
              "rounded-full px-2 py-0.5 text-micro font-medium",
              statusTone === "active"
                ? "bg-primary/10 text-primary"
                : "bg-muted text-muted-foreground",
            )}
          >
            {statusLabel}
          </span>
        </div>
        <p className="mt-1 text-caption text-muted-foreground leading-snug">
          {t(($) => $.welcome_after_onboarding.skip.cards[cardKey].subtitle)}
        </p>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Shared
// ---------------------------------------------------------------------------

function FullScreenLoading({ label }: { label: string }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
      <div className="flex flex-col items-center gap-3">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
        <p className="text-body text-muted-foreground">{label}</p>
      </div>
    </div>
  );
}

function FullScreenError({
  title,
  message,
  retryLabel,
  closeLabel,
  onRetry,
  onClose,
}: {
  title: string;
  message: string;
  retryLabel: string;
  closeLabel: string;
  onRetry: () => void;
  onClose: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/80 backdrop-blur-sm">
      <div className="flex max-w-md flex-col items-center gap-4 rounded-lg border bg-card p-6 shadow-md">
        <AlertCircle className="h-6 w-6 text-destructive" />
        <p className="text-center text-body font-medium text-foreground">
          {title}
        </p>
        <p className="text-center text-caption text-muted-foreground">{message}</p>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={onClose}>
            {closeLabel}
          </Button>
          <Button size="sm" onClick={onRetry}>
            {retryLabel}
          </Button>
        </div>
      </div>
    </div>
  );
}

// Resolve a workspace slug from the cache (the workspace list query is
// always pre-warmed by the layout). Falls back to a network fetch when
// the cache is cold.
async function resolveWorkspaceSlug(
  qc: ReturnType<typeof useQueryClient>,
  workspaceId: string,
): Promise<string> {
  const cached = qc
    .getQueriesData<{ id: string; slug: string }[] | undefined>({
      queryKey: workspaceKeys.list(),
    })
    .map(([, data]) => data)
    .find(Boolean);
  const hit = cached?.find((w) => w.id === workspaceId);
  if (hit) return hit.slug;
  const ws = await api.getWorkspace(workspaceId);
  return ws.slug;
}
