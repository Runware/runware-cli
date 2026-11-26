package put

import "context"

type contentIdentity struct {
	digest [32]byte
	size   int64
}

func (f *fileState) content() contentIdentity {
	return contentIdentity{
		digest: f.src.SHA256,
		size:   f.src.Size,
	}
}

// transferContents sends one representative of each new content identity first.
// An earlier declaration stays with its original attempt, even if its response
// was lost. Once those files are verified, the server can reuse their blobs for
// the remaining paths, which still get independent declarations and modes.
func (r *runner) transferContents(ctx context.Context, seal sealFile, files, undeclared []*fileState) error {
	var planned []any
	represented := make(map[contentIdentity]struct{}, len(files))
	for _, f := range files {
		if !f.planned {
			planned = append(planned, f.record(recordSource))
		}
		if f.id != "" || f.key != nil {
			represented[f.content()] = struct{}{}
		}
	}
	if len(planned) > 0 {
		if err := r.journal.logSync(planned...); err != nil {
			return err
		}
	}

	var first, deferred []*fileState
	for _, f := range undeclared {
		content := f.content()
		if _, exists := represented[content]; exists && f.key == nil {
			deferred = append(deferred, f)
			continue
		}
		represented[content] = struct{}{}
		first = append(first, f)
	}
	if err := r.declare(ctx, seal, first); err != nil {
		return err
	}
	if err := r.uploadDeclared(ctx, files); err != nil {
		return err
	}
	if err := r.declare(ctx, seal, deferred); err != nil {
		return err
	}
	// A blob that became ineligible between the two declarations can require
	// a regular upload. The API's answer remains authoritative.
	return r.uploadDeclared(ctx, deferred)
}

func (r *runner) uploadDeclared(ctx context.Context, files []*fileState) error {
	var todo []*fileState
	for _, f := range files {
		if f.id != "" && !f.reused && !f.verified {
			todo = append(todo, f)
		}
	}
	return r.uploadFiles(ctx, todo)
}
