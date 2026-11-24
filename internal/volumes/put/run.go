package put

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

const (
	defaultParallelism  = 4
	maxParallelism      = 16
	maxDeclarationBatch = 100
	keyPairAttempts     = 5
	maxUploadAttempts   = 18
	maxUploadBackoff    = 30 * time.Second
	baseBackoff         = 500 * time.Millisecond
	abortTimeout        = 15 * time.Second
)

var errUploadRetriesExhausted = errors.New("the upload retry limit was reached")

// Options configures one put.
type Options struct {
	Volume       VolumeRef
	Organization string
	Paths        []string
	To           string // the Volume directory the paths land under; empty is the root
	ConfigDir    string // the CLI config directory, holding the hash cache and the resume state
	API          API
	Keys         Keys
	Transport    Transport
	Parallelism  int // parts in flight; zero means four, and sixteen is the most
	Progress     io.Writer
	ProgressTTY  bool // Progress is a terminal

	now           func() time.Time
	sleep         func(context.Context, time.Duration) error
	tick          <-chan time.Time
	onHash        func(path string)
	progressEvery time.Duration
}

// Run uploads the paths to the Volume.
func Run(ctx context.Context, o Options) (Summary, error) {
	r, err := newRunner(o)
	if err != nil {
		return Summary{}, err
	}
	return r.run(ctx)
}

type runner struct {
	o                   Options
	org                 [16]byte
	api                 API
	keys                Keys
	tr                  Transport
	now                 func() time.Time
	sleep               func(context.Context, time.Duration) error
	stateDir            string
	parallel            int
	progress            *progress
	ref                 UploadRef
	expiresAt           time.Time
	journal             *journal
	completionAttempted bool
}

func newRunner(o Options) (*runner, error) {
	org, err := parseOrganization(o.Organization)
	if err != nil {
		return nil, err
	}
	if o.ConfigDir == "" {
		return nil, errors.New("volume upload: the config directory is not set")
	}
	r := &runner{
		o:        o,
		org:      org,
		api:      o.API,
		keys:     o.Keys,
		tr:       o.Transport,
		now:      o.now,
		sleep:    o.sleep,
		stateDir: filepath.Join(o.ConfigDir, stateDirName),
		parallel: o.Parallelism,
	}
	if r.parallel <= 0 {
		r.parallel = defaultParallelism
	}
	r.parallel = min(r.parallel, maxParallelism)
	if r.now == nil {
		r.now = time.Now
	}
	if r.sleep == nil {
		r.sleep = sleepContext
	}
	r.progress = newProgress(o.Progress, o.ProgressTTY, o.progressEvery, r.now, o.tick)
	return r, nil
}

// parseOrganization reads the organization id or says where to set it.
func parseOrganization(s string) ([16]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return [16]byte{}, fmt.Errorf("%w: %s", ErrNoOrganization, organizationHelp())
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return [16]byte{}, fmt.Errorf("the configured organization id is not a UUID: %s", organizationHelp())
	}
	return id, nil
}

func organizationHelp() string {
	return fmt.Sprintf("set organization in %s or the %s environment variable, and copy the id from the Runware console (%s)",
		config.ConfigPath(), config.OrganizationFromEnvVar, config.GetDashboardURL())
}

func (r *runner) run(ctx context.Context) (Summary, error) {
	sources, err := Walk(r.o.Paths, r.o.To)
	if err != nil {
		return Summary{}, err
	}
	before, err := r.api.LatestFiles(ctx, r.o.Volume)
	if err != nil {
		return Summary{}, fmt.Errorf("read the Volume's files: %w", err)
	}
	sess, err := r.openKeys(ctx)
	if err != nil {
		return Summary{}, err
	}
	defer sess.Clear()
	cache := loadHashCache(r.stateDir)
	err = hashSources(ctx, sources, cache, r.progress, r.o.onHash)
	r.saveCache(cache)
	if err != nil {
		return Summary{}, err
	}
	sum, err := r.upload(ctx, sess, sources, before)
	if errors.Is(err, ErrSourceChanged) {
		if fe, ok := errors.AsType[*FileError](err); ok {
			cache.forget(fe.Path)
			r.saveCache(cache)
		}
	}
	return sum, err
}

func (r *runner) saveCache(c *hashCache) {
	if err := c.save(); err != nil {
		r.warn("the hash cache was not saved: %v", err)
	}
}

func (r *runner) warn(format string, args ...any) {
	if r.o.Progress != nil {
		_, _ = fmt.Fprintf(r.o.Progress, "warning: "+format+"\n", args...)
	}
}

