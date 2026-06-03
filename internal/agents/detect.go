package agents

import (
	"fmt"
	"os"
	"regexp"
)

// AgentName is a validated agent identifier safe for use in HTTP headers.
type AgentName string

const (
	agentAmp        AgentName = "amp"
	agentClaudeCode AgentName = "claude-code"
	agentCodex      AgentName = "codex"
	agentCopilotCLI AgentName = "copilot-cli"
	agentGeminiCLI  AgentName = "gemini-cli"
	agentOpencode   AgentName = "opencode"
)

var validAgentName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// parseAgentName validates and returns an AgentName from a raw string.
// Only alphanumeric characters, hyphens, and underscores are allowed.
func parseAgentName(s string) (AgentName, error) {
	if !validAgentName.MatchString(s) {
		return "", fmt.Errorf("invalid agent name %q: must match [a-zA-Z0-9_-]+", s)
	}
	return AgentName(s), nil
}

// Detect returns the name of the AI coding agent driving the CLI,
// or an empty AgentName if none is detected.
func Detect() AgentName {
	return detectWith(os.LookupEnv)
}

func detectWith(lookup func(string) (string, bool)) AgentName {
	isSet := func(key string) bool {
		v, ok := lookup(key)
		return ok && v != ""
	}

	valueOf := func(key string) string {
		v, _ := lookup(key)
		return v
	}

	// Generic agent identifier — checked first because it is the most specific signal.
	if v, ok := lookup("AI_AGENT"); ok && v != "" {
		if name, err := parseAgentName(v); err == nil {
			return name
		}
	}

	// Tool-specific variables.
	// Check AGENT=amp before CLAUDECODE since Amp sets both.
	if valueOf("AGENT") == string(agentAmp) {
		return agentAmp
	}

	// OpenAI Codex CLI — https://github.com/openai/codex
	if isSet("CODEX_SANDBOX") || isSet("CODEX_CI") || isSet("CODEX_THREAD_ID") {
		return agentCodex
	}

	// Google Gemini CLI — https://github.com/google-gemini/gemini-cli
	if isSet("GEMINI_CLI") {
		return agentGeminiCLI
	}

	// GitHub Copilot CLI
	if isSet("COPILOT_CLI") {
		return agentCopilotCLI
	}

	// OpenCode — https://github.com/anomalyco/opencode
	if isSet("OPENCODE") {
		return agentOpencode
	}

	// Anthropic Claude Code — https://docs.anthropic.com/en/docs/agents-and-tools/claude-code/overview
	// Checked last because other agents (e.g. Amp) set CLAUDECODE=1 alongside their own vars.
	if isSet("CLAUDECODE") {
		return agentClaudeCode
	}

	return ""
}
