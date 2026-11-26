package put

import "testing"

func TestEndpointIdentityNormalizesEquivalentURLs(t *testing.T) {
	want, err := endpointIdentity("https://example.test/volumes")
	if err != nil {
		t.Fatal(err)
	}
	got, err := endpointIdentity("https://EXAMPLE.test:443/volumes/")
	if err != nil || got != want {
		t.Fatalf("equivalent URLs: identities equal = %v, err = %v", got == want, err)
	}
	for _, url := range []string{
		"http://example.test/volumes",
		"https://another.example.test/volumes",
		"https://example.test:8443/volumes",
		"https://example.test/another",
	} {
		got, err := endpointIdentity(url)
		if err != nil || got == want {
			t.Errorf("%s: identities equal = %v, err = %v", url, got == want, err)
		}
	}
}

func TestEndpointIdentityRejectsSecretBearingURLs(t *testing.T) {
	for _, url := range []string{
		"https://user:sample@example.test",
		"https://example.test/?key=sample",
		"https://example.test/#sample",
	} {
		got, err := endpointIdentity(url)
		if got != "" || err == nil || err.Error() != "invalid API base URL for volume uploads" {
			t.Errorf("invalid endpoint returned a fingerprint or revealed details")
		}
	}
}
