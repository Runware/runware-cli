package serverless

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/output"
)

func TestListPageParams(t *testing.T) {
	limit, cursor := listPageParams(0, "")
	if limit != nil || cursor != nil {
		t.Fatalf("expected nil params, got limit=%v cursor=%v", limit, cursor)
	}

	limit, cursor = listPageParams(25, "abc")
	if limit == nil || *limit != 25 {
		t.Fatalf("unexpected limit: %+v", limit)
	}
	if cursor == nil || *cursor != "abc" {
		t.Fatalf("unexpected cursor: %+v", cursor)
	}
}

func TestPrintPage_TableWritesNextCursorToStderr(t *testing.T) {
	next := "page-2"
	page := serverlessapi.Page[serverlessapi.Deployment]{
		Data:       nil,
		NextCursor: &next,
	}

	stderr := captureStderr(t, func() {
		if err := printPage(output.FormatTable, page, deploymentsResult(page.Data)); err != nil {
			t.Fatalf("printPage: %v", err)
		}
	})
	if !strings.Contains(stderr, "--cursor page-2") {
		t.Fatalf("stderr missing next-page hint: %q", stderr)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()
	_ = w.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return buf.String()
}
