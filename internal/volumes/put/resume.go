package put

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
)

// unreadableStateAge is how long resume state that cannot be read is kept, in case another put is still
// creating it; it is past any session deadline, so the state can no longer be resumed or aborted.
const unreadableStateAge = 25 * time.Hour

// resumeFile is what feedFile needs to resume a file: the part digests journalled before, and whether
// the file's completion waits for feedFile's whole-file check.
type resumeFile struct {
	digests map[int]string
	held    bool
}

// resumePlan is a saved session checked against the sources: the keys it saved, by journal index, and
// its files by Volume path.
type resumePlan struct {
	saved      *savedSession
	session    uuid.UUID
	sessionKey []byte
	keys       map[int]*storedFileKey
	byTarget   map[string]*savedFile
	next       int // the first journal index no saved file takes
}

func (p *resumePlan) clear() {
	clear(p.sessionKey)
	for _, k := range p.keys {
		k.Clear()
	}
}

// resume continues this Volume's saved session when every file it saved still matches the sources, and
// reports false when put should open a new session instead. It removes expired state, and aborts a saved
// session that cannot continue: a file changed since it was saved, a key that no longer opens, or
// a newer session for the same Volume. A resumed session fetches no key-pair statement and opens nothing.
func (r *runner) resume(ctx context.Context, saved []*savedSession, sources []Source, before FileList) (bool, Summary, error) {
	if len(saved) == 0 {
		return false, Summary{}, nil
	}
	targets := make(map[string]struct{}, len(sources))
	for _, src := range sources {
		targets[src.Target] = struct{}{}
	}
	saved = slices.DeleteFunc(slices.Clone(saved), func(s *savedSession) bool {
		if len(s.files) == 0 {
			return false
		}
		for _, file := range s.files {
			if _, ok := targets[file.rec.Path]; ok {
				return false
			}
		}
		return true
	})
	if len(saved) == 0 {
		return false, Summary{}, nil
	}
	cache := loadHashCache(r.stateDir)
	err := hashSources(ctx, sources, cache, r.progress, r.o.onHash)
	r.saveCache(cache)
	if err != nil {
		return true, Summary{}, err
	}
	slices.SortFunc(saved, func(a, b *savedSession) int { return b.session.CreatedAt.Compare(a.session.CreatedAt) })
	var plan *resumePlan
	for _, s := range saved {
		if plan != nil {
			r.abortSaved(ctx, s, "a newer session of this Volume is being resumed")
			continue
		}
		p, reason := r.planResume(s, sources)
		if reason != "" {
			r.abortSaved(ctx, s, reason)
			continue
		}
		plan = p
	}
	if plan == nil {
		return false, Summary{}, nil
	}
	defer plan.clear()
	sum, err := r.resumeSession(ctx, plan, sources, before)
	if errors.Is(err, ErrSourceChanged) {
		if fe, ok := errors.AsType[*FileError](err); ok {
			cache.forget(fe.Path)
			r.saveCache(cache)
		}
	}
	return true, sum, err
}

// sweep removes expired resume state, and unreadable state old enough to be abandoned, and returns the
// saved sessions of this organization's Volume.
func (r *runner) sweep() []*savedSession {
	root := filepath.Join(r.stateDir, uploadsDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	now := r.now()
	org := uuid.UUID(r.org).String()
	var mine []*savedSession
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		info, statErr := entry.Info()
		lock, err := lockSession(dir, false)
		if err != nil || lock == nil {
			continue
		}
		s, err := loadSaved(dir)
		if err == nil {
			_, err = s.sessionID()
		}
		if err != nil {
			if statErr == nil && now.Sub(info.ModTime()) > unreadableStateAge {
				r.removeState(dir, "unreadable")
			}
			_ = lock.Close()
			continue
		}
		if !now.Before(s.session.ExpiresAt) {
			r.removeState(dir, "expired")
			clear(s.session.SessionKey)
			_ = lock.Close()
			continue
		}
		if s.session.Organization == org && s.session.Volume == r.o.Volume.Name && s.session.Environment == r.o.Volume.Environment {
			s.lock = lock
			mine = append(mine, s)
		} else {
			clear(s.session.SessionKey)
			_ = lock.Close()
		}
	}
	return mine
}

func (r *runner) removeState(dir, why string) {
	if err := os.RemoveAll(dir); err != nil {
		r.warn("remove the resume state: %v", err)
		return
	}
	r.debug("volume upload: removed resume state", "session", filepath.Base(dir), "reason", why)
}

