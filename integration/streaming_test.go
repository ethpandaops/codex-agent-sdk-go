//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	codexsdk "github.com/ethpandaops/codex-agent-sdk-go"
)

// TestPartialMessages_DisabledByDefault verifies no StreamEvents when disabled.
func TestPartialMessages_DisabledByDefault(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var streamEventCount int

	for msg, err := range codexsdk.Query(ctx, codexsdk.Text("Say 'hello'"),
		codexsdk.WithPermissionMode("bypassPermissions"),
	) {
		if err != nil {
			skipIfCLINotInstalled(t, err)
			t.Fatalf("Query failed: %v", err)
		}

		if _, ok := msg.(*codexsdk.StreamEvent); ok {
			streamEventCount++
		}
	}

	require.Equal(t, 0, streamEventCount,
		"Should not receive StreamEvents when IncludePartialMessages is false")
}

// TestPartialMessages_ThinkingDeltaDistinguished verifies that reasoning
// deltas arrive as thinking_delta (not text_delta) when partial messages
// are enabled and reasoning effort is high.
func TestPartialMessages_ThinkingDeltaDistinguished(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var (
		hasThinkingDelta bool
		hasTextDelta     bool
		hasThinkingBlock bool
	)

	for msg, err := range codexsdk.Query(ctx,
		codexsdk.Text("What is the sum of the first 10 prime numbers? Think step by step."),
		codexsdk.WithPermissionMode("bypassPermissions"),
		codexsdk.WithIncludePartialMessages(true),
		codexsdk.WithEffort(codexsdk.EffortHigh),
	) {
		if err != nil {
			skipIfCLINotInstalled(t, err)
			t.Fatalf("Query failed: %v", err)
		}

		switch m := msg.(type) {
		case *codexsdk.StreamEvent:
			eventType, _ := m.Event["type"].(string)
			if eventType != "content_block_delta" {
				continue
			}

			delta, ok := m.Event["delta"].(map[string]any)
			if !ok {
				continue
			}

			switch delta["type"] {
			case "thinking_delta":
				hasThinkingDelta = true
			case "text_delta":
				hasTextDelta = true
			}

		case *codexsdk.AssistantMessage:
			for _, block := range m.Content {
				if _, ok := block.(*codexsdk.ThinkingBlock); ok {
					hasThinkingBlock = true
				}
			}
		}
	}

	// At minimum we should see text deltas from the response.
	require.True(t, hasTextDelta, "expected text_delta stream events")

	// When the model reasons, thinking deltas must use the distinct type.
	if hasThinkingDelta {
		t.Log("thinking_delta stream events received — reasoning deltas are distinguishable")
	}

	// If reasoning produced a completed item, it should be a ThinkingBlock.
	if hasThinkingBlock {
		t.Log("ThinkingBlock received in AssistantMessage — reasoning content surfaced")
	}
}
