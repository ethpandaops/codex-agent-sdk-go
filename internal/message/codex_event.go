package message

import (
	"encoding/json"
	"fmt"
)

// EventType represents the type of a Codex CLI JSONL event.
type EventType string

const (
	// EventThreadStarted is emitted when a new thread begins.
	EventThreadStarted EventType = "thread.started"

	// EventTurnStarted is emitted at the start of each agent turn.
	EventTurnStarted EventType = "turn.started"

	// EventTurnCompleted is emitted when a turn finishes successfully.
	EventTurnCompleted EventType = "turn.completed"

	// EventTurnFailed is emitted when a turn fails.
	EventTurnFailed EventType = "turn.failed"

	// EventItemStarted is emitted when an item begins processing.
	EventItemStarted EventType = "item.started"

	// EventItemUpdated is emitted when an item is incrementally updated.
	EventItemUpdated EventType = "item.updated"

	// EventItemCompleted is emitted when an item finishes processing.
	EventItemCompleted EventType = "item.completed"

	// EventError is emitted for top-level errors.
	EventError EventType = "error"
)

// CodexEvent is a single JSONL event from the Codex CLI.
//
//nolint:tagliatelle // JSON tags match Codex CLI wire format.
type CodexEvent struct {
	Type     EventType    `json:"type"`
	ThreadID string       `json:"thread_id,omitempty"`
	Item     *CodexItem   `json:"item,omitempty"`
	Usage    *CodexUsage  `json:"usage,omitempty"`
	Message  string       `json:"message,omitempty"`
	Error    *ErrorDetail `json:"error,omitempty"`
}

// ErrorDetail contains error information from turn.failed events.
type ErrorDetail struct {
	Message string `json:"message"`
}

// ItemType represents the type of an item in an event.
type ItemType string

const (
	// ItemTypeAgentMessage is a text response from the agent.
	ItemTypeAgentMessage ItemType = "agent_message"
	// ItemTypeReasoning is internal chain-of-thought reasoning.
	ItemTypeReasoning ItemType = "reasoning"
	// ItemTypeCommandExec is a shell command execution.
	ItemTypeCommandExec ItemType = "command_execution"
	// ItemTypeFileChange is a file modification.
	ItemTypeFileChange ItemType = "file_change"
	// ItemTypeMCPToolCall is a Model Context Protocol tool invocation.
	ItemTypeMCPToolCall ItemType = "mcp_tool_call"
	// ItemTypeDynamicToolCall is an SDK dynamic tool invocation.
	ItemTypeDynamicToolCall ItemType = "dynamic_tool_call"
	// ItemTypeWebSearch is a web search operation.
	ItemTypeWebSearch ItemType = "web_search"
	// ItemTypeTodoList is a todo list update.
	ItemTypeTodoList ItemType = "todo_list"
	// ItemTypeError is an error item.
	ItemTypeError ItemType = "error"
)

// CodexItem represents an item in a Codex event.
//
//nolint:tagliatelle // JSON tags match Codex CLI wire format.
type CodexItem struct {
	ID   string   `json:"id"`
	Type ItemType `json:"type"`

	// agent_message / reasoning
	Text string `json:"text,omitempty"`

	// command_execution
	Command          string `json:"command,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	Status           string `json:"status,omitempty"`

	// file_change
	Changes []FileChange `json:"changes,omitempty"`

	// mcp_tool_call
	Server string `json:"server,omitempty"`
	Tool   string `json:"tool,omitempty"`

	// dynamic_tool_call
	Name         string         `json:"name,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	ContentItems []ContentItem  `json:"contentItems,omitempty"`
	Success      *bool          `json:"success,omitempty"`

	// web_search
	Query string `json:"query,omitempty"`

	// todo_list
	Items []TodoItem `json:"items,omitempty"`

	// error
	Message string `json:"message,omitempty"`
}

// FileChange represents a single file modification.
type FileChange struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

// UnmarshalJSON accepts the current file-change kind shape.
func (f *FileChange) UnmarshalJSON(data []byte) error {
	var raw struct {
		Path string `json:"path"`
		Kind any    `json:"kind"`
	}

	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	f.Path = raw.Path

	kind, ok := raw.Kind.(map[string]any)
	if !ok {
		return fmt.Errorf("file change: missing or invalid kind object")
	}

	kindType, ok := kind["type"].(string)
	if !ok || kindType == "" {
		return fmt.Errorf("file change: missing or invalid kind type")
	}

	f.Kind = kindType

	return nil
}

// TodoItem represents an item in a todo list.
type TodoItem struct {
	Text      string `json:"text"`
	Completed bool   `json:"completed"`
}

// ContentItem represents a tool result content item from app-server.
type ContentItem struct {
	Type string         `json:"type"`
	Text string         `json:"text,omitempty"`
	Raw  map[string]any `json:"-"`
}

// UnmarshalJSON preserves the full content item payload so non-text tool
// results can be surfaced without losing fields.
func (c *ContentItem) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.Raw = raw

	if contentType, ok := raw["type"].(string); ok {
		c.Type = contentType
	}

	if text, ok := raw["text"].(string); ok {
		c.Text = text
	}

	return nil
}

// CodexUsage contains token consumption metrics from Codex.
//
//nolint:tagliatelle // JSON tags match Codex CLI wire format.
type CodexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// ParseCodexEvent converts a raw JSON map into a typed CodexEvent.
func ParseCodexEvent(raw map[string]any) (*CodexEvent, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var event CodexEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, err
	}

	return &event, nil
}
