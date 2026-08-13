package serverless

import (
	"bytes"
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

func TestValidateListLimit(t *testing.T) {
	if err := validateListLimit(0); err != nil {
		t.Fatalf("unset limit: %v", err)
	}
	if err := validateListLimit(1); err != nil {
		t.Fatalf("limit 1: %v", err)
	}
	if err := validateListLimit(100); err != nil {
		t.Fatalf("limit 100: %v", err)
	}
	if err := validateListLimit(-1); err == nil {
		t.Fatal("expected error for limit -1")
	}
	if err := validateListLimit(101); err == nil {
		t.Fatal("expected error for limit 101")
	}
}

func TestPrintPage_TableWritesNextCursorToErrOut(t *testing.T) {
	next := "page-2"
	page := serverlessapi.Page[serverlessapi.Deployment]{
		Data:       nil,
		NextCursor: &next,
	}

	var errOut bytes.Buffer
	if err := printPage(output.FormatTable, page, deploymentsResult(page.Data), &errOut); err != nil {
		t.Fatalf("printPage: %v", err)
	}
	if !strings.Contains(errOut.String(), "--cursor page-2") {
		t.Fatalf("errOut missing next-page hint: %q", errOut.String())
	}
}
