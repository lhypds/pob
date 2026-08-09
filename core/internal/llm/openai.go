// Package llm is a minimal OpenAI-compatible chat-completions client,
// a direct port of the Swift OpenAIClient.
package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type ChatResult struct {
	Success             bool
	ContentText         string
	ToolCalls           []ToolCall
	RawAssistantMessage map[string]any
	Usage               map[string]any
	Error               string
}

type Settings interface {
	APIKey() string
	BaseURL() string
	Model() string
	// The machine's rates, USD per million tokens, for the money line in
	// llm.log. Zero means nobody has said what this model costs.
	InputPricePer1M() float64
	OutputPricePer1M() float64
}

type Client struct {
	settings Settings
	http     *http.Client
}

func New(settings Settings) *Client {
	return &Client{
		settings: settings,
		// Vision requests with several screenshots can be slow; allow ample time.
		http: &http.Client{Timeout: 300 * time.Second},
	}
}

func failure(msg string) ChatResult {
	return ChatResult{Success: false, RawAssistantMessage: map[string]any{}, Error: msg}
}

// Chat sends a multi-turn conversation with optional tool definitions and an
// optional strict JSON-schema response format.
//
// purpose names what the call is for — it heads the call's block in llm.log,
// which every request goes through here to write.
func (c *Client) Chat(purpose string, messages []map[string]any, tools []map[string]any, responseSchema map[string]any) ChatResult {
	entry := requestLog{purpose: purpose, model: c.settings.Model(), start: time.Now()}
	p := prices{in: c.settings.InputPricePer1M(), out: c.settings.OutputPricePer1M()}
	// Named once so every return below writes its block — a call that failed
	// before it left the machine is still a call someone will look for.
	fail := func(msg string) ChatResult {
		result := failure(msg)
		entry.write(result, p)
		return result
	}

	apiKey := c.settings.APIKey()
	if apiKey == "" {
		return fail("API key not configured. Set openai_api_key in settings.json.")
	}

	payload := map[string]any{
		"model":    c.settings.Model(),
		"messages": messages,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		payload["parallel_tool_calls"] = false
	}
	if responseSchema != nil {
		payload["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "response",
				"strict": true,
				"schema": responseSchema,
			},
		}
	}

	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return fail(fmt.Sprintf("Failed to serialize request: %v", err))
	}

	endpoint := c.settings.BaseURL() + "/chat/completions"
	// Measured now: after this the body is a reader the request consumes, and
	// the size of it is most of what a call is charged for.
	entry = newRequestLog(purpose, endpoint, c.settings.Model(), messages, len(tools), responseSchema != nil, body.Len())

	req, err := http.NewRequest("POST", endpoint, &body)
	if err != nil {
		return fail(err.Error())
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fail(err.Error())
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fail(err.Error())
	}
	if resp.StatusCode != 200 {
		return fail(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(data)))
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fail("Unexpected response format")
	}
	choices, _ := parsed["choices"].([]any)
	if len(choices) == 0 {
		return fail("Unexpected response format")
	}
	first, _ := choices[0].(map[string]any)
	message, ok := first["message"].(map[string]any)
	if !ok {
		return fail("Unexpected response format")
	}

	usage, _ := parsed["usage"].(map[string]any)
	contentText, _ := message["content"].(string)

	var toolCalls []ToolCall
	if rawCalls, ok := message["tool_calls"].([]any); ok {
		for _, rc := range rawCalls {
			tc, ok := rc.(map[string]any)
			if !ok {
				continue
			}
			id, _ := tc["id"].(string)
			fn, _ := tc["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			var args map[string]any
			if id == "" || name == "" || json.Unmarshal([]byte(argsStr), &args) != nil {
				continue
			}
			toolCalls = append(toolCalls, ToolCall{ID: id, Name: name, Arguments: args})
		}
	}

	result := ChatResult{
		Success:             true,
		ContentText:         contentText,
		ToolCalls:           toolCalls,
		RawAssistantMessage: message,
		Usage:               usage,
	}
	entry.write(result, p)
	return result
}
