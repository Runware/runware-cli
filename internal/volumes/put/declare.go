package put

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

type sealFile func(digest [32]byte, size uint64) (FileKey, error)

// declare sends bounded batches. A replay uses the original key and attempt.
func (r *runner) declare(ctx context.Context, seal sealFile, files []*fileState) error {
	for start := 0; start < len(files); start += maxDeclarationBatch {
		batch := files[start:min(start+maxDeclarationBatch, len(files))]
		decls := FileDeclarations{Files: make([]FileDeclaration, 0, len(batch))}
		var recs []any
		for _, f := range batch {
			if err := unchanged(f.src); err != nil {
				return err
			}
			if f.key == nil {
				key, err := seal(f.src.SHA256, uint64(f.src.Size))
				if err != nil {
					return fileErr(f.src.Abs, fmt.Errorf("seal the data key: %w", err))
				}
				f.key = key
				attempt := key.Attempt()
				if err := r.journal.saveFileKeys(f.index, fileKeyRecord{
					DataKey:   key.DataKey(),
					SealedKey: key.Sealed(),
					Attempt:   hex.EncodeToString(attempt[:]),
				}); err != nil {
					return fileErr(f.src.Abs, err)
				}
				recs = append(recs, f.record(recordFile))
			}
			attempt := f.key.Attempt()
			decls.Files = append(decls.Files, FileDeclaration{
				Path:      f.src.Target,
				Mode:      f.src.Mode,
				Size:      f.src.Size,
				SHA256:    hex.EncodeToString(f.src.SHA256[:]),
				SealedKey: base64.RawURLEncoding.EncodeToString(f.key.Sealed()),
				AttemptID: hex.EncodeToString(attempt[:]),
			})
		}
		if len(recs) > 0 {
			if err := r.journal.logSync(recs...); err != nil {
				return err
			}
			syncDir(r.journal.dir)
		}
		got, err := retryUploadAPI(ctx, r.sleep, func() (DeclarationResult, error) {
			return r.api.DeclareFiles(ctx, r.ref, decls)
		})
		if err != nil {
			return explainDeclare(err, batch)
		}
		if err := r.accept(batch, decls.Files, got); err != nil {
			return err
		}
	}
	return nil
}

func (f *fileState) record(typ string) fileRecord {
	return fileRecord{
		Type:   typ,
		Index:  f.index,
		Path:   f.src.Target,
		Mode:   f.src.Mode,
		Size:   f.src.Size,
		SHA256: hex.EncodeToString(f.src.SHA256[:]),
	}
}
