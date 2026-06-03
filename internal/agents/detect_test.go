package agents

import (
	"testing"
)

const (
	envAIAgent   = "AI_AGENT"
	envAgent     = "AGENT"
	envGeminiCLI = "GEMINI_CLI"
)

func makeLookup(vars map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

func TestParseAgentName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    AgentName
		wantErr bool
	}{
		{name: "valid lowercase", input: "my-agent", want: "my-agent"},
		{name: "valid with underscore", input: "my_agent_v2", want: "my_agent_v2"},
		{name: "valid uppercase", input: "MyAgent", want: "MyAgent"},
		{name: "valid numbers", input: "agent123", want: "agent123"},
		{name: "spaces rejected", input: "my agent", wantErr: true},
		{name: "newline rejected", input: "my\nagent", wantErr: true},
		{name: "carriage return rejected", input: "my\ragent", wantErr: true},
		{name: "null byte rejected", input: "my\x00agent", wantErr: true},
		{name: "dot rejected", input: "my.agent", wantErr: true},
		{name: "slash rejected", input: "my/agent", wantErr: true},
		{name: "empty rejected", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAgentName(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseAgentName(%q): expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Fatalf("parseAgentName(%q): unexpected error: %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("parseAgentName(%q) = %q, want %q", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestDetectWith(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantAgent AgentName
	}{
		{
			name:      "clean environment",
			env:       map[string]string{},
			wantAgent: "",
		},
		{
			name:      "empty var is not detected",
			env:       map[string]string{envGeminiCLI: ""},
			wantAgent: "",
		},
		{
			name:      "AGENT=amp detected as amp",
			env:       map[string]string{envAgent: "amp"},
			wantAgent: agentAmp,
		},
		{
			name:      "AGENT with non-amp value is ignored",
			env:       map[string]string{envAgent: "other"},
			wantAgent: "",
		},
		{
			name:      "AI_AGENT returns value as agent name",
			env:       map[string]string{envAIAgent: "some-agent"},
			wantAgent: "some-agent",
		},
		{
			name:      "AI_AGENT with invalid characters is ignored",
			env:       map[string]string{envAIAgent: "bad\nagent"},
			wantAgent: "",
		},
		{
			name:      "AI_AGENT with spaces is ignored",
			env:       map[string]string{envAIAgent: "bad agent"},
			wantAgent: "",
		},
		{
			name:      "AI_AGENT takes priority over AGENT",
			env:       map[string]string{envAgent: "amp", envAIAgent: "other"},
			wantAgent: "other",
		},
		{
			name:      "CODEX_SANDBOX",
			env:       map[string]string{"CODEX_SANDBOX": "seatbelt"},
			wantAgent: agentCodex,
		},
		{
			name:      "CODEX_CI",
			env:       map[string]string{"CODEX_CI": "1"},
			wantAgent: agentCodex,
		},
		{
			name:      "CODEX_THREAD_ID",
			env:       map[string]string{"CODEX_THREAD_ID": "abc"},
			wantAgent: agentCodex,
		},
		{
			name:      "GEMINI_CLI",
			env:       map[string]string{envGeminiCLI: "1"},
			wantAgent: agentGeminiCLI,
		},
		{
			name:      "COPILOT_CLI",
			env:       map[string]string{"COPILOT_CLI": "1"},
			wantAgent: agentCopilotCLI,
		},
		{
			name:      "OPENCODE",
			env:       map[string]string{"OPENCODE": "1"},
			wantAgent: agentOpencode,
		},
		{
			name:      "CLAUDECODE",
			env:       map[string]string{"CLAUDECODE": "1"},
			wantAgent: agentClaudeCode,
		},
		{
			name:      "AGENT=amp takes priority over CLAUDECODE",
			env:       map[string]string{"AGENT": "amp", "CLAUDECODE": "1"},
			wantAgent: agentAmp,
		},
		{
			name:      "invalid AI_AGENT falls through to tool-specific detection",
			env:       map[string]string{envAIAgent: "bad agent", envGeminiCLI: "1"},
			wantAgent: agentGeminiCLI,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectWith(makeLookup(tt.env))
			if got != tt.wantAgent {
				t.Errorf("detectWith(%v) = %q, want %q", tt.env, got, tt.wantAgent)
			}
		})
	}
}
