package llm

// llm.log — one block per model call, written whether the call succeeded or
// not. Every request Pob makes goes through Chat, so this is the whole of what
// the app spends: what was asked, of which model at which address, how big the
// question was, how long it took, how many tokens it came to, and what that is
// in money.
//
// What it does not hold is the messages themselves. A request carries
// screenshots, and a base64 PNG per entry would make the file unreadable and
// unopenable within a day; the session's own logs keep the full conversation
// with the images stripped (see internal/storage), and the purpose line here
// names the session to look in.

import (
	"fmt"
	"strings"
	"time"

	"pob/core/internal/applog"
)

// requestLog is what is known about a call before it is made. The rest — the
// usage, the money, the answer — comes from the result it is written with.
type requestLog struct {
	purpose  string
	endpoint string
	model    string
	messages int
	images   int
	tools    int
	format   string
	bytes    int
	start    time.Time
}

// prices are what the machine is charged, in USD per million tokens. Zero means
// nobody has said, and no money is worked out — the tokens are still logged,
// which is what a bill is worked out from.
type prices struct{ in, out float64 }

// newRequestLog measures the request as it is about to go out.
func newRequestLog(purpose, endpoint, model string, messages []map[string]any, tools int, schema bool, bytes int) requestLog {
	format := "text"
	if schema {
		format = "json_schema"
	}
	return requestLog{
		purpose:  purpose,
		endpoint: endpoint,
		model:    model,
		messages: len(messages),
		images:   countImages(messages),
		tools:    tools,
		format:   format,
		bytes:    bytes,
		start:    time.Now(),
	}
}

// countImages counts the image parts across a conversation — the one thing that
// makes a request large, and the reason a call costs what it does.
func countImages(messages []map[string]any) int {
	n := 0
	for _, message := range messages {
		parts, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for _, part := range parts {
			if p, ok := part.(map[string]any); ok && p["type"] == "image_url" {
				n++
			}
		}
	}
	return n
}

// write appends the block for this call to llm.log.
func (r requestLog) write(result ChatResult, p prices) {
	var b strings.Builder

	fmt.Fprintf(&b, "[%s] %s\n", r.start.UTC().Format("2006-01-02T15:04:05Z"), or(r.purpose, "model call"))
	if r.endpoint != "" {
		fmt.Fprintf(&b, "  endpoint   %s\n", r.endpoint)
	}
	if r.model != "" {
		fmt.Fprintf(&b, "  model      %s\n", r.model)
	}
	if !r.start.IsZero() && r.messages > 0 {
		fmt.Fprintf(&b, "  request    %s\n", r.describeRequest())
		fmt.Fprintf(&b, "  duration   %s\n", time.Since(r.start).Round(time.Millisecond))
	}

	if !result.Success {
		fmt.Fprintf(&b, "  status     failed\n")
		fmt.Fprintf(&b, "  error      %s\n", oneLine(truncate(result.Error, 500)))
		applog.LLM(b.String())
		return
	}

	fmt.Fprintf(&b, "  status     ok\n")
	fmt.Fprintf(&b, "  usage      %s\n", describeUsage(result.Usage))
	fmt.Fprintf(&b, "  cost       %s\n", describeCost(result.Usage, p))
	if answer := describeAnswer(result); answer != "" {
		fmt.Fprintf(&b, "  response   %s\n", answer)
	}

	applog.LLM(b.String())
}

func (r requestLog) describeRequest() string {
	parts := []string{plural(r.messages, "message")}
	if r.images > 0 {
		parts = append(parts, plural(r.images, "image"))
	}
	if r.tools > 0 {
		parts = append(parts, plural(r.tools, "tool"))
	}
	parts = append(parts, r.format, humanBytes(r.bytes))
	return strings.Join(parts, ", ")
}

// describeUsage reads the usage block back out. Every number the provider
// reported is kept: what is billed differs between them, and the token counts
// are what any of their bills is worked out from.
func describeUsage(usage map[string]any) string {
	if len(usage) == 0 {
		return "not reported"
	}
	prompt := usageInt(usage, "prompt_tokens")
	completion := usageInt(usage, "completion_tokens")
	total := usageInt(usage, "total_tokens")
	if total == 0 {
		total = prompt + completion
	}
	cached := nestedInt(usage, "prompt_tokens_details", "cached_tokens")
	reasoning := nestedInt(usage, "completion_tokens_details", "reasoning_tokens")

	return fmt.Sprintf("%d tokens = %d prompt (%d cached) + %d completion (%d reasoning)",
		total, prompt, cached, completion, reasoning)
}

// describeCost prefers what the provider charged over what Pob would work out:
// a gateway that reports a cost has applied its own rates, discounts and cached
// pricing, and that number is the one on the bill.
func describeCost(usage map[string]any, p prices) string {
	if reported, ok := usageFloat(usage, "cost"); ok {
		return fmt.Sprintf("$%.6f reported by the provider", reported)
	}
	if p.in == 0 && p.out == 0 {
		return "— set price_input_per_1m and price_output_per_1m in settings.json"
	}
	prompt := usageInt(usage, "prompt_tokens")
	completion := usageInt(usage, "completion_tokens")
	inCost := float64(prompt) * p.in / 1_000_000
	outCost := float64(completion) * p.out / 1_000_000
	return fmt.Sprintf("$%.6f estimated — in %d × $%g/M + out %d × $%g/M",
		inCost+outCost, prompt, p.in, completion, p.out)
}

// describeAnswer is what came back, short enough to sit on one line. The whole
// of it is in the session's response.json.
func describeAnswer(result ChatResult) string {
	var parts []string
	if text := strings.TrimSpace(result.ContentText); text != "" {
		parts = append(parts, oneLine(truncate(text, 300)))
	}
	for _, call := range result.ToolCalls {
		parts = append(parts, call.Name+"()")
	}
	return strings.Join(parts, " ")
}

func usageInt(usage map[string]any, key string) int {
	if v, ok := usage[key].(float64); ok {
		return int(v)
	}
	return 0
}

func usageFloat(usage map[string]any, key string) (float64, bool) {
	v, ok := usage[key].(float64)
	return v, ok
}

func nestedInt(usage map[string]any, group, key string) int {
	details, ok := usage[group].(map[string]any)
	if !ok {
		return 0
	}
	return usageInt(details, key)
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// oneLine keeps a block one block: a newline in an error or an answer would
// otherwise look like the next field.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}
