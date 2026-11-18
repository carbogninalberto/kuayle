package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Provider account failures (KUA-218): what the provider says about the
// account or the configuration rather than about the work. Another attempt
// with the same account and model fails the same way, so neither is an agent
// failure a workflow should send back to the agent.
//
// An account out of credit (a prepaid balance or an API quota used up) pauses
// the task, like a usage limit, but nothing resets on its own: the task waits
// until a person tops the account up and resumes it. A model the provider does
// not offer is a configuration error: the task fails and says which model and
// where it is set.

const (
	// ProviderCreditErrorCode prefixes a run's error message when the
	// provider account has no credit left, like UsageLimitErrorCode.
	ProviderCreditErrorCode = "provider_credit_exhausted"
	// ModelNotFoundErrorCode prefixes a run's error message when the
	// provider does not offer the model the run asked for.
	ModelNotFoundErrorCode = "model_not_found"
	// FailureRuleProviderCredit and FailureRuleModelNotFound are the rules
	// that classify those runs (class provider).
	FailureRuleProviderCredit = "provider_credit"
	FailureRuleModelNotFound  = "model_not_found"
	// ProviderCreditResumedReason is a task's reason when its creator
	// resumed it after a credit pause.
	ProviderCreditResumedReason = "Resumed by request after the provider account ran out of credit"
	// providerCreditMessageBytes bounds the provider's text a pause shows.
	providerCreditMessageBytes = 300
	// modelNameBytes bounds a model name read from output.
	modelNameBytes = 200
)

var (
	// providerCreditText is the providers' wording for an account without
	// credit, read only from the harness's own error: OpenCode's "Insufficient
	// balance" (OpenCode Zen and Go), Anthropic's "Your credit balance is too
	// low" (Claude Code with an API key), OpenAI's insufficient_quota and "You
	// exceeded your current quota", and Codex's "Quota exceeded. Check your
	// plan and billing details." A subscription's usage limit is not here: it
	// resets on its own (DetectUsageLimit).
	providerCreditText = regexp.MustCompile(`(?i)(\binsufficient[ _]balance\b|\bcredit balance is too low\b|\binsufficient_quota\b|\byou exceeded your current quota\b|\bquota exceeded\. check your plan and billing details\b)`)
	// urlText is a link in a provider's message; a billing link can name the
	// account's workspace, and the reason never needs it.
	urlText = regexp.MustCompile(`(?i)\b(?:https?://|www\.)\S*`)

	// The providers' wording for a model they do not offer. Each captures the
	// model's name. OpenCode: ProviderModelNotFoundError ("Model not found:
	// provider/model"). Anthropic's API: a not_found_error whose message is
	// "model: <name>". Claude Code: "There's an issue with the selected model
	// (<name>)". OpenAI's API (Codex): "The model `<name>` does not exist or
	// you do not have access to it", code model_not_found.
	openCodeModelNotFound  = regexp.MustCompile(`ProviderModelNotFoundError(?:[^\n]*?\bModel not found:\s*([A-Za-z0-9][\w.:/@+-]*))?`)
	anthropicModelNotFound = regexp.MustCompile(`"type"\s*:\s*"not_found_error"\s*,\s*"message"\s*:\s*"model:\s*([^"\s]+)"`)
	claudeSelectedModel    = regexp.MustCompile(`(?i)there's an issue with the selected model \(([^()\s]+)\)`)
	openAIModelNotFound    = regexp.MustCompile("(?i)\\bthe model [`'\"]?([A-Za-z0-9][\\w.:/@+-]*)[`'\"]? does not exist or you do not have access to it")
	modelNotFoundCode      = regexp.MustCompile(`\bmodel_not_found\b`)
	// openCodeErrorLog is OpenCode's log line for an error on standard error
	// (--print-logs): other output there is not the harness's.
	openCodeErrorLog = regexp.MustCompile(`\blevel=ERROR\b`)
)

// DetectProviderCredit recognises a provider account without credit in a
// failed run: in what the harness reported as its error and in the error
// events of its JSON output (OpenCode and Codex error events, a Claude Code
// result marked is_error). What the agent wrote or a tool printed is never
// read. The message is the provider's sentence without links.
func DetectProviderCredit(harnessError, stdout string) (string, bool) {
	for _, text := range append([]string{harnessError}, harnessErrorEvents(stdout)...) {
		loc := providerCreditText.FindStringIndex(text)
		if loc == nil {
			continue
		}
		message := matchedSentence(text, loc)
		if message == "" {
			message = "The provider account has no credit left"
		}
		return message, true
	}
	return "", false
}

// ProviderCreditErrorMessage encodes the account's state as a run error
// message: "provider_credit_exhausted: <message>".
func ProviderCreditErrorMessage(message string) string {
	return ProviderCreditErrorCode + ": " + message
}

// ParseProviderCreditError reads a run error message written by
// ProviderCreditErrorMessage.
func ParseProviderCreditError(text string) (string, bool) {
	message, ok := strings.CutPrefix(text, ProviderCreditErrorCode+": ")
	return strings.TrimSpace(message), ok
}

