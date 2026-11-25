package put

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
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
	// maxCompletionRetryTime bounds the retries of a file or session
	// completion: no attempt starts once it has passed since the first one
	// started. Each attempt may take minutes, so the attempt count alone
	// would let a stalled connection hold put for over an hour.
	maxCompletionRetryTime = 15 * time.Minute
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
	ProgressTTY  bool         // Progress is a terminal
	Logger       *slog.Logger // debug events, such as a resumed or discarded session; nil logs nothing

	now           func() time.Time
	sleep         func(context.Context, time.Duration) error
	tick          <-chan time.Time
	onHash        func(path string)
	progressEvery time.Duration
	newSealer     sealerFactory
}

// Run uploads the paths to the Volume, aborting the session on failure and keeping it open for a resume when put is interrupted.
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
	invocation          string
	parallel            int
	progress            *progress
	ref                 UploadRef
	expiresAt           time.Time
	journal             *journal
	completionAttempted bool
	publishActions      map[int]string
	resumed             map[int]*resumeFile // resumed files by journal index; nil unless a saved session is resumed
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
		o:          o,
		org:        org,
		api:        o.API,
		keys:       o.Keys,
		tr:         o.Transport,
		now:        o.now,
		sleep:      o.sleep,
		stateDir:   filepath.Join(o.ConfigDir, stateDirName),
		invocation: invocationDigest(o.Paths, o.To),
		parallel:   o.Parallelism,
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

