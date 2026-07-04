package agent

import (
	"encoding/base64"
	"encoding/json"

	"pob/core/internal/applog"
	"pob/core/internal/storage"
)

const plannerSystemPrompt = `You are a desktop automation planner. Given a task instruction and a screenshot of the current screen, break the task into concrete, executable steps. Each step must describe a single UI interaction in plain language: what element to target, and what action to perform on it (e.g. click, type, scroll, drag, key press). Steps must be specific enough that an agent can execute them one by one without ambiguity. Do not include meta-steps like "verify" or "confirm" unless they require a specific action.

Respond with a JSON object in exactly this format:
{
  "steps": [
    { "sequence": 1, "instruction": "...", "expectation": "..." },
    { "sequence": 2, "instruction": "...", "expectation": "..." }
  ]
}

Fields:
- instruction: the concrete action to perform for this step.
- expectation: the observable outcome that confirms this step succeeded (e.g. "button is highlighted", "dialog appears", "text field is focused").`

var planSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"steps": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sequence":    map[string]any{"type": "integer"},
					"instruction": map[string]any{"type": "string"},
					"expectation": map[string]any{"type": "string"},
				},
				"required":             []string{"sequence", "instruction", "expectation"},
				"additionalProperties": false,
			},
		},
	},
	"required":             []string{"steps"},
	"additionalProperties": false,
}

// generatePlan asks the model to break the instruction into steps, saves the
// plan and returns it as normalized (pretty-printed) JSON, or "" on failure.
func (r *Runner) generatePlan(instruction string, screenshotPNG []byte, sessionID, planID string) string {
	messages := []map[string]any{
		{"role": "system", "content": plannerSystemPrompt},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "Task: " + instruction},
			imagePart(screenshotPNG),
		}},
	}

	result := r.llm.Chat(messages, nil, planSchema)
	if !result.Success {
		applog.Logf("[%s] Plan generation failed: %s", sessionID, result.Error)
	}
	if result.ContentText == "" {
		return ""
	}

	// Copy before adding usage so the raw message stays clean (it is the
	// same map that would be sent back to the API in multi-turn flows).
	responseToSave := map[string]any{"error": result.Error}
	if result.Success {
		responseToSave = shallowCopy(result.RawAssistantMessage)
	}
	if result.Usage != nil {
		responseToSave["usage"] = result.Usage
	}

	normalizedPlan := result.ContentText
	var obj any
	if err := json.Unmarshal([]byte(result.ContentText), &obj); err == nil {
		if pretty, err := storage.PrettyJSON(obj); err == nil {
			normalizedPlan = string(pretty)
		}
	}

	r.store.SavePlan(normalizedPlan, messages, responseToSave, sessionID, planID, screenshotPNG)
	applog.Logf("[%s] Plan saved", sessionID)
	return normalizedPlan
}

// imagePart builds an image_url content part from PNG bytes.
func imagePart(png []byte) map[string]any {
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		},
	}
}
