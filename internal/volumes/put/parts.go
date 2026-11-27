package put

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

const (
	maxRenewals   = 3
	maxRenewParts = 128
	// maxPartRetryTime bounds how long a part may keep failing without moving data: no attempt
	// starts once it has passed since the end of the part's last failed attempt that sent more than
	// maxBufferedPartBytes, or since its first failure when none did. One attempt on a slow link can
	// take longer than the window, so a window that ran from the first failure would stop a part
	// after one or two connection drops. Attempts that send a lot and fail at once still stop at
	// maxUploadAttempts.
	maxPartRetryTime = 5 * time.Minute
	// maxBufferedPartBytes is how much of a part's body an attempt may hand to the connection without
	// proving that any of it left the machine. The transport counts the bytes the HTTP client reads,
	// and on a stalled link the client still fills its write buffer, the TLS record and the kernel's
	// send buffer before it blocks, so a stalled attempt reads up to their sum. Send buffer
	// autotuning stops at 4 MiB by default on Linux and macOS, and macOS caps any socket buffer at
	// 8 MiB; the client's own buffers add tens of KiB. 16 MiB leaves room for a host tuned above
	// those defaults. Restarting the window on any byte sent would let a stalled link retry until
	// maxUploadAttempts, about 40 minutes. A part no larger than this, the last part of a file,
	// never proves progress and keeps the window from its first failure.
	maxBufferedPartBytes = 16 << 20
)

// partResult is an acknowledged part: its ack, ciphertext CRC and length.
type partResult struct {
	Ack Secret
	CRC uint64
	Len int64
}

// fileState is one declared file on its way through the upload.
type fileState struct {
	index    int
	src      *Source
	key      FileKey
	id       string
	reused   bool
	verified bool
	planned  bool
	layout   volumeformat.Layout
	identity volumeformat.FileIdentity

	mu      sync.Mutex
	targets map[int]Target
	results []partResult
	pending atomic.Int64
	sent    atomic.Int64
}

// prepare takes an uploading file's approved layout and its first upload targets.
func (f *fileState) prepare(a *DeclarationAnswer) error {
	layout, err := checkLayout(f.src.Size, a.File.Layout)
	if err != nil {
		return err
	}
	if len(a.UploadTargets) == 0 {
		return fmt.Errorf("%w: the uploading file came without upload targets", ErrProtocol)
	}
	targets := make(map[int]Target, len(a.UploadTargets))
	for i := range a.UploadTargets {
		t := a.UploadTargets[i]
		if t.PartNumber < 1 || int64(t.PartNumber) > layout.PartCount {
			return fmt.Errorf("%w: an upload target is for part %d of %d", ErrProtocol, t.PartNumber, layout.PartCount)
		}
		targets[t.PartNumber] = t
	}
	f.layout = layout
	f.identity = f.key.Identity()
	f.targets = targets
	f.results = make([]partResult, layout.PartCount)
	f.pending.Store(layout.PartCount)
	return nil
}

func (f *fileState) acknowledgedBytes() int64 {
	var n int64
	for i := range f.results {
		n += f.results[i].Len
	}
	return n
}

// partJob is a part to send and the digest of its plaintext, taken when the file was hashed and
// journalled before the part goes out.
type partJob struct {
	file   *fileState
	part   int
	digest [32]byte
}

// failure keeps the first error of a group of goroutines and cancels the rest.
type failure struct {
	once sync.Once
	err  error
}

func (f *failure) set(err error, cancel context.CancelFunc) {
	f.once.Do(func() {
		f.err = err
		cancel()
	})
}

// uploadFiles sends the files' parts in parallel and completes each file after its last ack.
func (r *runner) uploadFiles(ctx context.Context, todo []*fileState) error {
	if len(todo) == 0 {
		return nil
	}
	var total int64
	for _, f := range todo {
		total += f.layout.CiphertextSize - f.acknowledgedBytes()
	}
	r.progress.begin("uploading", len(todo), total)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var fail failure
	// Unbuffered, so the reader stays at most one part ahead of the senders.
	jobs := make(chan partJob)
	var wg sync.WaitGroup
	for range r.parallel {
		wg.Go(func() {
			for j := range jobs {
				if err := r.transferPart(ctx, j); err != nil {
					fail.set(err, cancel)
				}
			}
		})
	}
	for _, f := range todo {
		if err := r.feedFile(ctx, f, jobs); err != nil {
			fail.set(err, cancel)
			break
		}
	}
	close(jobs)
	wg.Wait()
	r.progress.end()
	if fail.err == nil {
		for _, f := range todo {
			r.progress.reportFile(f.src.Target, f.sent.Load(), false)
		}
	}
	return fail.err
}

