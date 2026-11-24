package account

import (
	"fmt"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/api"
)

func TestDetailsPrintsTheKeyIdentAsTheAPISendsIt(t *testing.T) {
	sent := "abcdEFGHijklMNOP" + strings.Repeat("*", 16)
	result := &api.AccountResult{
		APIKeys: []api.APIKeyInfo{
			{
				Name:   "default",
				APIKey: sent,
			},
		},
	}

	got := newAccountDetailsResult(result)

	if got.APIKeys[0].APIKey != sent {
		t.Errorf("api_keys[0] = %q, want %q", got.APIKeys[0].APIKey, sent)
	}
	if rows := fmt.Sprint(got.Rows()); !strings.Contains(rows, sent) {
		t.Errorf("table rows = %s, want %q in them", rows, sent)
	}
}

func TestDetailsHidesTheSecretHalfIfTheAPISendsIt(t *testing.T) {
	sent := "abcdEFGHijklMNOP" + "qrstUVWXyz012345"
	result := &api.AccountResult{
		APIKeys: []api.APIKeyInfo{
			{
				Name:   "default",
				APIKey: sent,
			},
		},
	}

	got := newAccountDetailsResult(result)

	if want := "abcdEFGHijklMNOP" + strings.Repeat("*", 16); got.APIKeys[0].APIKey != want {
		t.Errorf("api_keys[0] = %q, want %q", got.APIKeys[0].APIKey, want)
	}
	if rows := fmt.Sprint(got.Rows()); strings.Contains(rows, "qrstUVWXyz012345") {
		t.Errorf("table rows = %s, want no secret half in them", rows)
	}
}
