package hook

import (
	"encoding/json"
	"fmt"
)

// PromptOptions configure DecidePrompt. The decision itself (stances, state,
// trust) is injected, so this file stays a pure input/output adapter.
type PromptOptions struct {
	// Enter is called with the hook's cwd, the validated Claude session id
	// ("" when missing or not a valid tag) and the user's prompt. It returns
	// the text to add to the model's context, "" for nothing, which is the
	// usual answer.
	Enter func(cwd, session, prompt string) (string, error)
}

// promptInput is the part of the UserPromptSubmit stdin JSON the hook reads.
type promptInput struct {
	Cwd       string          `json:"cwd"`
	SessionID json.RawMessage `json:"session_id"`
	Event     string          `json:"hook_event_name"`
	Prompt    string          `json:"prompt"`
}

type promptOutput struct {
	Specific promptSpecific `json:"hookSpecificOutput"`
}

type promptSpecific struct {
	Event             string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// DecidePrompt is the UserPromptSubmit hook: it returns the JSON to print (an
// additionalContext for the model), or nil plus the reason nothing is added.
// An error means the input itself was unusable. The prompt is never changed or
// blocked: heavyq only adds context, and only when o.Enter says so.
func DecidePrompt(stdin []byte, o PromptOptions) ([]byte, string, error) {
	var in promptInput
	if err := json.Unmarshal(stdin, &in); err != nil {
		return nil, "", fmt.Errorf("stdin is not hook JSON: %w", err)
	}
	if in.Event != "" && in.Event != "UserPromptSubmit" {
		return nil, "event " + in.Event, nil
	}
	if in.Prompt == "" || o.Enter == nil {
		return nil, "no prompt", nil
	}
	text, err := o.Enter(in.Cwd, sessionID(in.SessionID), in.Prompt)
	if err != nil {
		return nil, "", err
	}
	if text == "" {
		return nil, "no stance", nil
	}
	out, err := marshal(promptOutput{promptSpecific{Event: "UserPromptSubmit", AdditionalContext: text}})
	if err != nil {
		return nil, "", err
	}
	return out, "stance injected", nil
}