// feedFile journals each part's digest before the part is sent. The digests come from hashing, which
// read the file once and computed the file's digest from the same bytes, so feedFile reads nothing:
// the sender reads each part from the file and must find the bytes hashed for it, and a part whose
// bytes differ is never acknowledged. Before the last part it checks the file's size and times have
// not moved since hashing.
func (r *runner) feedFile(ctx context.Context, f *fileState, jobs chan<- partJob) error {
	last := int(f.layout.PartCount)
	if len(f.src.Parts) != last {
		return fileErr(f.src.Abs, fmt.Errorf("%d part digests for %d parts", len(f.src.Parts), last))
	}
	for part := 1; part <= last; part++ {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		_, n, err := f.layout.PlaintextRange(int64(part))
		if err != nil {
			return fileErr(f.src.Abs, err)
		}
		if part == last {
			if err := unchanged(f.src); err != nil {
				return err
			}
		}
		digest := f.src.Parts[part-1]
		send, err := r.resumePart(f, part, digest)
		if err != nil {
			return err
		}
		if !send {
			continue
		}
		if err := r.journal.logSync(partRecord{
			Type:   recordPart,
			Index:  f.index,
			Part:   part,
			SHA256: hex.EncodeToString(digest[:]),
			Length: n,
		}); err != nil {
			return err
		}
		select {
		case jobs <- partJob{
			file:   f,
			part:   part,
			digest: digest,
		}:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	return r.resumeVerified(ctx, f)
}

// partSource reads a part's plaintext from the file for one send and hashes it on the way. Where the
// part ends it returns io.EOF only when the bytes match the digest journalled for the part, and
// ErrSourceChanged otherwise. The sealer seals the part's last segment only after that io.EOF, so a
// part whose bytes changed since they were journalled stops short and is never acknowledged.
type partSource struct {
	r      *io.SectionReader
	h      hash.Hash
	digest [32]byte
	part   int
	end    int64 // the file offset where the part ends
	left   int64 // bytes of the part not yet read
	err    error // io.EOF once the part is read and matches, or what stopped it
}

func newPartSource(file io.ReaderAt, part int, offset, n int64, digest [32]byte) *partSource {
	return &partSource{
		r:      io.NewSectionReader(file, offset, n),
		h:      sha256.New(),
		digest: digest,
		part:   part,
		end:    offset + n,
		left:   n,
	}
}

func (s *partSource) Read(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.left == 0 {
		s.err = io.EOF
		if [32]byte(s.h.Sum(nil)) != s.digest {
			s.err = sourceChangedError{
				cause: fmt.Sprintf("part %d no longer matches what was hashed", s.part),
			}
		}
		return 0, s.err
	}
	n, err := s.r.Read(p[:min(int64(len(p)), s.left)])
	s.h.Write(p[:n])
	s.left -= int64(n)
	switch {
	case errors.Is(err, io.EOF) && s.left > 0:
		s.err = sourceChangedError{
			cause: fmt.Sprintf("the file is shorter than when it was hashed: part %d ends before byte %d", s.part, s.end),
		}
	case err != nil && !errors.Is(err, io.EOF):
		s.err = err
	}
	return n, s.err
}

// failure is the error that stopped the part's plaintext, or nil when none did.
func (s *partSource) failure() error {
	if errors.Is(s.err, io.EOF) {
		return nil
	}
	return s.err
}

// transferPart sends one part, records its ack and completes the file after its last part.
func (r *runner) transferPart(ctx context.Context, j partJob) error {
	f := j.file
	res, err := r.sendPart(ctx, f, j.part, j.digest)
	if err != nil {
		// A changed part names itself.
		if _, ok := errors.AsType[sourceChangedError](err); !ok {
			err = fmt.Errorf("part %d: %w", j.part, err)
		}
		return fileErr(f.src.Abs, err)
	}
	f.results[j.part-1] = res
	r.progress.addBytes(res.Len)
	if err := r.journal.logAck(ackRecord{
		Type:      recordAck,
		Index:     f.index,
		Part:      j.part,
		Ack:       string(res.Ack),
		CRC64NVME: fmt.Sprintf("%016x", res.CRC),
		Length:    res.Len,
	}); err != nil {
		return err
	}
	if f.pending.Add(-1) == 0 {
		return r.completeFile(ctx, f)
	}
	return nil
}

// sendPart seals and sends one part, streaming its plaintext from the file: every attempt reads the
// part again from its offset and seals it under a fresh salt, with a renewed token when it expired.
// Bytes that differ from digest stop the attempt before the part's last segment and end the send.
func (r *runner) sendPart(ctx context.Context, f *fileState, part int, digest [32]byte) (partResult, error) {
	offset, n, err := f.layout.PlaintextRange(int64(part))
	if err != nil {
		return partResult{}, err
	}
	file, err := r.openSource(f.src.Abs)
	if err != nil {
		return partResult{}, fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	defer func() { _ = file.Close() }()
	var failed Secret
	// The retry window limits when another attempt can start. It starts at the first failure and
	// again after every failed attempt that sent more than maxBufferedPartBytes.
	retryCtx := ctx
	var stopRetryTimer context.CancelFunc
	defer func() {
		if stopRetryTimer != nil {
			stopRetryTimer()
		}
	}()
	attempts, renewals := 0, 0
	for {
		if cause := context.Cause(retryCtx); cause != nil {
			return partResult{}, cause
		}
		target, err := r.target(retryCtx, f, part, failed)
		if err != nil {
			return partResult{}, err
		}
		sealer, err := r.newPartSealer(f.key.DataKey(), f.identity, f.layout, int64(part))
		if err != nil {
			return partResult{}, fmt.Errorf("seal the part: %w", err)
		}
		src := newPartSource(file, part, offset, n, digest)
		body, err := sealer.StreamReader(src)
		if err != nil {
			return partResult{}, fmt.Errorf("seal the part: %w", err)
		}
		crc := volumeformat.NewCRC64NVME()
		if cause := context.Cause(retryCtx); cause != nil {
			return partResult{}, cause
		}
		traffic := &partTraffic{
			file:     f,
			progress: r.progress,
		}
		ack, err := r.tr.PutPart(ctx, target, io.TeeReader(body, io.MultiWriter(crc, traffic)), sealer.Len())
		if srcErr := src.failure(); srcErr != nil {
			return partResult{}, srcErr
		}
		if err == nil {
			return partResult{
				Ack: ack,
				CRC: crc.Sum64(),
				Len: sealer.Len(),
			}, nil
		}
		if ctx.Err() != nil {
			return partResult{}, context.Cause(ctx)
		}
		if stopRetryTimer == nil || traffic.sent.Load() > cmp.Or(r.bufferedPartBytes, maxBufferedPartBytes) {
			if stopRetryTimer != nil {
				stopRetryTimer()
			}
			retryWindow, cancel := context.WithTimeoutCause(ctx, maxPartRetryTime, errUploadRetriesExhausted)
			stopRetryTimer = cancel
			retryCtx = retryWindow
		}
		tokenExpired := errors.Is(err, ErrPartTokenExpired)
		switch {
		case retryCtx.Err() != nil:
			return partResult{}, errors.Join(context.Cause(retryCtx), err)
		case tokenExpired:
			if renewals++; renewals > maxRenewals {
				return partResult{}, err
			}
			failed = target.Token
		case !retryable(err):
			return partResult{}, err
		default:
			if attempts++; attempts >= maxUploadAttempts {
				return partResult{}, fmt.Errorf("%w: %w", errUploadRetriesExhausted, err)
			}
		}
		if !tokenExpired {
			if err := r.sleep(retryCtx, uploadBackoff(attempts)); err != nil {
				return partResult{}, err
			}
		}
	}
}

// partTraffic counts ciphertext consumed by the transport on one attempt, and adds it to the file's
// and the upload's totals.
type partTraffic struct {
	file     *fileState
	progress *progress
	sent     atomic.Int64
}

func (p *partTraffic) Write(b []byte) (int, error) {
	p.sent.Add(int64(len(b)))
	p.file.sent.Add(int64(len(b)))
	p.progress.addSent(int64(len(b)))
	return len(b), nil
}

// retryable reports whether the request can benefit from a retry.
func retryable(err error) bool {
	if errors.Is(err, ErrPartTokenInvalid) || errors.Is(err, ErrPartLengthMismatch) || errors.Is(err, ErrProtocol) {
		return false
	}
	if p, ok := errors.AsType[*ProblemError](err); ok {
		return p.Status >= http.StatusInternalServerError || p.Status == http.StatusTooManyRequests
	}
	if transport.IsAuthError(err) {
		return false
	}
	if e, ok := errors.AsType[*transport.RunwareError](err); ok && e.StatusCode != 0 {
		return e.StatusCode >= http.StatusInternalServerError || e.StatusCode == http.StatusTooManyRequests
	}
	return true
}

// target returns the part's upload target, renewing tokens when needed but never past the session deadline.
func (r *runner) target(ctx context.Context, f *fileState, part int, failed Secret) (Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := r.now()
	if !now.Before(r.expiresAt) {
		return Target{}, ErrSessionExpired
	}
	if t, ok := f.targets[part]; ok && t.ExpiresAt.After(now) && (failed == "" || t.Token != failed) {
		return t, nil
	}
	parts := make([]int, 0, maxRenewParts)
	for p := part; p <= int(f.layout.PartCount) && len(parts) < maxRenewParts; p++ {
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return Target{}, fmt.Errorf("%w: no upload target for part %d", ErrProtocol, part)
	}
	got, err := retryUploadAPI(ctx, r.sleep, func() ([]Target, error) {
		if !r.now().Before(r.expiresAt) {
			return nil, ErrSessionExpired
		}
		return r.api.RenewParts(ctx, r.ref, f.id, parts)
	})
	if err != nil {
		return Target{}, fmt.Errorf("renew the part tokens: %w", explain(err))
	}
	if len(got) != len(parts) {
		return Target{}, fmt.Errorf("%w: %d upload targets for %d parts", ErrProtocol, len(got), len(parts))
	}
	if !got[0].ExpiresAt.After(r.now()) {
		return Target{}, fmt.Errorf("%w: the renewed token for part %d already expired", ErrPartTokenExpired, part)
	}
	for i := range got {
		if got[i].PartNumber != parts[i] {
			return Target{}, fmt.Errorf("%w: an upload target is for part %d, not %d", ErrProtocol, got[i].PartNumber, parts[i])
		}
	}
	for i := range got {
		f.targets[parts[i]] = got[i]
	}
	return f.targets[part], nil
}

// completeFile submits the ciphertext CRC and every part's acknowledgement.
func (r *runner) completeFile(ctx context.Context, f *fileState) error {
	parts := make([]PartAck, len(f.results))
	for i := range f.results {
		parts[i] = PartAck{
			PartNumber: i + 1,
			Ack:        f.results[i].Ack,
		}
	}
	completion := FileCompletion{
		CRC64NVME: fmt.Sprintf("%016x", fileCRC(f.results)),
		Parts:     parts,
	}
	done, err := retryUploadAPIWithin(ctx, r.sleep, maxCompletionRetryTime, func() (UploadFile, error) {
		return r.api.CompleteFile(ctx, r.ref, f.id, completion)
	})
	if err != nil {
		return fileErr(f.src.Abs, fmt.Errorf("complete the file: %w", explain(err)))
	}
	if done.State != stateVerified {
		return fileErr(f.src.Abs, fmt.Errorf("%w: the completed file is %q, not verified", ErrProtocol, done.State))
	}
	f.key.Clear()
	if err := r.journal.logSync(completeRecord{
		Type:  recordComplete,
		Index: f.index,
	}); err != nil {
		return fileErr(f.src.Abs, err)
	}
	if err := r.journal.discardFileKeys(f.index); err != nil {
		return fileErr(f.src.Abs, err)
	}
	r.progress.fileDone()
	return nil
}