// openKeys reads and verifies the key-pair statement, retrying while the first key pair is not ready.
func (r *runner) openKeys(ctx context.Context) (KeySession, error) {
	st, err := r.keyPairStatement(ctx)
	if err != nil {
		return nil, err
	}
	statement, err := base64.RawURLEncoding.DecodeString(st.Statement)
	if err != nil {
		return nil, fmt.Errorf("%w: the key-pair statement is not base64url", ErrProtocol)
	}
	signature, err := base64.RawURLEncoding.DecodeString(st.Signature)
	if err != nil {
		return nil, fmt.Errorf("%w: the key-pair signature is not base64url", ErrProtocol)
	}
	return r.keys.Open(statement, signature, r.org)
}

func (r *runner) keyPairStatement(ctx context.Context) (KeyPairStatement, error) {
	for attempt := 1; ; attempt++ {
		st, err := r.api.KeyPairStatement(ctx)
		if err == nil {
			return st, nil
		}
		if !errors.Is(err, ErrKeyPairNotReady) || attempt == keyPairAttempts {
			return KeyPairStatement{}, fmt.Errorf("read the organization's key pair: %w", err)
		}
		if err := r.sleep(ctx, backoff(attempt)); err != nil {
			return KeyPairStatement{}, err
		}
	}
}

// upload opens a session and keeps a journal while the upload is in progress.
func (r *runner) upload(ctx context.Context, sess KeySession, sources []Source, before FileList) (Summary, error) {
	up, err := r.api.OpenUpload(ctx, r.o.Volume, UploadOpen{
		KeyPairID:     uuid.UUID(sess.KeyPairID()).String(),
		Encapsulation: base64.RawURLEncoding.EncodeToString(sess.Encapsulation()),
	})
	if err != nil {
		return Summary{}, fmt.Errorf("open the upload session: %w", err)
	}
	id, err := uuid.Parse(up.ID)
	if err != nil {
		return Summary{}, fmt.Errorf("%w: the session id is not a UUID", ErrProtocol)
	}
	r.ref = UploadRef{
		Volume: r.o.Volume,
		ID:     id.String(),
	}
	r.expiresAt = up.ExpiresAt
	if err := r.begin(sess, id, up); err != nil {
		return Summary{}, r.abort(ctx, err)
	}
	sum, err := r.transfer(ctx, sess, sources, before)
	if err != nil {
		if cause := context.Cause(ctx); cause != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, cause)) {
			err = cause
		}
		// A fresh put is the next step unless the completion request was sent, which may have published the files.
		rerun := errors.Is(err, errUploadRetriesExhausted) && !r.completionAttempted
		err = r.abort(ctx, err)
		if rerun {
			err = fmt.Errorf("%w; run put again", err)
		}
		return Summary{}, err
	}
	if err := r.journal.remove(); err != nil {
		r.warn("%v", err)
	}
	return sum, nil
}

// begin binds the sealing key to the session and saves it before anything is declared.
func (r *runner) begin(sess KeySession, id uuid.UUID, up Upload) error {
	if err := sess.Bind(id); err != nil {
		return fmt.Errorf("derive the session sealing key: %w", err)
	}
	j, err := createJournal(r.stateDir, sessionRecord{
		Upload:       r.ref.ID,
		Volume:       r.o.Volume.Name,
		Environment:  r.o.Volume.Environment,
		Organization: uuid.UUID(r.org).String(),
		KeyPair:      uuid.UUID(sess.KeyPairID()).String(),
		SessionKey:   sess.Key(),
		CreatedAt:    up.CreatedAt,
		ExpiresAt:    up.ExpiresAt,
	})
	r.journal = j
	return err
}

// abort tries to close the session and removes the local journal even if the server is unreachable.
func (r *runner) abort(ctx context.Context, cause error) error {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()
	_, abortErr := retryUploadAPI(actx, r.sleep, func() (struct{}, error) {
		err := r.api.AbortUpload(actx, r.ref)
		if p, ok := errors.AsType[*ProblemError](err); ok && p.Slug == slugUploadNotOpen {
			if r.completionAttempted {
				cause = fmt.Errorf("%w (the upload may already have completed; check the Volume's latest files)", cause)
			}
			return struct{}{}, nil
		}
		return struct{}{}, err
	})
	if abortErr != nil {
		cause = &AbortFailedError{
			Cause:     cause,
			Abort:     abortErr,
			ExpiresAt: r.expiresAt,
		}
	}
	if r.journal != nil {
		if err := r.journal.remove(); err != nil {
			return errors.Join(cause, err)
		}
	}
	return cause
}

