// Package gemini provides response translation functionality for OpenAI to Gemini API.
// This package handles the conversion of OpenAI Chat Completions API responses into Gemini API-compatible
// JSON format, transforming streaming events and non-streaming responses into the format
// expected by Gemini API clients. It supports both streaming and non-streaming modes,
// handling text content, tool calls, and usage metadata appropriately.
package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ConvertOpenAIResponseToGeminiParams holds parameters for response conversion
type ConvertOpenAIResponseToGeminiParams struct {
	// Tool calls accumulator for streaming
	ToolCallsAccumulator map[int]*ToolCallAccumulator
	// Content accumulator for streaming
	ContentAccumulator strings.Builder
	// Track if this is the first chunk
	IsFirstChunk bool
}

// ToolCallAccumulator holds the state for accumulating tool call data
type ToolCallAccumulator struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

// ConvertOpenAIResponseToGemini converts OpenAI Chat Completions streaming response format to Gemini API format.
// This function processes OpenAI streaming chunks and transforms them into Gemini-compatible JSON responses.
// It handles text content, tool calls, and usage metadata, outputting responses that match the Gemini API format.
//
// Parameters:
//   - ctx: The context for the request.
//   - modelName: The name of the model.
//   - rawJSON: The raw JSON response from the OpenAI API.
//   - param: A pointer to a parameter object for the conversion.
//
// Returns:
//   - []string: A slice of strings, each containing a Gemini-compatible JSON response.
func ConvertOpenAIResponseToGemini(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, param *any) []string {
	if *param == nil {
		*param = &ConvertOpenAIResponseToGeminiParams{
			ToolCallsAccumulator: nil,
			ContentAccumulator:   strings.Builder{},
			IsFirstChunk:         false,
		}
	}

	// Handle [DONE] marker
	if strings.TrimSpace(string(rawJSON)) == "[DONE]" {
		return []string{}
	}

	if bytes.HasPrefix(rawJSON, []byte("data:")) {
		rawJSON = bytes.TrimSpace(rawJSON[5:])
	}

	root := gjson.ParseBytes(rawJSON)

	// Initialize accumulators if needed
	if (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator == nil {
		(*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
	}

	// Process choices
	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		// Handle empty choices array (usage-only chunk)
		if len(choices.Array()) == 0 {
			// This is a usage-only chunk, handle usage and return
			if usage := root.Get("usage"); usage.Exists() {
				template := `{"candidates":[],"usageMetadata":{}}`

				// Set model if available
				if model := root.Get("model"); model.Exists() {
					template, _ = sjson.Set(template, "model", model.String())
				}

				usageObj := map[string]interface{}{
					"promptTokenCount":     usage.Get("prompt_tokens").Int(),
					"candidatesTokenCount": usage.Get("completion_tokens").Int(),
					"totalTokenCount":      usage.Get("total_tokens").Int(),
				}
				template, _ = sjson.Set(template, "usageMetadata", usageObj)
				return []string{template}
			}
			return []string{}
		}

		var results []string

		choices.ForEach(func(choiceIndex, choice gjson.Result) bool {
			// Base Gemini response template without finishReason; set when known
			template := `{"candidates":[{"content":{"parts":[],"role":"model"},"index":0}]}`

			// Set model if available
			if model := root.Get("model"); model.Exists() {
				template, _ = sjson.Set(template, "model", model.String())
			}

			_ = int(choice.Get("index").Int()) // choiceIdx not used in streaming
			delta := choice.Get("delta")

			// Handle role (only in first chunk)
			if role := delta.Get("role"); role.Exists() && (*param).(*ConvertOpenAIResponseToGeminiParams).IsFirstChunk {
				// OpenAI assistant -> Gemini model
				if role.String() == "assistant" {
					template, _ = sjson.Set(template, "candidates.0.content.role", "model")
				}
				(*param).(*ConvertOpenAIResponseToGeminiParams).IsFirstChunk = false
				results = append(results, template)
				return true
			}

			// Handle content delta
			if content := delta.Get("content"); content.Exists() && content.String() != "" {
				contentText := content.String()
				(*param).(*ConvertOpenAIResponseToGeminiParams).ContentAccumulator.WriteString(contentText)

				// Create text part for this delta
				parts := []interface{}{
					map[string]interface{}{
						"text": contentText,
					},
				}
				template, _ = sjson.Set(template, "candidates.0.content.parts", parts)
				results = append(results, template)
				return true
			}

			// Handle tool calls delta
			if toolCalls := delta.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
					toolIndex := int(toolCall.Get("index").Int())
					toolID := toolCall.Get("id").String()
					toolType := toolCall.Get("type").String()
					function := toolCall.Get("function")

					// Skip non-function tool calls explicitly marked as other types.
					if toolType != "" && toolType != "function" {
						return true
					}

					// OpenAI streaming deltas may omit the type field while still carrying function data.
					if !function.Exists() {
						return true
					}

					functionName := function.Get("name").String()
					functionArgs := function.Get("arguments").String()

					// Initialize accumulator if needed so later deltas without type can append arguments.
					if _, exists := (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator[toolIndex]; !exists {
						(*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator[toolIndex] = &ToolCallAccumulator{
							ID:   toolID,
							Name: functionName,
						}
					}

					acc := (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator[toolIndex]

					// Update ID if provided
					if toolID != "" {
						acc.ID = toolID
					}

					// Update name if provided
					if functionName != "" {
						acc.Name = functionName
					}

					// Accumulate arguments
					if functionArgs != "" {
						acc.Arguments.WriteString(functionArgs)
					}

					return true
				})

				// Don't output anything for tool call deltas - wait for completion
				return true
			}

			// Handle finish reason
			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				geminiFinishReason := mapOpenAIFinishReasonToGemini(finishReason.String())
				template, _ = sjson.Set(template, "candidates.0.finishReason", geminiFinishReason)

				// If we have accumulated tool calls, output them now
				if len((*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator) > 0 {
					var parts []interface{}
					for _, accumulator := range (*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator {
						argsStr := accumulator.Arguments.String()
						var argsMap map[string]interface{}

						argsMap = parseArgsToMap(argsStr)

						functionCallPart := map[string]interface{}{
							"functionCall": map[string]interface{}{
								"name": accumulator.Name,
								"args": argsMap,
							},
						}
						parts = append(parts, functionCallPart)
					}

					if len(parts) > 0 {
						template, _ = sjson.Set(template, "candidates.0.content.parts", parts)
					}

					// Clear accumulators
					(*param).(*ConvertOpenAIResponseToGeminiParams).ToolCallsAccumulator = make(map[int]*ToolCallAccumulator)
				}

				results = append(results, template)
				return true
			}

			// Handle usage information
			if usage := root.Get("usage"); usage.Exists() {
				usageObj := map[string]interface{}{
					"promptTokenCount":     usage.Get("prompt_tokens").Int(),
					"candidatesTokenCount": usage.Get("completion_tokens").Int(),
					"totalTokenCount":      usage.Get("total_tokens").Int(),
				}
				template, _ = sjson.Set(template, "usageMetadata", usageObj)
				results = append(results, template)
				return true
			}

			return true
		})
		return results
	}
	return []string{}
}

// mapOpenAIFinishReasonToGemini maps OpenAI finish reasons to Gemini finish reasons
func mapOpenAIFinishReasonToGemini(openAIReason string) string {
	switch openAIReason {
	case "stop":
		return "STOP"
	case "length":
		return "MAX_TOKENS"
	case "tool_calls":
		return "STOP" // Gemini doesn't have a specific tool_calls finish reason
	case "content_filter":
		return "SAFETY"
	default:
		return "STOP"
	}
}

// parseArgsToMap safely parses a JSON string of function arguments into a map.
// It returns an empty map if the input is empty or cannot be parsed as a JSON object.
func parseArgsToMap(argsStr string) map[string]interface{} {
	trimmed := strings.TrimSpace(argsStr)
	if trimmed == "" || trimmed == "{}" {
		return map[string]interface{}{}
	}

	// First try strict JSON
	var out map[string]interface{}
	if errUnmarshal := json.Unmarshal([]byte(trimmed), &out); errUnmarshal == nil {
		return out
	}

	// Tolerant parse: handle streams where values are barewords (e.g., 北京, celsius)
	tolerant := tolerantParseJSONMap(trimmed)
	if len(tolerant) > 0 {
		return tolerant
	}

	// Fallback: return empty object when parsing fails
	return map[string]interface{}{}
}

// tolerantParseJSONMap attempts to parse a JSON-like object string into a map, tolerating
// bareword values (unquoted strings) commonly seen during streamed tool calls.
// Example input: {"location": 北京, "unit": celsius}
func tolerantParseJSONMap(s string) map[string]interface{} {
	// Ensure we operate within the outermost braces if present
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || start >= end {
		return map[string]interface{}{}
	}
	content := s[start+1 : end]

	runes := []rune(content)
	n := len(runes)
	i := 0
	result := make(map[string]interface{})

	for i < n {
		// Skip whitespace and commas
		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t' || runes[i] == ',') {
			i++
		}
		if i >= n {
			break
		}

		// Expect quoted key
		if runes[i] != '"' {
			// Unable to parse this segment reliably; skip to next comma
			for i < n && runes[i] != ',' {
				i++
			}
			continue
		}

		// Parse JSON string for key
		keyToken, nextIdx := parseJSONStringRunes(runes, i)
		if nextIdx == -1 {
			break
		}
		keyName := jsonStringTokenToRawString(keyToken)
		i = nextIdx

		// Skip whitespace
		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t') {
			i++
		}
		if i >= n || runes[i] != ':' {
			break
		}
		i++ // skip ':'
		// Skip whitespace
		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t') {
			i++
		}
		if i >= n {
			break
		}

		// Parse value (string, number, object/array, bareword)
		var value interface{}
		switch runes[i] {
		case '"':
			// JSON string
			valToken, ni := parseJSONStringRunes(runes, i)
			if ni == -1 {
				// Malformed; treat as empty string
				value = ""
				i = n
			} else {
				value = jsonStringTokenToRawString(valToken)
				i = ni
			}
		case '{', '[':
			// Bracketed value: attempt to capture balanced structure
			seg, ni := captureBracketed(runes, i)
			if ni == -1 {
				i = n
			} else {
				var anyVal interface{}
				if errUnmarshal := json.Unmarshal([]byte(seg), &anyVal); errUnmarshal == nil {
					value = anyVal
				} else {
					value = seg
				}
				i = ni
			}
		default:
			// Bare token until next comma or end
			j := i
			for j < n && runes[j] != ',' {
				j++
			}
			token := strings.TrimSpace(string(runes[i:j]))
			// Interpret common JSON atoms and numbers; otherwise treat as string
			if token == "true" {
				value = true
			} else if token == "false" {
				value = false
			} else if token == "null" {
				value = nil
			} else if numVal, ok := tryParseNumber(token); ok {
				value = numVal
			} else {
				value = token
			}
			i = j
		}

		result[keyName] = value

		// Skip trailing whitespace and optional comma before next pair
		for i < n && (runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\r' || runes[i] == '\t') {
			i++
		}
		if i < n && runes[i] == ',' {
			i++
		}
	}

	return result
}

