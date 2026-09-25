package cmd

import (
	"strings"
	"testing"
)

func TestRootLongMentionsServerless(t *testing.T) {
	cmd := NewRootCmd(newLogger())
	if !strings.Contains(cmd.Long, "serverless") {
		t.Fatalf("root Long does not mention serverless:\n%s", cmd.Long)
	}
}