func (r *runner) transfer(ctx context.Context, sess KeySession, sources []Source, before FileList) (Summary, error) {
	files, err := r.declare(ctx, sess, sources)
	defer clearKeys(files)
	if err != nil {
		return Summary{}, err
	}
	var todo []*fileState
	for _, f := range files {
		if !f.reused {
			todo = append(todo, f)
		}
	}
	if err := r.uploadFiles(ctx, todo); err != nil {
		return Summary{}, err
	}
	for i := range sources {
		if err := unchanged(&sources[i]); err != nil {
			return Summary{}, err
		}
	}
	res, err := retryUploadAPI(ctx, r.sleep, func() (UploadResult, error) {
		r.completionAttempted = true
		return r.api.CompleteUpload(ctx, r.ref)
	})
	if err != nil {
		return Summary{}, fmt.Errorf("complete the upload: %w", explain(err))
	}
	return r.summarize(files, before, res), nil
}

func clearKeys(files []*fileState) {
	for _, f := range files {
		f.key.Clear()
	}
}

// declare seals a key per file, saves it and declares the files in batches.
func (r *runner) declare(ctx context.Context, sess KeySession, sources []Source) ([]*fileState, error) {
	files := make([]*fileState, 0, len(sources))
	for start := 0; start < len(sources); start += maxDeclarationBatch {
		end := min(start+maxDeclarationBatch, len(sources))
		batch := make([]*fileState, 0, end-start)
		decls := FileDeclarations{Files: make([]FileDeclaration, 0, end-start)}
		recs := make([]any, 0, end-start)
		for i := start; i < end; i++ {
			src := &sources[i]
			if err := unchanged(src); err != nil {
				return files, err
			}
			fk, err := sess.SealFile(src.SHA256, uint64(src.Size))
			if err != nil {
				return files, fileErr(src.Abs, fmt.Errorf("seal the data key: %w", err))
			}
			f := &fileState{
				index: i,
				src:   src,
				key:   fk,
			}
			files = append(files, f)
			batch = append(batch, f)
			attempt := fk.Attempt()
			attemptID := hex.EncodeToString(attempt[:])
			sealed := fk.Sealed()
			if err := r.journal.saveFileKeys(i, fileKeyRecord{
				DataKey:   fk.DataKey(),
				SealedKey: sealed,
				Attempt:   attemptID,
			}); err != nil {
				return files, fileErr(src.Abs, err)
			}
			digest := hex.EncodeToString(src.SHA256[:])
			decls.Files = append(decls.Files, FileDeclaration{
				Path:      src.Target,
				Mode:      src.Mode,
				Size:      src.Size,
				SHA256:    digest,
				SealedKey: base64.RawURLEncoding.EncodeToString(sealed),
				AttemptID: attemptID,
			})
			recs = append(recs, fileRecord{
				Type:   recordFile,
				Index:  i,
				Path:   src.Target,
				Mode:   src.Mode,
				Size:   src.Size,
				SHA256: digest,
			})
		}
		if err := r.journal.logSync(recs...); err != nil {
			return files, err
		}
		syncDir(r.journal.dir)
		got, err := retryUploadAPI(ctx, r.sleep, func() (DeclarationResult, error) {
			return r.api.DeclareFiles(ctx, r.ref, decls)
		})
		if err != nil {
			return files, explainDeclare(err, batch)
		}
		if err := r.accept(batch, decls.Files, got); err != nil {
			return files, err
		}
	}
	return files, nil
}

