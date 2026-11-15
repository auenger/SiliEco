"use client";

import { useCallback, useState, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@silieco/ui/components/ui/button";
import { toast } from "sonner";
import { setCurrentWorkspace } from "@silieco/core/platform";
import { useAuthStore } from "@silieco/core/auth";
import {
  completeOnboarding,
  ONBOARDING_STEP_ORDER,
  useWelcomeStore,
  type OnboardingStep,
} from "@silieco/core/onboarding";
import { api } from "@silieco/core/api";
import { workspaceListOptions } from "@silieco/core/workspace/queries";
import type { AgentRuntime, Workspace } from "@silieco/core/types";
import { StepWelcome } from "./steps/step-welcome";
import { StepWorkspace } from "./steps/step-workspace";
import {
  StepRuntimeDetection,
  type RuntimeDetectionResult,
} from "./steps/step-runtime-detection";
import { StepWorkspaceDescription } from "./steps/step-workspace-description";
import { useT } from "../i18n";
import { useLogout } from "../auth";

/**
 * Shell's onComplete contract:
 *   onComplete(workspace?, taskId?) — if an task id is present, navigate
 *   straight into that onboarding task; otherwise navigate into the
 *   workspace tasks list.
 *
 * Three exit shapes feed onComplete:
 *   - Skip-existing (Welcome): completeOnboarding marks onboarded; navigate
 *     to the existing workspace's task list.
 *   - Runtime-skipped (no runtime on Step 3): completeOnboarding marks
 *     onboarded; we push a {choice:"skip"} welcome signal and navigate
 *     to the workspace. The welcome hook in the workspace shell creates
 *     the install-runtime / create-agent guide tasks on landing.
 *   - Runtime-connected (runtime picked on Step 3): completeOnboarding
 *     marks onboarded; we push a {choice:"runtime", runtimeId} welcome
 *     signal and navigate. The welcome hook creates Sili Agent
 *     agent on the picked runtime and shows the starter-card Modal.
 *
 * V3 contract: this file never touches createAgent / createTask. The
 * "what runs in the workspace shell after onboarding" decision is in
 * `packages/views/workspace/welcome-after-onboarding.tsx`.
 */
export function OnboardingFlow({
  onComplete,
  runtimeInstructions,
  onRuntimeProbe,
  onRuntimeRefresh,
}: {
  onComplete: (workspace?: Workspace, taskId?: string) => void;
  runtimeInstructions?: React.ReactNode;
  /** Desktop probes installed local Agent tools before a workspace exists. */
  onRuntimeProbe?: () => Promise<RuntimeDetectionResult>;
  /** Desktop wires this to restart the bundled daemon so a freshly
   *  installed agent CLI gets picked up on the runtime step. Web omits
   *  it — its CLI install flow already runs on the user's machine and
   *  the embedded picker reacts to daemon:register events. */
  onRuntimeRefresh?: () => void | Promise<void>;
}) {
  const { t } = useT("onboarding");
  const logout = useLogout();
  const { t: tw } = useT("workspace");
  const user = useAuthStore((s) => s.user);
  if (!user) {
    throw new Error("OnboardingFlow requires an authenticated user");
  }

  const [step, setStep] = useState<OnboardingStep>("welcome");
  const [workspace, setWorkspace] = useState<Workspace | null>(null);

  // Keep existing membership available throughout setup and when skipping.
  const { data: workspaces = [], isFetched: workspacesFetched } = useQuery({
    ...workspaceListOptions(),
    enabled: true,
  });
  const existingWorkspace = workspace ?? workspaces[0] ?? null;
  const canSkipWelcome = workspacesFetched && workspaces.length > 0;
  const [skipping, setSkipping] = useState(false);
  const skipInFlight = useRef(false);
  const handleSkipAll = async () => {
    if (skipInFlight.current) return;
    skipInFlight.current = true;
    setSkipping(true);
    try {
      await completeOnboarding();
      onComplete(existingWorkspace ?? undefined);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.errors.skip_failed));
    } finally {
      skipInFlight.current = false;
      setSkipping(false);
    }
  };

  // The `runtimeInstructions` slot is only plumbed by the web shell
  // (desktop bundles a daemon, so a CLI install card would be noise
  // there). We reuse its presence as the web signal rather than
  // introducing a redundant prop.
  const isWeb = !!runtimeInstructions;

  // Derive "what comes after `from`" from ONBOARDING_STEP_ORDER so
  // inserting/reordering a persisted step only requires editing the
  // canonical array. Returns null if `from` is the last persisted step
  // or not in the array (callers fall back to bespoke routing).
  const nextStep = useCallback((from: OnboardingStep): OnboardingStep | null => {
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx < 0 || idx >= ONBOARDING_STEP_ORDER.length - 1) return null;
    return ONBOARDING_STEP_ORDER[idx + 1]!;
  }, []);

  const advanceFrom = useCallback(
    (from: OnboardingStep) => {
      const next = nextStep(from);
      if (next) setStep(next);
    },
    [nextStep],
  );

  const handleWelcomeNext = useCallback(() => {
    // Welcome is intentionally not in ONBOARDING_STEP_ORDER (it's a
    // product intro, not a persisted step), so the first persisted
    // step is hard-coded as the entry point.
    setStep(ONBOARDING_STEP_ORDER[0]!);
  }, []);

  // "I've done this before" path — returning user who already has a
  // workspace and just wants to land there. Marks onboarding complete
  // server-side (idempotent via COALESCE on onboarded_at); when the
  // target workspace has no runtime yet, the server seeds the same
  // install-runtime task as Step 3 Skip so the user lands on a
  // concrete next step.
  const handleWelcomeSkip = useCallback(async () => {
    try {
      await completeOnboarding("skip_existing", workspaces[0]?.id);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t(($) => $.errors.skip_failed),
      );
      return;
    }
    onComplete(workspaces[0] ?? undefined);
  }, [workspaces, onComplete]);

  const handleWorkspaceCreated = useCallback(
    (ws: Workspace) => {
      setWorkspace(ws);
      setCurrentWorkspace(ws.slug, ws.id);
      advanceFrom("workspace");
      // A newly-created workspace changes the daemon registration scope.
      // Restart in the background while the user writes the description so
      // a concrete runtime is normally available by the final submit.
      void onRuntimeRefresh?.();
    },
    [advanceFrom, onRuntimeRefresh],
  );

  const handleRuntimeDetected = useCallback(
    (_result: RuntimeDetectionResult | null) => {
      advanceFrom("runtime");
    },
    [advanceFrom],
  );

  const handleDescriptionComplete = useCallback(
    async (description: string) => {
      if (!workspace) return;
      let updatedWorkspace: Workspace;
      let rt: AgentRuntime | null = null;
      try {
        updatedWorkspace = await api.updateWorkspace(workspace.id, {
          description,
        });

        // The desktop daemon was restarted after workspace creation. Give its
        // workspace sync a short window to register the preflight-detected
        // providers, then select the first online runtime for Sili Agent.
        for (let attempt = 0; attempt < 6; attempt += 1) {
          const runtimes = await api.listRuntimes({
            workspace_id: workspace.id,
            owner: "me",
          });
          rt =
            runtimes.find((candidate) => candidate.status === "online") ??
            runtimes[0] ??
            null;
          if (rt) break;
          if (attempt < 5) {
            await new Promise((resolve) => window.setTimeout(resolve, 750));
          }
        }

        await completeOnboarding(
          rt ? "full" : "runtime_skipped",
          workspace.id,
        );
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : t(($) => $.errors.skip_failed),
        );
        return;
      }
      useWelcomeStore.getState().set({
        workspaceId: workspace.id,
        choice: rt ? "runtime" : "skip",
        ...(rt ? { runtimeId: rt.id } : {}),
      });
      onComplete(updatedWorkspace, undefined);
    },
    [workspace, onComplete, t],
  );

  const handleBack = useCallback((from: OnboardingStep) => {
    const idx = ONBOARDING_STEP_ORDER.indexOf(from);
    if (idx <= 0) {
      // Runtime detection (the first persisted step) returns to Welcome.
      setStep("welcome");
      return;
    }
    const prev = ONBOARDING_STEP_ORDER[idx - 1]!;
    setStep(prev);
  }, []);

  // Welcome and all three setup steps own full-bleed layouts.
  const renderStep = () => {
  if (step === "welcome") {
    return (
      <StepWelcome
        onNext={handleWelcomeNext}
        onSkip={canSkipWelcome ? handleWelcomeSkip : undefined}
        isWeb={isWeb}
      />
    );
  }

  if (step === "runtime") {
    return (
      <StepRuntimeDetection
        onProbe={onRuntimeProbe}
        onNext={handleRuntimeDetected}
        onBack={() => handleBack("runtime")}
        installInstructions={runtimeInstructions}
      />
    );
  }

  if (step === "workspace") {
    return (
      <StepWorkspace
        existing={existingWorkspace}
        onCreated={handleWorkspaceCreated}
        onBack={() => handleBack("workspace")}
      />
    );
  }

  if (step === "workspace_description" && workspace) {
    return (
      <StepWorkspaceDescription
        workspace={workspace}
        onComplete={handleDescriptionComplete}
        onBack={() => handleBack("workspace_description")}
      />
    );
  }

  return null;
  };
  return (
    <div className="flex min-h-full flex-col">
      <div className="flex shrink-0 items-center justify-end gap-3 border-b bg-background px-6 pt-12 pb-2">
        <span className="min-w-0 truncate text-caption text-muted-foreground">{user.email}</span>
        <Button variant="ghost" disabled={skipping} onClick={logout}>{tw(($) => $.new_page.log_out)}</Button>
        <Button variant="ghost" disabled={skipping || !workspacesFetched} onClick={() => void handleSkipAll()}>
          {skipping ? t(($) => $.skip_all_pending) : t(($) => $.skip_all)}
        </Button>
      </div>
      <fieldset disabled={skipping} className="min-w-0 flex-1 border-0 p-0">{renderStep()}</fieldset>
    </div>
  );
}

export type { OnboardingStep };
