package message

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAssistantMessage(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name           string
		data           map[string]any
		wantError      bool
		wantParseErr   bool
		wantErrorValue AssistantMessageError
		wantModel      string
		wantContentLen int
		wantToolUseID  *string
	}{
		{
			name: "no error field",
			data: map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "hello"},
					},
					"model": "claude-sonnet-4-5-20250514",
				},
			},
			wantError:      false,
			wantModel:      "claude-sonnet-4-5-20250514",
			wantContentLen: 1,
		},
		{
			name: "authentication_failed error",
			data: map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"content": []any{},
					"model":   "claude-sonnet-4-5-20250514",
				},
				"error": "authentication_failed",
			},
			wantError:      true,
			wantErrorValue: AssistantMessageErrorAuthFailed,
			wantModel:      "claude-sonnet-4-5-20250514",
			wantContentLen: 0,
		},
		{
			name: "rate_limit error",
			data: map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"content": []any{},
					"model":   "claude-sonnet-4-5-20250514",
				},
				"error": "rate_limit",
			},
			wantError:      true,
			wantErrorValue: AssistantMessageErrorRateLimit,
			wantModel:      "claude-sonnet-4-5-20250514",
			wantContentLen: 0,
		},
		{
			name: "unknown error",
			data: map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"content": []any{},
					"model":   "claude-sonnet-4-5-20250514",
				},
				"error": "unknown",
			},
			wantError:      true,
			wantErrorValue: AssistantMessageErrorUnknown,
			wantModel:      "claude-sonnet-4-5-20250514",
			wantContentLen: 0,
		},
		{
			name: "error at top level not in nested message",
			data: map[string]any{
				"type": "assistant",
				"message": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "partial response"},
					},
					"model": "claude-sonnet-4-5-20250514",
					"error": "should_be_ignored",
				},
				"error":              "billing_error",
				"parent_tool_use_id": "tool-123",
			},
			wantError:      true,
			wantErrorValue: AssistantMessageErrorBilling,
			wantModel:      "claude-sonnet-4-5-20250514",
			wantContentLen: 1,
			wantToolUseID:  new("tool-123"),
		},
		{
			name: "missing message field returns parse error",
			data: map[string]any{
				"type": "assistant",
			},
			wantParseErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := Parse(logger, tt.data)

			if tt.wantParseErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			assistant, ok := msg.(*AssistantMessage)
			require.True(t, ok, "expected *AssistantMessage")
			require.Equal(t, "assistant", assistant.Type)
			require.Equal(t, tt.wantModel, assistant.Model)
			require.Len(t, assistant.Content, tt.wantContentLen)

			if tt.wantError {
				require.NotNil(t, assistant.Error)
				require.Equal(t, tt.wantErrorValue, *assistant.Error)
			} else {
				require.Nil(t, assistant.Error)
			}

			if tt.wantToolUseID != nil {
				require.NotNil(t, assistant.ParentToolUseID)
				require.Equal(t, *tt.wantToolUseID, *assistant.ParentToolUseID)
			}
		})
	}
}

func TestParseCodexAgentMessageDeltaSuppression(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name       string
		data       map[string]any
		wantType   string
		wantSystem bool
	}{
		{
			name: "item.updated agent_message suppressed to SystemMessage",
			data: map[string]any{
				"type": "item.updated",
				"item": map[string]any{
					"type": "agent_message",
					"text": "partial delta",
				},
			},
			wantType:   "system",
			wantSystem: true,
		},
		{
			name: "item.started agent_message suppressed to SystemMessage",
			data: map[string]any{
				"type": "item.started",
				"item": map[string]any{
					"type": "agent_message",
					"text": "",
				},
			},
			wantType:   "system",
			wantSystem: true,
		},
		{
			name: "empty completed reasoning suppressed to SystemMessage",
			data: map[string]any{
				"type": "item.completed",
				"item": map[string]any{
					"type": "reasoning",
				},
			},
			wantType:   "system",
			wantSystem: true,
		},
		{
			name: "item.completed agent_message emits AssistantMessage",
			data: map[string]any{
				"type": "item.completed",
				"item": map[string]any{
					"type": "agent_message",
					"text": "complete text",
				},
			},
			wantType:   "assistant",
			wantSystem: false,
		},
		{
			name: "item.updated command_execution emits AssistantMessage",
			data: map[string]any{
				"type": "item.updated",
				"item": map[string]any{
					"type":    "command_execution",
					"id":      "cmd_1",
					"command": "ls",
				},
			},
			wantType:   "assistant",
			wantSystem: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := Parse(logger, tt.data)
			require.NoError(t, err)
			require.Equal(t, tt.wantType, msg.MessageType())

			if tt.wantSystem {
				sys, ok := msg.(*SystemMessage)
				require.True(t, ok, "expected *SystemMessage")
				require.True(t,
					strings.Contains(sys.Subtype, "agent_message_delta") ||
						strings.Contains(sys.Subtype, "reasoning_delta"),
				)
			}
		})
	}
}