// accept reads a batch's answers: a reused file is done, an uploading file takes its layout and targets.
func (r *runner) accept(batch []*fileState, decls []FileDeclaration, got DeclarationResult) error {
	if len(got.Files) != len(batch) {
		return fmt.Errorf("%w: %d answers for %d declared files", ErrProtocol, len(got.Files), len(batch))
	}
	recs := make([]any, 0, len(batch))
	for i, f := range batch {
		a := &got.Files[i]
		if a.File.Path != decls[i].Path || a.File.ID == "" {
			return fileErr(f.src.Abs, fmt.Errorf("%w: the answer is for another file", ErrProtocol))
		}
		f.id = a.File.ID
		switch a.File.State {
		case stateReused:
			f.reused = true
			f.key.Clear()
		case stateUploading:
			if err := f.prepare(a); err != nil {
				return fileErr(f.src.Abs, err)
			}
		default:
			return fileErr(f.src.Abs, fmt.Errorf("%w: the file was declared as %q", ErrProtocol, a.File.State))
		}
		recs = append(recs, declaredRecord{
			Type:   recordDeclared,
			Index:  f.index,
			FileID: f.id,
			State:  a.File.State,
			Layout: a.File.Layout,
		})
	}
	if err := r.journal.logSync(recs...); err != nil {
		return err
	}
	for _, f := range batch {
		if f.reused {
			if err := r.journal.discardFileKeys(f.index); err != nil {
				return fileErr(f.src.Abs, err)
			}
		}
	}
	for _, f := range batch {
		if f.reused {
			if err := unchanged(f.src); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *runner) summarize(files []*fileState, before FileList, res UploadResult) Summary {
	existing := make(map[string]VolumeFile, len(before.Files))
	for _, file := range before.Files {
		existing[file.Path] = file
	}
	s := Summary{
		Volume:         r.o.Volume.Name,
		Environment:    cmp.Or(r.o.Volume.Environment, productionEnvironment),
		Changed:        res.Changed,
		ManifestSHA256: res.ManifestSHA256,
		Files:          make([]FileResult, 0, len(files)),
	}
	for _, f := range files {
		fr := FileResult{
			Path:      f.src.Target,
			Size:      f.src.Size,
			SHA256:    hex.EncodeToString(f.src.SHA256[:]),
			BytesSent: f.bytesSent(),
		}
		if f.reused {
			s.Reused++
		}
		switch old, found := existing[fr.Path]; {
		case !found:
			fr.Action = ActionAdded
			s.Added++
		case old.Mode != f.src.Mode || old.Size != fr.Size || old.SHA256 != fr.SHA256:
			fr.Action = ActionReplaced
			s.Replaced++
		default:
			fr.Action = ActionUnchanged
		}
		s.BytesSent += fr.BytesSent
		s.Files = append(s.Files, fr)
	}
	return s
}

// checkLayout accepts the approved layout when it is the format's layout for the size.
func checkLayout(size int64, l *PartLayout) (volumeformat.Layout, error) {
	want, err := volumeformat.PlanLayout(size)
	if err != nil {
		return volumeformat.Layout{}, err
	}
	if l == nil || l.SegmentBytes != volumeformat.SegmentBytes || l.SegmentPlaintextBytes != volumeformat.SegmentPlaintextBytes ||
		l.SegmentsPerPart != want.SegmentsPerPart || l.PartSizeBytes != want.PartSize ||
		l.PartCount != want.PartCount || l.CiphertextBytes != want.CiphertextSize {
		return volumeformat.Layout{}, fmt.Errorf("%w: the approved part layout is not the format's layout for %d bytes", ErrProtocol, size)
	}
	return want, nil
}

// explainDeclare names the file a refused declaration is about and the limit it went past.
func explainDeclare(err error, batch []*fileState) error {
	p, ok := errors.AsType[*ProblemError](err)
	if !ok {
		return fmt.Errorf("declare the files: %w", err)
	}
	cause := explain(err)
	if i, ok := pointerFile(p.Pointer); ok && i < len(batch) {
		return fileErr(batch[i].src.Abs, cause)
	}
	return fmt.Errorf("declare the files: %w", cause)
}

// explain turns a limit problem into a LimitError and leaves any other error as it is.
func explain(err error) error {
	if p, ok := errors.AsType[*ProblemError](err); ok && p.Slug == slugLimitExceeded && p.Limit != "" {
		return &LimitError{
			Limit:   p.Limit,
			Cap:     p.Cap,
			Problem: err,
		}
	}
	return err
}

// pointerFile reads the index in a JSON pointer such as /files/3/size.
func pointerFile(pointer string) (int, bool) {
	rest, ok := strings.CutPrefix(pointer, "/files/")
	if !ok {
		return 0, false
	}
	index, _, _ := strings.Cut(rest, "/")
	i, err := strconv.Atoi(index)
	return i, err == nil && i >= 0
}

func retryUploadAPI[T any](ctx context.Context, sleep func(context.Context, time.Duration) error, call func() (T, error)) (T, error) {
	var empty T
	var lastErr error
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return empty, errors.Join(context.Cause(ctx), lastErr)
		}
		res, err := call()
		if err == nil {
			return res, nil
		}
		if ctx.Err() != nil {
			if errors.Is(err, ctx.Err()) || errors.Is(err, context.Cause(ctx)) {
				err = lastErr
			}
			return empty, errors.Join(context.Cause(ctx), err)
		}
		if !retryable(err) || errors.Is(err, context.Canceled) || errors.Is(err, ErrSessionExpired) {
			return empty, err
		}
		if attempt == maxUploadAttempts {
			return empty, fmt.Errorf("%w: %w", errUploadRetriesExhausted, err)
		}
		lastErr = err
		if err := sleep(ctx, uploadBackoff(attempt)); err != nil {
			return empty, errors.Join(err, lastErr)
		}
	}
}

func backoff(attempt int) time.Duration {
	return baseBackoff << (attempt - 1)
}

func uploadBackoff(attempt int) time.Duration {
	window := min(backoff(attempt), maxUploadBackoff)
	return window/2 + rand.N(window/2) //nolint:gosec // Retry jitter is not security sensitive.
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
