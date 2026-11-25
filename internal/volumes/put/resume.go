package put

import (
	"context"
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

// resumeCandidates releases locks on sessions that address other source paths.
func (r *runner) resumeCandidates(saved []*savedSession, sources []Source) []*savedSession {
	targets := make(map[string]struct{}, len(sources))
	for _, src := range sources {
		targets[src.Target] = struct{}{}
	}
	var candidates []*savedSession
	for _, s := range saved {
		if r.matchesTargets(s, targets) {
			candidates = append(candidates, s)
		} else {
			s.release()
		}
	}
	return candidates
}

func (r *runner) matchesTargets(s *savedSession, targets map[string]struct{}) bool {
	if s.session.Invocation != "" {
		if s.session.Invocation == r.invocation {
			return true
		}
		// A copied journal may be resumed from another directory.
		if len(s.sources) != len(targets) {
			return false
		}
		for path := range s.sources {
			if _, ok := targets[path]; !ok {
				return false
			}
		}
		return len(s.sources) > 0
	}
	for _, file := range s.files {
		if _, ok := targets[file.rec.Path]; ok {
			return true
		}
	}
	return len(s.files) == 0
}

// resume continues a matching saved session without opening a new one or
// fetching a key-pair statement.
func (r *runner) resume(ctx context.Context, saved []*savedSession, sources []Source, before FileList) (bool, Summary, error) {
	if len(saved) == 0 {
		return false, Summary{}, nil
	}
	cache := loadHashCache(r.stateDir)
	err := hashSources(ctx, sources, cache, r.progress, r.o.onHash)
	r.saveCache(cache)
	if err != nil {
		if file, ok := errors.AsType[*FileError](err); ok && errors.Is(err, os.ErrNotExist) {
			err = fileErr(file.Path, ErrSourceChanged)
		}
		if errors.Is(err, ErrSourceChanged) {
			for _, s := range saved {
				if s.session.Invocation == r.invocation || s.session.Invocation == "" {
					aborted := r.abortSaved(ctx, s, sourceChangeReason(err))
					if s.publishing && !aborted {
						return true, Summary{}, uncertainSourceChange(err)
					}
				}
			}
		}
		return true, Summary{}, err
	}
	slices.SortFunc(saved, func(a, b *savedSession) int { return b.session.CreatedAt.Compare(a.session.CreatedAt) })
	var plan *resumePlan
	defer func() {
		if plan != nil {
			plan.clear()
		}
	}()
	for _, s := range saved {
		p, err := r.planResume(s, sources)
		if err != nil {
			// Matching target names alone cannot identify a copied session.
			if errors.Is(err, ErrSourceChanged) && s.session.Invocation != "" && s.session.Invocation != r.invocation {
				s.release()
				continue
			}
			reason := err.Error()
			if errors.Is(err, ErrSourceChanged) {
				reason = sourceChangeReason(err)
			}
			aborted := r.abortSaved(ctx, s, reason)
			s.release()
			if plan == nil && errors.Is(err, ErrSourceChanged) {
				if s.publishing && !aborted {
					return true, Summary{}, uncertainSourceChange(err)
				}
				return true, Summary{}, err
			}
			continue
		}
		if plan != nil {
			p.clear()
			r.abortSaved(ctx, s, "a newer matching session is being resumed")
			s.release()
			continue
		}
		plan = p
	}
	if plan == nil {
		return false, Summary{}, nil
	}
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
		abandoned := statErr == nil && now.Sub(info.ModTime()) > unreadableStateAge
		if _, err := os.Stat(filepath.Join(dir, journalName)); errors.Is(err, os.ErrNotExist) && !abandoned {
			continue
		}
		lock, err := lockSession(dir, false)
		if err != nil || lock == nil {
			continue
		}
		s, err := loadSaved(dir)
		if err == nil {
			_, err = s.sessionID()
		}
		if err != nil {
			if abandoned {
				r.removeState(dir, lock, "unreadable")
			}
			_ = lock.Close()
			continue
		}
		if !now.Before(s.session.ExpiresAt) {
			r.removeState(dir, lock, "expired")
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

func (r *runner) removeState(dir string, lock *os.File, why string) {
	if err := removeSessionState(dir, lock); err != nil {
		r.warn("%v", err)
		return
	}
	r.debug("volume upload: removed resume state", "session", filepath.Base(dir), "reason", why)
}

// planResume checks the saved sources and verifies each in-flight key.
func (r *runner) planResume(s *savedSession, sources []Source) (*resumePlan, error) {
	id, err := s.sessionID()
	if err != nil {
		return nil, err
	}
	if s.session.Version != journalVersion {
		return nil, errors.New("it was saved in another format")
	}
	if len(s.session.SessionKey) != 32 || s.session.KeyPair == "" {
		return nil, errors.New("the session sealing key was lost")
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
	for _, rec := range s.sources {
		src, ok := byTarget[rec.Path]
		if !ok {
			p.clear()
			return nil, fileErr(rec.Path, ErrSourceChanged)
		}
		if !matchesSource(rec, src) {
			p.clear()
			return nil, fileErr(src.Abs, ErrSourceChanged)
		}
		p.next = max(p.next, rec.Index+1)
	}
	for _, index := range s.order {
		f := s.files[index]
		src, ok := byTarget[f.rec.Path]
		if !ok {
			p.clear()
			return nil, fileErr(f.rec.Path, ErrSourceChanged)
		}
		if !matchesSource(f.rec, src) {
			p.clear()
			return nil, fileErr(src.Abs, ErrSourceChanged)
		}
		p.byTarget[f.rec.Path] = f
		p.next = max(p.next, index+1)
		saved, ok, err := loadFileKeys(s.dir, index)
		if err == nil && !ok && !f.keyless() {
			err = fmt.Errorf("%w: the keys of file %d are missing", errBadJournal, index)
		}
		if err != nil {
			p.clear()
			return nil, errors.New("a saved file key was lost")
		}
		if !ok {
			continue
		}
		key, err := storedKey(p.sessionKey, id, r.org, f.rec, saved)
		clear(saved.DataKey)
		if err != nil {
			p.clear()
			return nil, errors.New("a saved file key was lost")
		}
		p.keys[index] = key
	}
	if s.publishing && len(sources) != len(s.files) {
		p.clear()
		return nil, errors.New("the paths differ from the upload sent for publication")
	}
	return p, nil
}

func matchesSource(rec fileRecord, src *Source) bool {
	return src.Size == rec.Size && src.Mode == rec.Mode && hex.EncodeToString(src.SHA256[:]) == rec.SHA256
}

func sourceChangeReason(err error) string {
	file, ok := errors.AsType[*FileError](err)
	if !ok {
		return "a source changed"
	}
	return file.Path + " changed"
}

func uncertainSourceChange(err error) error {
	return fmt.Errorf("%s and the earlier upload may have published. Check the Volume's files before running put again", sourceChangeReason(err))
}

// abortSaved aborts a saved session that cannot continue, then removes its state; the state stays while
// the session stays open.
func (r *runner) abortSaved(ctx context.Context, s *savedSession, reason string) bool {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()
	err := r.api.AbortUpload(actx, UploadRef{
		Volume: r.o.Volume,
		ID:     s.session.Upload,
	})
	if p, ok := errors.AsType[*ProblemError](err); ok && p.Slug == slugUploadNotOpen {
		r.warn("the saved upload session %s is closed and cannot be resumed because %s", s.session.Upload, reason)
		r.removeState(s.dir, s.lock, "closed")
		return false
	}
	if err == nil {
		r.warn("aborted the saved upload session %s, which cannot be resumed because %s", s.session.Upload, reason)
		r.removeState(s.dir, s.lock, "aborted")
		return true
	}
	r.warn("the saved upload session %s cannot be resumed because %s, and could not be aborted, so it stays open until %s: %v",
		s.session.Upload, reason, s.session.ExpiresAt.Format(time.RFC3339), err)
	return false
}

// resumeSession continues a planned session and uses the fresh upload's
// publication and failure handling.
func (r *runner) resumeSession(ctx context.Context, p *resumePlan, sources []Source, before FileList) (Summary, error) {
	r.ref = UploadRef{
		Volume: r.o.Volume,
		ID:     p.session.String(),
	}
	r.expiresAt = p.saved.session.ExpiresAt
	r.completionAttempted = p.saved.publishing
	r.publishActions = p.saved.actions
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
	defer func() { clearKeys(files) }()
	r.resumed = map[int]*resumeFile{}
	var undeclared []*fileState
	next := p.next
	acked := 0
	for i := range sources {
		src := &sources[i]
		sf := p.byTarget[src.Target]
		if sf == nil {
			index, planned := p.saved.sources[src.Target]
			if !planned {
				index.Index = next
				next++
			}
			f := &fileState{
				index:   index.Index,
				src:     src,
				planned: planned,
			}
			files = append(files, f)
			undeclared = append(undeclared, f)
			continue
		}
		_, planned := p.saved.sources[src.Target]
		f := &fileState{
			index:   sf.rec.Index,
			src:     src,
			planned: planned,
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
				f.verified = true
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
	seal := func(digest [32]byte, size uint64) (FileKey, error) {
		return sealStoredFile(p.sessionKey, p.session, r.org, digest, size)
	}
	if err := r.transferContents(ctx, seal, files, undeclared); err != nil {
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