// parseJSONStringRunes returns the JSON string token (including quotes) and the index just after it.
func parseJSONStringRunes(runes []rune, start int) (string, int) {
	if start >= len(runes) || runes[start] != '"' {
		return "", -1
	}
	i := start + 1
	escaped := false
	for i < len(runes) {
		r := runes[i]
		if r == '\\' && !escaped {
			escaped = true
			i++
			continue
		}
		if r == '"' && !escaped {
			return string(runes[start : i+1]), i + 1
		}
		escaped = false
		i++
	}
	return string(runes[start:]), -1
}

// jsonStringTokenToRawString converts a JSON string token (including quotes) to a raw Go string value.
func jsonStringTokenToRawString(token string) string {
	var s string
	if errUnmarshal := json.Unmarshal([]byte(token), &s); errUnmarshal == nil {
		return s
	}
	// Fallback: strip surrounding quotes if present
	if len(token) >= 2 && token[0] == '"' && token[len(token)-1] == '"' {
		return token[1 : len(token)-1]
	}
	return token
}

// captureBracketed captures a balanced JSON object/array starting at index i.
// Returns the segment string and the index just after it; -1 if malformed.
func captureBracketed(runes []rune, i int) (string, int) {
	if i >= len(runes) {
		return "", -1
	}
	startRune := runes[i]
	var endRune rune
	if startRune == '{' {
		endRune = '}'
	} else if startRune == '[' {
		endRune = ']'
	} else {
		return "", -1
	}
	depth := 0
	j := i
	inStr := false
	escaped := false
	for j < len(runes) {
		r := runes[j]
		if inStr {
			if r == '\\' && !escaped {
				escaped = true
				j++
				continue
			}
			if r == '"' && !escaped {
				inStr = false
			} else {
				escaped = false
			}
			j++
			continue
		}
		if r == '"' {
			inStr = true
			j++
			continue
		}
		if r == startRune {
			depth++
		} else if r == endRune {
			depth--
			if depth == 0 {
				return string(runes[i : j+1]), j + 1
			}
		}
		j++
	}
	return string(runes[i:]), -1
}

