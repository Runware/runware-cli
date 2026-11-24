package account

import (
	"testing"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
)

func TestMaskAPIKeysShowsOnlyTheLastFourCharacters(t *testing.T) {
	sent := "abcdEFGHijklMNOPqrstUVWXyz0123456"
	keys := []api.APIKeyInfo{
		{
			Name:   "default",
			APIKey: sent,
		},
	}

	got := maskAPIKeys(keys)

	if want := config.MaskedKeySuffix + "3456"; got[0].APIKey != want {
		t.Errorf("APIKey = %q, want %q", got[0].APIKey, want)
	}
}
