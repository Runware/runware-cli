package put

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"

	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

const (
	maxPartAttempts = 5
	maxRenewals     = 3
	maxRenewParts   = 128
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
	layout   volumeformat.Layout
	identity volumeformat.FileIdentity

	mu      sync.Mutex
	targets map[int]Target
	results []partResult
	pending atomic.Int64
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

func (f *fileState) bytesSent() int64 {
	var n int64
	for i := range f.results {
		n += f.results[i].Len
	}
	return n
}

type partJob struct {
	file *fileState
	part int
	buf  *partBuffer
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

// partBuffer holds one part's plaintext from the read to the acknowledgement.
type partBuffer struct {
	data []byte
}

// bufferPool bounds the plaintext held in memory to a fixed number of part buffers.
type bufferPool struct {
	free chan *partBuffer
}

func newBufferPool(n int) *bufferPool {
	p := &bufferPool{free: make(chan *partBuffer, n)}
	for range n {
		p.free <- &partBuffer{}
	}
	return p
}

// get waits for a free buffer and sizes it to n bytes.
func (p *bufferPool) get(ctx context.Context, n int) (*partBuffer, error) {
	select {
	case b := <-p.free:
		if cap(b.data) < n {
			b.data = make([]byte, n)
		}
		b.data = b.data[:n]
		return b, nil
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

func (p *bufferPool) put(b *partBuffer) { p.free <- b }

// uploadFiles sends the files' parts in parallel and completes each file after its last ack.
func (r *runner) uploadFiles(ctx context.Context, todo []*fileState) error {
	if len(todo) == 0 {
		return nil
	}
	var total int64
	for _, f := range todo {
		total += f.layout.CiphertextSize
	}
	r.progress.begin("uploading", len(todo), total)
	defer r.progress.end()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var fail failure
	pool := newBufferPool(r.parallel + 1)
	jobs := make(chan partJob)
	var wg sync.WaitGroup
	for range r.parallel {
		wg.Go(func() {
			for j := range jobs {
				if err := r.transferPart(ctx, j); err != nil {
					fail.set(err, cancel)
				}
				pool.put(j.buf)
			}
		})
	}
	for _, f := range todo {
		if err := r.feedFile(ctx, f, pool, jobs); err != nil {
			fail.set(err, cancel)
			break
		}
	}
	close(jobs)
	wg.Wait()
	return fail.err
}

// feedFile reads a file part by part and checks its digest before the last part goes out.
func (r *runner) feedFile(ctx context.Context, f *fileState, pool *bufferPool, jobs chan<- partJob) error {
	file, err := os.Open(f.src.Abs)
	if err != nil {
		return fileErr(f.src.Abs, fmt.Errorf("%w: %w", ErrSourceChanged, err))
	}
	defer func() { _ = file.Close() }()
	whole := sha256.New()
	last := int(f.layout.PartCount)
	for part := 1; part <= last; part++ {
		offset, n, err := f.layout.PlaintextRange(int64(part))
		if err != nil {
			return fileErr(f.src.Abs, err)
		}
		buf, err := pool.get(ctx, int(n))
		if err != nil {
			return err
		}
		if err := readPart(file, offset, buf.data); err != nil {
			pool.put(buf)
			return fileErr(f.src.Abs, err)
		}
		whole.Write(buf.data)
		if part == last {
			if err := f.verifySource(whole.Sum(nil)); err != nil {
				pool.put(buf)
				return err
			}
		}
		digest := sha256.Sum256(buf.data)
		if err := r.journal.log(partRecord{
			Type:   recordPart,
			Index:  f.index,
			Part:   part,
			SHA256: hex.EncodeToString(digest[:]),
			Length: n,
		}); err != nil {
			pool.put(buf)
			return err
		}
		select {
		case jobs <- partJob{
			file: f,
			part: part,
			buf:  buf,
		}:
		case <-ctx.Done():
			pool.put(buf)
			return context.Cause(ctx)
		}
	}
	return nil
}

// readPart fills buf from the file at offset; a file that ends early has changed.
func readPart(file *os.File, offset int64, buf []byte) error {
	_, err := io.ReadFull(io.NewSectionReader(file, offset, int64(len(buf))), buf)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: it ended before byte %d", ErrSourceChanged, offset+int64(len(buf)))
	}
	return err
}

// verifySource checks the digest of what was read and the file's size and time since hashing.
func (f *fileState) verifySource(sum []byte) error {
	if !bytes.Equal(sum, f.src.SHA256[:]) {
		return fileErr(f.src.Abs, ErrSourceChanged)
	}
	return unchanged(f.src)
}

// transferPart sends one part, records its ack and completes the file after its last part.
func (r *runner) transferPart(ctx context.Context, j partJob) error {
	f := j.file
	res, err := r.sendPart(ctx, f, j.part, j.buf.data)
	if err != nil {
		return fileErr(f.src.Abs, fmt.Errorf("part %d: %w", j.part, err))
	}
	f.results[j.part-1] = res
	r.progress.addBytes(res.Len)
	if err := r.journal.log(ackRecord{
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

// sendPart seals and sends one part: under a fresh salt on retry, with a renewed token when it expired.
func (r *runner) sendPart(ctx context.Context, f *fileState, part int, plain []byte) (partResult, error) {
	var failed Secret
	attempts, renewals := 0, 0
	for {
		target, err := r.target(ctx, f, part, failed)
		if err != nil {
			return partResult{}, err
		}
		sealer, err := volumeformat.NewPartSealer(f.key.DataKey(), f.identity, f.layout, int64(part))
		if err != nil {
			return partResult{}, fmt.Errorf("seal the part: %w", err)
		}
		body, err := sealer.Reader(plain)
		if err != nil {
			return partResult{}, fmt.Errorf("seal the part: %w", err)
		}
		crc := volumeformat.NewCRC64NVME()
		ack, err := r.tr.PutPart(ctx, target, io.TeeReader(body, crc), sealer.Len())
		switch {
		case err == nil:
			return partResult{
				Ack: ack,
				CRC: crc.Sum64(),
				Len: sealer.Len(),
			}, nil
		case ctx.Err() != nil:
			return partResult{}, context.Cause(ctx)
		case errors.Is(err, ErrPartTokenExpired):
			if renewals++; renewals > maxRenewals {
				return partResult{}, err
			}
			failed = target.Token
		case !retryable(err):
			return partResult{}, err
		default:
			if attempts++; attempts >= maxPartAttempts {
				return partResult{}, fmt.Errorf("%w: %w", errUploadRetriesExhausted, err)
			}
			if err := r.sleep(ctx, backoff(attempts)); err != nil {
				return partResult{}, err
			}
		}
	}
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
	done, err := retryUploadAPI(ctx, r.sleep, func() (UploadFile, error) {
		return r.api.CompleteFile(ctx, r.ref, f.id, completion)
	})
	if err != nil {
		return fileErr(f.src.Abs, fmt.Errorf("complete the file: %w", explain(err)))
	}
	if done.State != stateVerified {
		return fileErr(f.src.Abs, fmt.Errorf("%w: the completed file is %q, not verified", ErrProtocol, done.State))
	}
	f.key.Clear()
	r.progress.fileDone()
	return r.journal.log(completeRecord{
		Type:  recordComplete,
		Index: f.index,
	})
}
