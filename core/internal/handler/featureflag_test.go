package handler

import (
	"testing"

	"github.com/silieco-ai/silieco/core/internal/featureflags"
	"github.com/silieco-ai/silieco/core/pkg/featureflag"
)

func withComposioMCPAppsFlag(t *testing.T, h *Handler, enabled bool) {
	withFeatureFlag(t, h, featureflags.ComposioMCPApps, enabled)
}

func withResourceLabelsFlag(t *testing.T, h *Handler, enabled bool) {
	withFeatureFlag(t, h, featureflags.ResourceLabels, enabled)
}

func withFeatureFlag(t *testing.T, h *Handler, key string, enabled bool) {
	t.Helper()
	provider := featureflag.NewStaticProvider()
	provider.Set(key, featureflag.Rule{Default: enabled})
	flags := featureflag.NewService(provider)

	origHandlerFlags := h.FeatureFlags
	h.FeatureFlags = flags
	var origRunFlags *featureflag.Service
	if h.AgentRunService != nil {
		origRunFlags = h.AgentRunService.FeatureFlags
		h.AgentRunService.FeatureFlags = flags
	}
	t.Cleanup(func() {
		h.FeatureFlags = origHandlerFlags
		if h.AgentRunService != nil {
			h.AgentRunService.FeatureFlags = origRunFlags
		}
	})
}
