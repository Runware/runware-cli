package serverless

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// unpublishedMarkers are spec extensions that mark an operation, schema or
// field serverless keeps out of its published bundle. The bundle built by
// `make bundle-api-spec` in serverless has none of them, so finding one means
// the source openapi.yaml was copied instead.
var unpublishedMarkers = []string{
	"x-internal",
	"x-runware-",
}

// unpublishedPathPrefixes are API paths that are not published yet.
var unpublishedPathPrefixes = []string{
	"/v1/volumes",
	"/v1/volume-",
}

type specPaths struct {
	Paths map[string]yaml.Node `yaml:"paths"`
}

// TestSpecIsThePublishedBundle keeps api/serverless/openapi.yaml a copy of the
// published bundle, so the generated client only has operations and fields
// customers can call.
func TestSpecIsThePublishedBundle(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "serverless", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	for _, marker := range unpublishedMarkers {
		if bytes.Contains(raw, []byte(marker)) {
			t.Errorf("api/serverless/openapi.yaml contains %q: copy serverless's bin/openapi.public.yaml from `make bundle-api-spec`, not its openapi.yaml", marker)
		}
	}

	var spec specPaths
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Paths) == 0 {
		t.Fatal("the spec has no paths")
	}
	for path := range spec.Paths {
		for _, prefix := range unpublishedPathPrefixes {
			if strings.HasPrefix(path, prefix) {
				t.Errorf("api/serverless/openapi.yaml has the unpublished path %s", path)
			}
		}
	}
}
