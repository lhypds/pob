package agent

import (
	"encoding/json"
	"fmt"

	"pob/core/internal/applog"
)

type verifyVerdict int

const (
	verifyVerified verifyVerdict = iota
	verifyResumeStep
	verifyResumePlan
	verifyStop
)

const verifySystemPromptFormat = `You are a verification assistant. Given a screenshot and step details, determine the outcome.
Respond with JSON using one of three results:
  {"result": "verified"} — expectation is met, proceed to next step.
  {"result": "resumeStep", "reason": "..."} — this step failed and should be retried from the beginning.
  {"result": "resumeStep", "stepSeq": N, "reason": "..."} — resume from step N (a specific step sequence number from the plan). Use this when a prior step must be re-executed to fix corrupted state.
  {"result": "resumePlan", "reason": "..."} — a critical error occurred; the entire plan must be recreated and restarted.
  {"result": "stop", "reason": "..."} — execution should stop entirely.

When the current state indicates that earlier steps were not completed correctly (e.g. wrong accumulated value), identify the earliest step that needs to be re-run and set stepSeq to that step's sequence number from the plan below.

Full plan for reference:
%s`

var verifySchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"result": map[string]any{"type": "string", "enum": []string{"verified", "resumeStep", "resumePlan", "stop"}},
		"reason": map[string]any{"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "null"},
		}},
		"stepSeq": map[string]any{"anyOf": []any{
			map[string]any{"type": "integer"},
			map[string]any{"type": "null"},
		}},
	},
	"required":             []string{"result", "reason", "stepSeq"},
	"additionalProperties": false,
}

// verifyStep checks the step's expectation against a fresh screenshot.
// Returns the verdict and, for resumeStep, the optional target step sequence.
func (r *Runner) verifyStep(sess *session, planID string, step planStep, plan string) (verifyVerdict, *int) {
	if step.Expectation == "" {
		return verifyVerified, nil
	}
	shot, err := r.br.CaptureScreenshot(true, nil)
	if err != nil {
		return verifyVerified, nil
	}

	applog.Logf("[plan:%s/step:%d] Verifying: %s", sess.id, step.Sequence, step.Expectation)

	messages := []map[string]any{
		{"role": "system", "content": fmt.Sprintf(verifySystemPromptFormat, plan)},
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": fmt.Sprintf("Step instruction: %s\nExpectation: %s\n\nDoes the current screenshot match this expectation?", step.Instruction, step.Expectation)},
			imagePart(shot),
		}},
	}

	result := r.llm.Chat(messages, nil, verifySchema)
	r.store.SaveVerification(sess.id, planID, step.Sequence,
		append(messages, result.RawAssistantMessage), result.RawAssistantMessage, shot)

	if !result.Success || result.ContentText == "" {
		return verifyVerified, nil
	}
	var parsed struct {
		Result  string `json:"result"`
		Reason  string `json:"reason"`
		StepSeq *int   `json:"stepSeq"`
	}
	if json.Unmarshal([]byte(result.ContentText), &parsed) != nil {
		return verifyVerified, nil
	}

	reasonSuffix := ""
	if parsed.Reason != "" {
		reasonSuffix = ": " + parsed.Reason
	}

	switch parsed.Result {
	case "resumeStep":
		seqDesc := ""
		if parsed.StepSeq != nil {
			seqDesc = fmt.Sprintf(" → step%d", *parsed.StepSeq)
		}
		applog.Logf("[plan:%s/step:%d] Verification RESUME STEP%s%s", sess.id, step.Sequence, seqDesc, reasonSuffix)
		return verifyResumeStep, parsed.StepSeq
	case "resumePlan":
		applog.Logf("[plan:%s/step:%d] Verification RESUME PLAN%s", sess.id, step.Sequence, reasonSuffix)
		return verifyResumePlan, nil
	case "stop":
		applog.Logf("[plan:%s/step:%d] Verification STOP%s", sess.id, step.Sequence, reasonSuffix)
		return verifyStop, nil
	default:
		applog.Logf("[plan:%s/step:%d] Verification PASS", sess.id, step.Sequence)
		return verifyVerified, nil
	}
}