// planResume checks a saved session against the hashed sources and its keys against its sealing key. A
// non-empty reason says why the session cannot continue.
func (r *runner) planResume(s *savedSession, sources []Source) (*resumePlan, string) {
	id, err := s.sessionID()
	if err != nil {
		return nil, err.Error()
	}
	if s.session.Version != journalVersion {
		return nil, "it was saved in another format"
	}
	if len(s.session.SessionKey) != 32 || s.session.KeyPair == "" {
		return nil, "the session sealing key was lost"
	}
	if s.publishing && len(sources) != len(s.files) {
		return nil, "the paths differ from the upload sent for publication"
	}
	byTarget := make(map[string]*Source, len(sources))
	for i := range sources {
		byTarget[sources[i].Target] = &sources[i]
	}
	p := &resumePlan{
		saved:      s,
		session:    id,
		sessionKey: slices.Clone(s.session.SessionKey),
		keys:       map[int]*storedFileKey{},
		byTarget:   map[string]*savedFile{},
	}
	for _, index := range s.order {
		f := s.files[index]
		src, ok := byTarget[f.rec.Path]
		if !ok {
			p.clear()
			return nil, fmt.Sprintf("%s is no longer among the files to upload", f.rec.Path)
		}
		if src.Size != f.rec.Size || src.Mode != f.rec.Mode || hex.EncodeToString(src.SHA256[:]) != f.rec.SHA256 {
			p.clear()
			return nil, fmt.Sprintf("%s changed since the session was saved", f.rec.Path)
		}
		p.byTarget[f.rec.Path] = f
		p.next = max(p.next, index+1)
		saved, ok, err := loadFileKeys(s.dir, index)
		if err == nil && !ok && !f.keyless() {
			err = fmt.Errorf("%w: the keys of file %d are missing", errBadJournal, index)
		}
		if err != nil {
			p.clear()
			return nil, "a saved file key was lost"
		}
		if !ok {
			continue
		}
		key, err := storedKey(p.sessionKey, id, r.org, f.rec, saved)
		clear(saved.DataKey)
		if err != nil {
			p.clear()
			return nil, "a saved file key was lost"
		}
		p.keys[index] = key
	}
	return p, ""
}

// abortSaved aborts a saved session that cannot continue, then removes its state; the state stays while
// the session stays open.
func (r *runner) abortSaved(ctx context.Context, s *savedSession, reason string) {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()
	err := r.api.AbortUpload(actx, UploadRef{
		Volume: r.o.Volume,
		ID:     s.session.Upload,
	})
	if p, ok := errors.AsType[*ProblemError](err); ok && p.Slug == slugUploadNotOpen {
		r.warn("the saved upload session %s is closed and cannot be resumed because %s; starting a new one", s.session.Upload, reason)
		r.removeState(s.dir, "closed")
		return
	}
	if err == nil {
		r.warn("aborted the saved upload session %s, which cannot be resumed because %s; starting a new one", s.session.Upload, reason)
		r.removeState(s.dir, "aborted")
		return
	}
	r.warn("the saved upload session %s cannot be resumed because %s, and could not be aborted, so it stays open until %s: %v",
		s.session.Upload, reason, s.session.ExpiresAt.Format(time.RFC3339), err)
}

// resumeSession continues a planned session and ends it as a fresh one ends: removing its state on
// completion, keeping it on an interrupt and aborting it on any other failure.
func (r *runner) resumeSession(ctx context.Context, p *resumePlan, sources []Source, before FileList) (Summary, error) {
	r.ref = UploadRef{
		Volume: r.o.Volume,
		ID:     p.session.String(),
	}
	r.expiresAt = p.saved.session.ExpiresAt
	r.completionAttempted = p.saved.publishing
	j, err := reopenJournal(p.saved)
	if err != nil {
		return Summary{}, err
	}
	r.journal = j
	sum, err := r.continueSession(ctx, p, sources, before)
	if err != nil {
		return Summary{}, r.fail(ctx, err)
	}
	if err := r.journal.remove(); err != nil {
		r.warn("%v", err)
	}
	return sum, nil
}