func TestParseCodexDynamicToolCall(t *testing.T) {
	logger := slog.Default()

	msg, err := Parse(logger, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"id":   "call_123",
			"type": "dynamic_tool_call",
			"name": "add",
			"arguments": map[string]any{
				"a": 12.0,
				"b": 30.0,
			},
			"success": true,
			"contentItems": []any{
				map[string]any{
					"type": "inputText",
					"text": "{\"result\":42}",
				},
			},
		},
	})
	require.NoError(t, err)

	assistant, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage")
	require.Len(t, assistant.Content, 2)

	toolUse, ok := assistant.Content[0].(*ToolUseBlock)
	require.True(t, ok, "expected ToolUseBlock")
	require.Equal(t, "add", toolUse.Name)
	require.Equal(t, 12.0, toolUse.Input["a"])
	require.Equal(t, 30.0, toolUse.Input["b"])

	toolResult, ok := assistant.Content[1].(*ToolResultBlock)
	require.True(t, ok, "expected ToolResultBlock")
	require.Equal(t, "call_123", toolResult.ToolUseID)
	require.False(t, toolResult.IsError)
	require.Len(t, toolResult.Content, 1)

	textBlock, ok := toolResult.Content[0].(*TextBlock)
	require.True(t, ok, "expected TextBlock")
	require.Equal(t, "{\"result\":42}", textBlock.Text)
}

func TestParseCodexDynamicToolCall_NonTextContentNotDropped(t *testing.T) {
	logger := slog.Default()

	msg, err := Parse(logger, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"id":   "call_image_123",
			"type": "dynamic_tool_call",
			"name": "render_diagram",
			"arguments": map[string]any{
				"prompt": "draw a diagram",
			},
			"success": true,
			"contentItems": []any{
				map[string]any{
					"type":     "image",
					"data":     "ZmFrZV9wbmc=",
					"mimeType": "image/png",
				},
			},
		},
	})
	require.NoError(t, err)

	assistant, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage")
	require.Len(t, assistant.Content, 2)

	toolResult, ok := assistant.Content[1].(*ToolResultBlock)
	require.True(t, ok, "expected ToolResultBlock")
	require.NotEmpty(t,
		toolResult.Content,
		"non-text dynamic tool results should be preserved instead of being dropped",
	)
}

func TestParseCodexDynamicToolCall_PublicSDKMCPNameMatchesMCPToolCallFormat(t *testing.T) {
	logger := slog.Default()

	regularMsg, err := Parse(logger, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"id":     "mcp_123",
			"type":   "mcp_tool_call",
			"server": "calc",
			"tool":   "add",
		},
	})
	require.NoError(t, err)

	sdkMsg, err := Parse(logger, map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"id":   "sdk_123",
			"type": "dynamic_tool_call",
			"tool": "mcp__calc__add",
			"arguments": map[string]any{
				"a": 15.0,
				"b": 27.0,
			},
			"success": true,
		},
	})
	require.NoError(t, err)

	regularAssistant, ok := regularMsg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage for MCP tool call")

	sdkAssistant, ok := sdkMsg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage for SDK-backed MCP tool call")

	regularToolUse, ok := regularAssistant.Content[0].(*ToolUseBlock)
	require.True(t, ok, "expected ToolUseBlock for MCP tool call")

	sdkToolUse, ok := sdkAssistant.Content[0].(*ToolUseBlock)
	require.True(t, ok, "expected ToolUseBlock for SDK-backed MCP tool call")

	require.Equal(t,
		regularToolUse.Name,
		sdkToolUse.Name,
		"SDK-backed MCP tools should expose the same public tool name format as normal MCP tool calls",
	)
}

