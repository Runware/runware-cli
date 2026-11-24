package keypair

import (
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/config"
)

// productionBases are spellings of the production API base URL, and base URLs with no host
// the CLI could reach, all of which hold upload addresses to the platform's domain.
var productionBases = []string{
	config.DefaultServerlessBaseURL,
	"https://API.SERVERLESS.RUNWARE.AI",
	"https://" + productionHost + ".",
	"https://" + productionHost + ":443/v1",
	"https://user@" + productionHost,
	"https://ＡＰＩ.serverless.runware.ai",
	// No host the CLI could reach: the check fails closed.
	"",
	productionHost,
	"http://[::1",
	"https://⒈.com",
}

func TestPlatformDomainHoldsTheProductionHost(t *testing.T) {
	if !strings.HasSuffix(productionHost, "."+platformDomain) {
		t.Fatalf("%s is not under %s", productionHost, platformDomain)
	}
}

// TestAllowsUploadTargetOnThePlatformDomain: the production API's upload addresses on the
// platform's domain are accepted, however the host is spelled.
func TestAllowsUploadTargetOnThePlatformDomain(t *testing.T) {
	for _, base := range productionBases {
		for _, target := range []string{
			"https://volumes.serverless.runware.ai/v1/parts",
			"https://VOLUMES.Serverless.Runware.AI/v1/parts",
			"https://volumes.serverless.runware.ai./v1/parts",
			"https://volumes.serverless.runware.ai:443/v1/parts",
			"https://runware.ai/v1/parts",
			"https://volumes。serverless。runware。ai/v1/parts",
		} {
			if !AllowsUploadTarget(base, target) {
				t.Errorf("base %q refused %q", base, target)
			}
		}
	}
}

// TestAllowsUploadTargetRefusesOtherHostsInProduction: the production API's upload address
// on any other host is refused, including names that only look like the platform's domain.
func TestAllowsUploadTargetRefusesOtherHostsInProduction(t *testing.T) {
	for _, base := range productionBases {
		for _, target := range []string{
			"https://uploads.example.com/v1/parts",
			"https://evilrunware.ai/v1/parts",
			"https://volumes.evilrunware.ai/v1/parts",
			"https://runware.ai.example.com/v1/parts",
			"https://runware.ai.example.com./v1/parts",
			"https://volumes.serverless.runware.ai.example.com/v1/parts",
			"https://volumes.serverless.runware.ai@uploads.example.com/v1/parts",
			"https://uploads.example.com/volumes.serverless.runware.ai",
			"https://uploads.example.com/?host=.runware.ai",
			"https://uploads.example.com#.runware.ai",
			"https://.runware.ai/v1/parts",
			"https://volumes..runware.ai/v1/parts",
			"https://192.0.2.10/v1/parts",
			"https://[2001:db8::1]/v1/parts",
			"https://localhost/v1/parts",
			"https://runware－ai.example.com/v1/parts",
			"https://⒈.com/v1/parts",
			"",
			"://runware.ai",
		} {
			if AllowsUploadTarget(base, target) {
				t.Errorf("base %q accepted %q", base, target)
			}
		}
	}
}

// TestAllowsUploadTargetAnyHostOffProduction: an API host other than production, such as a
// development API, keeps handing out upload addresses on its own hosts.
func TestAllowsUploadTargetAnyHostOffProduction(t *testing.T) {
	for _, base := range []string{
		"https://api.example.test",
		"http://127.0.0.1:8080",
		"https://" + productionHost + ".example.test",
	} {
		for _, target := range []string{
			"https://uploads.example.com/v1/parts",
			"https://127.0.0.1:9443/v1/parts",
			"https://evilrunware.ai/v1/parts",
			"https://volumes.serverless.runware.ai/v1/parts",
		} {
			if !AllowsUploadTarget(base, target) {
				t.Errorf("base %q refused %q", base, target)
			}
		}
	}
}
