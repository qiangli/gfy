// Package semantic provides LLM-based semantic extraction via Ollama.
package semantic

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// modelPreference lists model families ranked by capability for
// semantic code extraction (structured JSON output from code context).
var modelPreference = []string{
	"qwen3",
	"gemma3",
	"phi4",
	"deepseek-r1",
	"llama3.1",
	"llama3.2",
	"mistral",
	"codellama",
}

// ModelInfo describes an Ollama model returned by /api/tags.
type ModelInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Size       int64  `json:"size"`
	ParamSize  string `json:"parameter_size"` // e.g. "8.0B", "14.0B"
	paramBytes int64  // parsed from ParamSize
}

// tagsResponse is the JSON shape returned by GET /api/tags.
type tagsResponse struct {
	Models []struct {
		Name    string `json:"name"`
		Model   string `json:"model"`
		Size    int64  `json:"size"`
		Details struct {
			ParamSize string `json:"parameter_size"`
		} `json:"details"`
	} `json:"models"`
}

// Usage tracks token counts from a completion.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Client talks to an Ollama (OpenAI-compatible) server.
type Client struct {
	BaseURL string
	Model   string
}

// Probe checks if Ollama is reachable at the given base URL.
func Probe(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ListModels fetches the list of locally available models from Ollama.
func ListModels(baseURL string) ([]ModelInfo, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(baseURL + "/api/tags")
	if err != nil {
		return nil, fmt.Errorf("connect to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	var tags tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("decode Ollama response: %w", err)
	}

	models := make([]ModelInfo, len(tags.Models))
	for i, m := range tags.Models {
		models[i] = ModelInfo{
			Name:       m.Name,
			Model:      m.Model,
			Size:       m.Size,
			ParamSize:  m.Details.ParamSize,
			paramBytes: parseParamSize(m.Details.ParamSize),
		}
	}
	return models, nil
}

// SelectModel picks the best locally available model for semantic extraction.
// Returns the model name to use, or "" if no suitable model is found.
func SelectModel(models []ModelInfo) string {
	for _, family := range modelPreference {
		var best string
		var bestParams int64
		for _, m := range models {
			// Match by family prefix (e.g., "qwen3" matches "qwen3:8b", "qwen3:14b").
			baseName := strings.Split(m.Name, ":")[0]
			if baseName != family {
				continue
			}
			params := m.paramBytes
			// Prefer largest model <= 14B; if all are larger, take the smallest.
			if params > 0 && params <= 14e9 && params > bestParams {
				best = m.Name
				bestParams = params
			} else if best == "" {
				best = m.Name
				bestParams = params
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

// parseParamSize converts strings like "8.0B", "14B", "3.8B" to byte counts.
func parseParamSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.ToUpper(s)
	multiplier := int64(1)
	if rest, ok := strings.CutSuffix(s, "B"); ok {
		s = rest
		multiplier = 1e9
	} else if rest, ok := strings.CutSuffix(s, "M"); ok {
		s = rest
		multiplier = 1e6
	}
	var val float64
	fmt.Sscanf(s, "%f", &val)
	return int64(val * float64(multiplier))
}

// chatRequest is the OpenAI-compatible chat completion request.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []toolDef     `json:"tools,omitempty"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`   // present in assistant responses
	ToolCallID string     `json:"tool_call_id,omitempty"` // present in tool result messages
}

// chatResponse is the OpenAI-compatible chat completion response.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Tool-use types for OpenAI-compatible function calling.

type toolDef struct {
	Type     string       `json:"type"` // "function"
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  toolParamSchema `json:"parameters"`
}

type toolParamSchema struct {
	Type       string                 `json:"type"` // "object"
	Properties map[string]toolPropDef `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type toolPropDef struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function functionCall `json:"function"`
}

type functionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// astToolsToToolDefs converts ASTTool definitions to the OpenAI tool format.
func astToolsToToolDefs(tools []ASTTool) []toolDef {
	defs := make([]toolDef, len(tools))
	for i, t := range tools {
		props := make(map[string]toolPropDef, len(t.Parameters))
		for name, param := range t.Parameters {
			props[name] = toolPropDef{
				Type:        param.Type,
				Description: param.Description,
			}
		}
		defs[i] = toolDef{
			Type: "function",
			Function: toolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters: toolParamSchema{
					Type:       "object",
					Properties: props,
					Required:   t.Required,
				},
			},
		}
	}
	return defs
}

// ChatCompletion sends a chat completion request to the Ollama OpenAI-compatible endpoint.
func (c *Client) ChatCompletion(system, user string) (string, Usage, error) {
	req := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0,
		Stream:      false,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", Usage{}, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return "", Usage{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// No timeout — LLM inference on large models (35B+) with thousands of
	// AST nodes in the system prompt can take 30+ minutes per chunk.
	httpClient := &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", Usage{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", Usage{}, fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", Usage{}, fmt.Errorf("decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", Usage{}, fmt.Errorf("no choices in response")
	}

	usage := Usage{
		PromptTokens:     chatResp.Usage.PromptTokens,
		CompletionTokens: chatResp.Usage.CompletionTokens,
	}
	return chatResp.Choices[0].Message.Content, usage, nil
}

// maxToolRounds caps the number of tool-use conversation rounds to prevent
// runaway loops on models with weak tool-use support.
const maxToolRounds = 10

// ChatCompletionWithTools sends a chat completion request with tool definitions.
// It runs a conversation loop: if the LLM responds with tool calls, it executes
// them locally and sends results back until the LLM produces a final text response.
// Falls back to regular content if the model doesn't use tools.
func (c *Client) ChatCompletionWithTools(system, user string, tools []ASTTool) (string, Usage, error) {
	toolDefs := astToolsToToolDefs(tools)

	// Build tool dispatch map.
	dispatch := make(map[string]func(map[string]any) string, len(tools))
	for _, t := range tools {
		dispatch[t.Name] = t.Execute
	}

	messages := []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}

	var totalUsage Usage

	for round := 0; round < maxToolRounds; round++ {
		req := chatRequest{
			Model:       c.Model,
			Messages:    messages,
			Tools:       toolDefs,
			Temperature: 0,
			Stream:      false,
		}

		respMsg, usage, err := c.sendRequest(req)
		if err != nil {
			return "", totalUsage, err
		}
		totalUsage.PromptTokens += usage.PromptTokens
		totalUsage.CompletionTokens += usage.CompletionTokens

		// If no tool calls, we have the final response.
		if len(respMsg.ToolCalls) == 0 {
			return respMsg.Content, totalUsage, nil
		}

		// Append assistant message with tool calls to conversation.
		messages = append(messages, chatMessage{
			Role:      "assistant",
			Content:   respMsg.Content,
			ToolCalls: respMsg.ToolCalls,
		})

		// Execute each tool call and append results.
		for _, tc := range respMsg.ToolCalls {
			fn, ok := dispatch[tc.Function.Name]
			if !ok {
				messages = append(messages, chatMessage{
					Role:       "tool",
					Content:    "Unknown tool: " + tc.Function.Name,
					ToolCallID: tc.ID,
				})
				continue
			}

			// Parse arguments JSON.
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				messages = append(messages, chatMessage{
					Role:       "tool",
					Content:    "Invalid arguments: " + err.Error(),
					ToolCallID: tc.ID,
				})
				continue
			}

			result := fn(args)
			fmt.Printf("      tool: %s → %d chars\n", tc.Function.Name, len(result))

			messages = append(messages, chatMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// If we exhausted rounds, return whatever content we have.
	return "", totalUsage, fmt.Errorf("tool-use loop exceeded %d rounds", maxToolRounds)
}

// sendRequest sends a chat request and returns the parsed response message.
func (c *Client) sendRequest(req chatRequest) (struct {
	Content   string
	ToolCalls []toolCall
}, Usage, error) {
	type result struct {
		Content   string
		ToolCalls []toolCall
	}

	body, err := json.Marshal(req)
	if err != nil {
		return result{}, Usage{}, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return result{}, Usage{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return result{}, Usage{}, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result{}, Usage{}, fmt.Errorf("LLM returned status %d", resp.StatusCode)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return result{}, Usage{}, fmt.Errorf("decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return result{}, Usage{}, fmt.Errorf("no choices in response")
	}

	msg := chatResp.Choices[0].Message
	usage := Usage{
		PromptTokens:     chatResp.Usage.PromptTokens,
		CompletionTokens: chatResp.Usage.CompletionTokens,
	}
	return result{Content: msg.Content, ToolCalls: msg.ToolCalls}, usage, nil
}