func TestParseCodexTurnCompletedStructuredOutput(t *testing.T) {
	logger := slog.Default()

	msg, err := Parse(logger, map[string]any{
		"type": "turn.completed",
		"structured_output": map[string]any{
			"answer": "4",
		},
	})
	require.NoError(t, err)

	result, ok := msg.(*ResultMessage)
	require.True(t, ok, "expected *ResultMessage")

	structured, ok := result.StructuredOutput.(map[string]any)
	require.True(t, ok, "expected structured output map")
	require.Equal(t, "4", structured["answer"])
}

func TestParseCodexFileChangeKindObject(t *testing.T) {
	logger := slog.Default()

	data := map[string]any{
		"type": "item.completed",
		"item": map[string]any{
			"id":   "item-1",
			"type": "file_change",
			"changes": []any{
				map[string]any{
					"path": "hello.txt",
					"kind": map[string]any{
						"type": "create",
					},
				},
			},
		},
	}

	msg, err := Parse(logger, data)
	require.NoError(t, err)

	assistant, ok := msg.(*AssistantMessage)
	require.True(t, ok, "expected *AssistantMessage")
	require.Len(t, assistant.Content, 1)

	toolUse, ok := assistant.Content[0].(*ToolUseBlock)
	require.True(t, ok, "expected first content block to be ToolUseBlock")
	require.Equal(t, "Write", toolUse.Name)
	require.Equal(t, "hello.txt", toolUse.Input["file_path"])
}

func TestParseTypedSystemMessages(t *testing.T) {
	logger := slog.Default()

	t.Run("task.started", func(t *testing.T) {
		msg, err := Parse(logger, map[string]any{
			"type":    "system",
			"subtype": "task.started",
			"data": map[string]any{
				"turn_id":                 "turn_123",
				"collaboration_mode_kind": "plan",
				"model_context_window":    float64(256000),
			},
		})
		require.NoError(t, err)

		taskMsg, ok := msg.(*TaskStartedMessage)
		require.True(t, ok, "expected *TaskStartedMessage")
		require.Equal(t, "turn_123", taskMsg.TurnID)
		require.Equal(t, "plan", taskMsg.CollaborationModeKind)
		require.NotNil(t, taskMsg.ModelContextWindow)
		require.Equal(t, int64(256000), *taskMsg.ModelContextWindow)
	})

	t.Run("task.complete", func(t *testing.T) {
		msg, err := Parse(logger, map[string]any{
			"type":    "system",
			"subtype": "task.complete",
			"data": map[string]any{
				"turn_id":            "turn_456",
				"last_agent_message": "done",
			},
		})
		require.NoError(t, err)

		taskMsg, ok := msg.(*TaskCompleteMessage)
		require.True(t, ok, "expected *TaskCompleteMessage")
		require.Equal(t, "turn_456", taskMsg.TurnID)
		require.NotNil(t, taskMsg.LastAgentMessage)
		require.Equal(t, "done", *taskMsg.LastAgentMessage)
	})

	t.Run("thread.rolled_back", func(t *testing.T) {
		msg, err := Parse(logger, map[string]any{
			"type":    "system",
			"subtype": "thread.rolled_back",
			"data": map[string]any{
				"num_turns": float64(2),
			},
		})
		require.NoError(t, err)

		rollbackMsg, ok := msg.(*ThreadRolledBackMessage)
		require.True(t, ok, "expected *ThreadRolledBackMessage")
		require.Equal(t, 2, rollbackMsg.NumTurns)
	})

	t.Run("task.complete requires canonical data envelope", func(t *testing.T) {
		_, err := Parse(logger, map[string]any{
			"type":               "system",
			"subtype":            "task.complete",
			"turn_id":            "turn_456",
			"last_agent_message": "done",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), `"data"`)
	})
}
