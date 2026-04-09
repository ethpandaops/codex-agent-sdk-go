package main

import (
	"context"
	"fmt"
	"os"
	"time"

	codexsdk "github.com/ethpandaops/codex-agent-sdk-go"
)

func main() {
	fmt.Println("=== Include Partial Messages Example ===")
	fmt.Println("Streaming deltas will appear as they arrive, followed by the complete message.")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for msg, err := range codexsdk.Query(ctx, codexsdk.Text("Say 'Hello, streaming world!' one word at a time."),
		codexsdk.WithIncludePartialMessages(true),
		codexsdk.WithPermissionMode("bypassPermissions"),
	) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
			os.Exit(1)
		}

		switch m := msg.(type) {
		case *codexsdk.StreamEvent:
			event := m.Event

			eventType, _ := event["type"].(string)
			if eventType != "content_block_delta" {
				continue
			}

			delta, ok := event["delta"].(map[string]any)
			if !ok {
				continue
			}

			switch delta["type"] {
			case "text_delta":
				text, _ := delta["text"].(string)
				fmt.Print(text)
			case "thinking_delta":
				thinking, _ := delta["thinking"].(string)
				fmt.Printf("[thinking] %s", thinking)
			}

		case *codexsdk.AssistantMessage:
			fmt.Println()
			fmt.Println()
			fmt.Print("Complete: ")

			for _, block := range m.Content {
				if textBlock, ok := block.(*codexsdk.TextBlock); ok {
					fmt.Print(textBlock.Text)
				}
			}

			fmt.Println()

		case *codexsdk.ResultMessage:
			fmt.Println()

			if m.Usage != nil {
				fmt.Printf("Tokens: %d in / %d out\n", m.Usage.InputTokens, m.Usage.OutputTokens)
			}
		}
	}
}