func invocationDigest(paths []string, destination string) string {
	roots := make([]string, len(paths))
	for i, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		roots[i] = abs
	}
	slices.Sort(roots)
	destination, _ = cleanTarget(destination)
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%q %q", destination, roots)
	return hex.EncodeToString(h.Sum(nil))
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
	saved := r.sweep()
	defer func() {
		for _, s := range saved {
			s.release()
		}
	}()
	if err != nil {
		if errors.Is(err, ErrNoFiles) || errors.Is(err, os.ErrNotExist) {
			for _, s := range saved {
				if s.session.Invocation == r.invocation {
					r.abortSaved(ctx, s, err.Error())
				}
			}
		}
		return Summary{}, err
	}
	candidates := r.resumeCandidates(saved, sources)
	before, err := r.api.LatestFiles(ctx, r.o.Volume)
	if err != nil {
		return Summary{}, fmt.Errorf("read the Volume's files: %w", err)
	}
	if resumed, sum, err := r.resume(ctx, candidates, sources, before); resumed {
		return sum, err
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

// upload opens a session and keeps a journal while the upload is in progress; an interrupt leaves both for a resume.
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
	err = r.begin(sess, id, up)
	defer r.journal.release()
	if err != nil {
		return Summary{}, r.abort(ctx, err)
	}
	sum, err := r.transfer(ctx, sess, sources, before)
	if err != nil {
		return Summary{}, r.fail(ctx, err)
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
		Invocation:   r.invocation,
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
				if errors.Is(cause, ErrSourceChanged) {
					cause = uncertainSourceChange(cause)
				} else {
					cause = fmt.Errorf("%w (the upload may already have completed; check the Volume's latest files)", cause)
				}
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
	files := make([]*fileState, len(sources))
	for i := range sources {
		files[i] = &fileState{
			index: i,
			src:   &sources[i],
		}
	}
	defer clearKeys(files)
	if err := r.transferContents(ctx, sess.SealFile, files, files); err != nil {
		return Summary{}, err
	}
	return r.publish(ctx, files, sources, before)
}

// publish re-checks every source and completes the session, fresh or resumed.
func (r *runner) publish(ctx context.Context, files []*fileState, sources []Source, before FileList) (Summary, error) {
	for i := range sources {
		if err := unchanged(&sources[i]); err != nil {
			return Summary{}, err
		}
	}
	recovered := r.completionAttempted
	if len(r.publishActions) != len(files) {
		r.publishActions = compareFiles(files, before)
	}
	if !recovered {
		records := make([]any, 0, len(files)+1)
		for _, f := range files {
			records = append(records, actionRecord{
				Type:   recordAction,
				Index:  f.index,
				Action: r.publishActions[f.index],
			})
		}
		records = append(records, recordType{Type: recordPublish})
		if err := r.journal.logSync(records...); err != nil {
			return Summary{}, err
		}
	}
	res, err := r.completeUpload(ctx, sources)
	if err != nil {
		return Summary{}, fmt.Errorf("complete the upload: %w", explain(err))
	}
	return r.summarize(files, res, recovered), nil
}

func clearKeys(files []*fileState) {
	for _, f := range files {
		if f.key != nil {
			f.key.Clear()
		}
	}
}

// accept reads a batch's answers: a reused file is done, an uploading file takes its layout and targets.
func (r *runner) accept(batch []*fileState, decls []FileDeclaration, got DeclarationResult) error {
	if len(got.Files) != len(batch) {
		return fmt.Errorf("%w: %d answers for %d declared files", ErrProtocol, len(got.Files), len(batch))
	}
	recs := make([]any, 0, len(batch))
	for i, f := range batch {
		a := &got.Files[i]
		if a.File.Path != decls[i].Path || a.File.ID == "" || (f.id != "" && f.id != a.File.ID) {
			return fileErr(f.src.Abs, fmt.Errorf("%w: the answer is for another file", ErrProtocol))
		}
		f.id = a.File.ID
		switch a.File.State {
		case stateReused:
			f.reused = true
			f.key.Clear()
		case stateVerified:
			if r.resumed == nil {
				return fileErr(f.src.Abs, fmt.Errorf("%w: a new file was already verified", ErrProtocol))
			}
			f.verified = true
			f.key.Clear()
		case stateUploading:
			if f.targets == nil {
				if err := f.prepare(a); err != nil {
					return fileErr(f.src.Abs, err)
				}
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
		if f.reused || f.verified {
			if err := r.journal.discardFileKeys(f.index); err != nil {
				return fileErr(f.src.Abs, err)
			}
		}
	}
	for _, f := range batch {
		if f.reused || f.verified {
			if err := unchanged(f.src); err != nil {
				return err
			}
		}
	}
	return nil
}

func compareFiles(files []*fileState, before FileList) map[int]string {
	existing := make(map[string]VolumeFile, len(before.Files))
	for _, file := range before.Files {
		existing[file.Path] = file
	}
	actions := make(map[int]string, len(files))
	for _, f := range files {
		src := f.src
		switch old, found := existing[src.Target]; {
		case !found:
			actions[f.index] = ActionAdded
		case old.Mode != src.Mode || old.Size != src.Size || old.SHA256 != hex.EncodeToString(src.SHA256[:]):
			actions[f.index] = ActionReplaced
		default:
			actions[f.index] = ActionUnchanged
		}
	}
	return actions
}

func (r *runner) summarize(files []*fileState, res UploadResult, recovered bool) Summary {
	s := Summary{
		Volume:         r.o.Volume.Name,
		Environment:    cmp.Or(r.o.Volume.Environment, productionEnvironment),
		Changed:        res.Changed,
		ManifestSHA256: res.ManifestSHA256,
		recovered:      recovered,
		Files:          make([]FileResult, 0, len(files)),
	}
	for _, f := range files {
		fr := FileResult{
			Path:      f.src.Target,
			Action:    cmp.Or(r.publishActions[f.index], ActionUnchanged),
			Size:      f.src.Size,
			SHA256:    hex.EncodeToString(f.src.SHA256[:]),
			BytesSent: f.sent.Load(),
			Reused:    f.reused,
		}
		if f.reused {
			s.Reused++
		}
		switch fr.Action {
		case ActionAdded:
			s.Added++
		case ActionReplaced:
			s.Replaced++
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

// completeUpload applies the session, retrying within maxCompletionRetryTime.
func (r *runner) completeUpload(ctx context.Context, sources []Source) (UploadResult, error) {
	return retryUploadAPIWithin(ctx, r.sleep, maxCompletionRetryTime, func() (UploadResult, error) {
		for i := range sources {
			if err := unchanged(&sources[i]); err != nil {
				return UploadResult{}, err
			}
		}
		r.completionAttempted = true
		return r.api.CompleteUpload(ctx, r.ref)
	})
}

func retryUploadAPI[T any](ctx context.Context, sleep func(context.Context, time.Duration) error, call func() (T, error)) (T, error) {
	return retryUploadAPIWithin(ctx, sleep, 0, call)
}

// retryUploadAPIWithin is retryUploadAPI that, when window is not zero, starts
// no attempt once window has passed since the first one started.
func retryUploadAPIWithin[T any](ctx context.Context, sleep func(context.Context, time.Duration) error, window time.Duration, call func() (T, error)) (T, error) {
	var empty T
	var lastErr error
	start := time.Now()
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return empty, errors.Join(context.Cause(ctx), lastErr)
		}
		if attempt > 1 && window > 0 && time.Since(start) >= window {
			return empty, fmt.Errorf("%w: %w", errUploadRetriesExhausted, lastErr)
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
		if !retryable(err) || errors.Is(err, context.Canceled) || errors.Is(err, ErrSessionExpired) || errors.Is(err, ErrSourceChanged) {
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