// tryParseNumber attempts to parse a string as an int or float.
func tryParseNumber(s string) (interface{}, bool) {
	if s == "" {
		return nil, false
	}
	// Try integer
	if i64, errParseInt := strconv.ParseInt(s, 10, 64); errParseInt == nil {
		return i64, true
	}
	if u64, errParseUInt := strconv.ParseUint(s, 10, 64); errParseUInt == nil {
		return u64, true
	}
	if f64, errParseFloat := strconv.ParseFloat(s, 64); errParseFloat == nil {
		return f64, true
	}
	return nil, false
}

// ConvertOpenAIResponseToGeminiNonStream converts a non-streaming OpenAI response to a non-streaming Gemini response.
//
// Parameters:
//   - ctx: The context for the request.
//   - modelName: The name of the model.
//   - rawJSON: The raw JSON response from the OpenAI API.
//   - param: A pointer to a parameter object for the conversion.
//
// Returns:
//   - string: A Gemini-compatible JSON response.
func ConvertOpenAIResponseToGeminiNonStream(_ context.Context, _ string, originalRequestRawJSON, requestRawJSON, rawJSON []byte, _ *any) string {
	root := gjson.ParseBytes(rawJSON)

	// Base Gemini response template without finishReason; set when known
	out := `{"candidates":[{"content":{"parts":[],"role":"model"},"index":0}]}`

	// Set model if available
	if model := root.Get("model"); model.Exists() {
		out, _ = sjson.Set(out, "model", model.String())
	}

	// Process choices
	if choices := root.Get("choices"); choices.Exists() && choices.IsArray() {
		choices.ForEach(func(choiceIndex, choice gjson.Result) bool {
			choiceIdx := int(choice.Get("index").Int())
			message := choice.Get("message")

			// Set role
			if role := message.Get("role"); role.Exists() {
				if role.String() == "assistant" {
					out, _ = sjson.Set(out, "candidates.0.content.role", "model")
				}
			}

			var parts []interface{}

			// Handle content first
			if content := message.Get("content"); content.Exists() && content.String() != "" {
				parts = append(parts, map[string]interface{}{
					"text": content.String(),
				})
			}

			// Handle tool calls
			if toolCalls := message.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
				toolCalls.ForEach(func(_, toolCall gjson.Result) bool {
					if toolCall.Get("type").String() == "function" {
						function := toolCall.Get("function")
						functionName := function.Get("name").String()
						functionArgs := function.Get("arguments").String()

						// Parse arguments
						var argsMap map[string]interface{}
						argsMap = parseArgsToMap(functionArgs)

						functionCallPart := map[string]interface{}{
							"functionCall": map[string]interface{}{
								"name": functionName,
								"args": argsMap,
							},
						}
						parts = append(parts, functionCallPart)
					}
					return true
				})
			}

			// Set parts
			if len(parts) > 0 {
				out, _ = sjson.Set(out, "candidates.0.content.parts", parts)
			}

			// Handle finish reason
			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				geminiFinishReason := mapOpenAIFinishReasonToGemini(finishReason.String())
				out, _ = sjson.Set(out, "candidates.0.finishReason", geminiFinishReason)
			}

			// Set index
			out, _ = sjson.Set(out, "candidates.0.index", choiceIdx)

			return true
		})
	}

	// Handle usage information
	if usage := root.Get("usage"); usage.Exists() {
		usageObj := map[string]interface{}{
			"promptTokenCount":     usage.Get("prompt_tokens").Int(),
			"candidatesTokenCount": usage.Get("completion_tokens").Int(),
			"totalTokenCount":      usage.Get("total_tokens").Int(),
		}
		out, _ = sjson.Set(out, "usageMetadata", usageObj)
	}

	return out
}
