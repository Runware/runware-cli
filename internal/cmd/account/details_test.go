package account

import (
	"fmt"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
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

// maskSecretHalfCase is a key the API sends and what the command prints.
type maskSecretHalfCase struct {
	name string
	sent string
	want string
}

func TestDetailsMasksTheSecretHalf(t *testing.T) {
	ident := "abcdEFGHijklMNOP"
	cases := []maskSecretHalfCase{
		{
			name: "ident and stars, as the API sends it",
			sent: ident + strings.Repeat("*", 16),
			want: ident + strings.Repeat("*", 16),
		},
		{
			name: "full key",
			sent: ident + "qrstUVWXyz012345",
			want: ident + strings.Repeat("*", 16),
		},
		{
			name: "multi-byte characters after the ident",
			sent: ident + "qrstUVWXyz01”345",
			want: ident + strings.Repeat("*", 16),
		},
		{
			name: "ident only",
			sent: ident,
			want: config.MaskedKeySuffix,
		},
		{
			name: "shorter than the ident",
			sent: "short",
			want: config.MaskedKeySuffix,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := &api.AccountResult{
				APIKeys: []api.APIKeyInfo{
					{
						Name:    "default",
						APIKey:  tc.sent,
						Enabled: true,
					},
				},
			}

			got := newAccountDetailsResult(result)

			if got.APIKeys[0].APIKey != tc.want {
				t.Errorf("api_keys[0] = %q, want %q", got.APIKeys[0].APIKey, tc.want)
			}
			if got.APIKeys[0].Name != "default" || !got.APIKeys[0].Enabled {
				t.Errorf("api_keys[0] = %+v, want name and enabled kept", got.APIKeys[0])
			}
			if rows := fmt.Sprint(got.Rows()); !strings.Contains(rows, tc.want) {
				t.Errorf("table rows = %s, want %q in them", rows, tc.want)
			}
		})
	}
}