// DetectModelNotFound recognises a provider refusing the run's model, and
// names the model when the output does. It reads the harness's error, the
// error events of its JSON output and, on standard error, only OpenCode's
// ERROR log lines: OpenCode reports this error as "UnknownError: Unexpected
// server error" in its event and names it only in its log.
func DetectModelNotFound(harnessError, stdout, stderr string) (string, bool) {
	texts := append([]string{harnessError}, harnessErrorEvents(stdout)...)
	for _, line := range strings.Split(stderr, "\n") {
		if openCodeErrorLog.MatchString(line) {
			texts = append(texts, line)
		}
	}
	found, model := false, ""
	for _, text := range texts {
		for _, pattern := range []*regexp.Regexp{openCodeModelNotFound, anthropicModelNotFound, claudeSelectedModel, openAIModelNotFound, modelNotFoundCode} {
			match := pattern.FindStringSubmatch(text)
			if match == nil {
				continue
			}
			found = true
			if len(match) > 1 && model == "" {
				model = cleanModelName(match[1])
			}
		}
		if model != "" {
			break
		}
	}
	return model, found
}

// ModelNotFoundErrorMessage encodes the refusal as a run error message:
// "model_not_found: <model>", the model empty when the output named none.
func ModelNotFoundErrorMessage(model string) string {
	return ModelNotFoundErrorCode + ": " + model
}

// ParseModelNotFoundError reads a run error message written by
// ModelNotFoundErrorMessage.
func ParseModelNotFoundError(text string) (string, bool) {
	model, ok := strings.CutPrefix(text, ModelNotFoundErrorCode+":")
	return strings.TrimSpace(model), ok
}

// harnessErrorEvents collects the errors a harness reported in its JSON
// output: OpenCode's {"type":"error","error":{"name","data":{"message"}}}
// (a model it does not know carries data.providerID and data.modelID), Codex's
// {"type":"error","message"} and {"type":"turn.failed","error":{"message"}},
// and a Claude Code result with is_error. Tool output, assistant text and a
// successful result are the agent's work, never the provider's error.
func harnessErrorEvents(stdout string) []string {
	var out []string
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event struct {
			Type    string          `json:"type"`
			IsError bool            `json:"is_error"`
			Result  string          `json:"result"`
			Message json.RawMessage `json:"message"`
			Error   json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		switch event.Type {
		case "error", "turn.failed":
			var message string
			if json.Unmarshal(event.Message, &message) == nil && message != "" {
				out = append(out, message)
			}
			if json.Unmarshal(event.Error, &message) == nil && message != "" {
				out = append(out, message)
			}
			var failure struct {
				Name    string `json:"name"`
				Message string `json:"message"`
				Data    struct {
					Message    string `json:"message"`
					ProviderID string `json:"providerID"`
					ModelID    string `json:"modelID"`
				} `json:"data"`
			}
			if json.Unmarshal(event.Error, &failure) != nil {
				continue
			}
			if failure.Message != "" {
				out = append(out, failure.Message)
			}
			if failure.Name != "" || failure.Data.Message != "" {
				out = append(out, strings.Trim(failure.Name+": "+failure.Data.Message, ": "))
			}
			if failure.Name == "ProviderModelNotFoundError" && failure.Data.ModelID != "" {
				model := failure.Data.ModelID
				if failure.Data.ProviderID != "" {
					model = failure.Data.ProviderID + "/" + model
				}
				out = append(out, "ProviderModelNotFoundError: Model not found: "+model)
			}
		case "result":
			if event.IsError && event.Result != "" {
				out = append(out, event.Result)
			}
		}
	}
	return out
}

// matchedSentence is the sentence of text around the match at loc: from the
// previous sentence end, line break or quote to the next. It drops the
// harness's own prefix and any link, and is one bounded line.
func matchedSentence(text string, loc []int) string {
	start := 0
	for i := loc[0] - 1; i >= 0; i-- {
		if text[i] == '\n' || text[i] == '"' || (sentenceEnd(text[i]) && i+1 < len(text) && isSpace(text[i+1])) {
			start = i + 1
			break
		}
	}
	end := len(text)
	for i := loc[1]; i < len(text); i++ {
		if text[i] == '\n' || text[i] == '"' {
			end = i
			break
		}
		if sentenceEnd(text[i]) && (i+1 == len(text) || isSpace(text[i+1])) {
			end = i + 1
			break
		}
	}
	sentence := strings.TrimSpace(text[start:end])
	sentence = strings.TrimSpace(strings.TrimPrefix(sentence, "The harness reported an error:"))
	sentence = strings.Join(strings.Fields(urlText.ReplaceAllString(sentence, "")), " ")
	return TruncateBytes(sentence, providerCreditMessageBytes)
}

func sentenceEnd(c byte) bool { return c == '.' || c == '!' || c == '?' }
func isSpace(c byte) bool     { return c == ' ' || c == '\n' || c == '\t' }

// cleanModelName drops the punctuation a sentence leaves after a name.
func cleanModelName(name string) string {
	name = strings.TrimRight(name, ".,;:")
	if len(name) > modelNameBytes {
		return ""
	}
	return name
}
