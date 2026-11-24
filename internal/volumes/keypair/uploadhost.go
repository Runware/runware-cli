package keypair

import "strings"

// The upload host rule lives beside productionHost and platformHost so that the statement
// keys and the upload addresses decide "production" from one normalisation of the host.

// platformDomain is the registrable domain of productionHost. The upload host rule requires
// the production API to hand out upload addresses only on hosts under it, so a production
// build must not ship verification keys before that holds.
const platformDomain = "runware.ai"

// AllowsUploadTarget reports whether a part may be sent to targetURL, an upload address the
// serverless API at serverlessBaseURL answered with. For the production API host the
// address's host must be platformDomain or a name under it, compared the way the HTTP
// client dials both hosts (see platformHost). Any other API host keeps whatever host it
// answers with, so a development API can hand out its own upload hosts. A base URL with no
// host the CLI could reach is held to the production rule, as statement keys are: it fails
// closed rather than turning the check off.
func AllowsUploadTarget(serverlessBaseURL, targetURL string) bool {
	if api := platformHost(serverlessBaseURL); api != "" && api != productionHost {
		return true
	}
	host := platformHost(targetURL)
	if strings.HasPrefix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	return host == platformDomain || strings.HasSuffix(host, "."+platformDomain)
}