// continueSession rebuilds each file's state from the journal, declares what the session has not
// answered yet, sends the parts not acknowledged and completes the session.
func (r *runner) continueSession(ctx context.Context, p *resumePlan, sources []Source, before FileList) (Summary, error) {
	files := make([]*fileState, 0, len(sources))
	defer func() {
		for _, f := range files {
			if f.key != nil {
				f.key.Clear()
			}
		}
	}()
	r.resumed = map[int]*resumeFile{}
	done := map[*fileState]struct{}{}
	var undeclared []*fileState
	next := p.next
	acked := 0
	for i := range sources {
		src := &sources[i]
		sf := p.byTarget[src.Target]
		if sf == nil {
			files = append(files, &fileState{
				index: next,
				src:   src,
			})
			undeclared = append(undeclared, files[len(files)-1])
			next++
			continue
		}
		f := &fileState{
			index: sf.rec.Index,
			src:   src,
		}
		if key := p.keys[sf.rec.Index]; key != nil {
			f.key = key
		}
		files = append(files, f)
		if sf.declared == nil {
			undeclared = append(undeclared, f)
			continue
		}
		f.id = sf.declared.FileID
		switch sf.declared.State {
		case stateReused:
			f.reused = true
		case stateVerified:
			f.verified = true
		case stateUploading:
			n, err := r.restoreFile(f, sf)
			if err != nil {
				return Summary{}, err
			}
			acked += n
			if sf.complete {
				done[f] = struct{}{}
			} else if int64(n) != f.layout.PartCount {
				// A restored snapshot may predate verification. Replay the original
				// declaration before renewing any missing part of that file.
				undeclared = append(undeclared, f)
			}
		default:
			return Summary{}, fileErr(src.Abs, fmt.Errorf("%w: the file was saved as %q", ErrProtocol, sf.declared.State))
		}
	}
	r.debug("volume upload: resuming session", "session", r.ref.ID, "files", len(files),
		"undeclared", len(undeclared), "acknowledgedParts", acked)
	if err := r.declareResumed(ctx, p, undeclared); err != nil {
		return Summary{}, err
	}
	var todo []*fileState
	for _, f := range files {
		if _, ok := done[f]; !ok && !f.reused && !f.verified {
			todo = append(todo, f)
		}
	}
	if err := r.uploadFiles(ctx, todo); err != nil {
		return Summary{}, err
	}
	return r.publish(ctx, files, sources, before)
}

// restoreFile rebuilds an uploading file from its saved layout and acknowledgements and returns how many
// parts were acknowledged. Its targets are renewed when its parts go out, and its completion waits for
// the whole-file check, since its last part may already be acknowledged.
func (r *runner) restoreFile(f *fileState, sf *savedFile) (int, error) {
	layout, err := checkLayout(f.src.Size, sf.declared.Layout)
	if err != nil {
		return 0, fileErr(f.src.Abs, err)
	}
	f.layout = layout
	if f.key != nil {
		f.identity = f.key.Identity()
	}
	f.targets = map[int]Target{}
	f.results = make([]partResult, layout.PartCount)
	acked := 0
	for part := 1; part <= int(layout.PartCount); part++ {
		if res, ok := sf.trustedAck(layout, part); ok {
			f.results[part-1] = res
			acked++
		}
	}
	f.pending.Store(layout.PartCount - int64(acked) + 1)
	r.resumed[f.index] = &resumeFile{
		digests: sf.digests,
		held:    true,
	}
	return acked, nil
}

// declareResumed declares the files the session has not answered: a saved file again with its saved
// sealed key and attempt, which the API answers as a retry, and a new file under a fresh key sealed with
// the stored session key, journalled before it is declared.
func (r *runner) declareResumed(ctx context.Context, p *resumePlan, files []*fileState) error {
	for start := 0; start < len(files); start += maxDeclarationBatch {
		batch := files[start:min(start+maxDeclarationBatch, len(files))]
		decls := FileDeclarations{Files: make([]FileDeclaration, 0, len(batch))}
		var recs []any
		for _, f := range batch {
			if err := unchanged(f.src); err != nil {
				return err
			}
			if f.key == nil {
				key, err := sealStoredFile(p.sessionKey, p.session, r.org, f.src.SHA256, uint64(f.src.Size))
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
				recs = append(recs, fileRecord{
					Type:   recordFile,
					Index:  f.index,
					Path:   f.src.Target,
					Mode:   f.src.Mode,
					Size:   f.src.Size,
					SHA256: hex.EncodeToString(f.src.SHA256[:]),
				})
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

// resumePart checks a part read for a resumed file against the digest journalled for it before: other
// bytes abort the session, since the file changed between a send and its retry. An acknowledged
// part with the same bytes is not sent again.
func (r *runner) resumePart(f *fileState, part int, digest [32]byte) (bool, error) {
	rf := r.resumed[f.index]
	if rf == nil {
		return true, nil
	}
	if want, ok := rf.digests[part]; ok && want != hex.EncodeToString(digest[:]) {
		return false, fileErr(f.src.Abs, fmt.Errorf("%w: part %d differs from the bytes read for it before", ErrSourceChanged, part))
	}
	if res := f.results[part-1]; res.Ack != "" {
		r.progress.addBytes(res.Len)
		return false, nil
	}
	return true, nil
}

// resumeVerified releases a resumed file's completion once feedFile has read all of it and its digest
// matched.
func (r *runner) resumeVerified(ctx context.Context, f *fileState) error {
	rf := r.resumed[f.index]
	if rf == nil || !rf.held {
		return nil
	}
	if f.pending.Add(-1) == 0 {
		return r.completeFile(ctx, f)
	}
	return nil
}

// debug writes a debug event when a logger is set.
func (r *runner) debug(msg string, args ...any) {
	if r.o.Logger != nil {
		r.o.Logger.Debug(msg, args...)
	}
}
