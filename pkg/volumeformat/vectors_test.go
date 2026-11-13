package volumeformat

import (
	"bytes"
	"crypto/hpke"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The golden vectors are testdata/format-vectors.json, the format's shared vector
// file, whose output an independent TypeScript implementation also matched.
// Only its two about notes differ. Every consumer of the format runs them.

type vectorFile struct {
	Constants vectorConstants `json:"constants"`
	KeyPair   vectorKeyPair   `json:"keyPair"`
	Session   vectorSession   `json:"session"`
	Files     []vectorCase    `json:"files"`
	Negatives []vectorNeg     `json:"negatives"`
}

type vectorConstants struct {
	KEMIdentifier         string `json:"kemIdentifier"`
	MaxParts              int64  `json:"maxParts"`
	MinSegmentsPerPart    int64  `json:"minSegmentsPerPart"`
	SaltBytes             int64  `json:"saltBytes"`
	SegmentBytes          int64  `json:"segmentBytes"`
	SegmentPlaintextBytes int64  `json:"segmentPlaintextBytes"`
}

type vectorKeyPair struct {
	Organization     string `json:"organization"`
	KeyPairID        string `json:"keyPairId"`
	DeriveKeyPairIKM string `json:"deriveKeyPairIkm"`
	PrivateKey       string `json:"privateKey"`
	PublicKey        string `json:"publicKey"`
}

type vectorSession struct {
	Organization  string `json:"organization"`
	KeyPairID     string `json:"keyPairId"`
	UploadID      string `json:"uploadId"`
	Encapsulation string `json:"encapsulation"`
	SessionKey    string `json:"sessionKey"`
}

type vectorCase struct {
	Name      string          `json:"name"`
	Plaintext vectorPlaintext `json:"plaintext"`
	SHA256    string          `json:"sha256"`
	AttemptID string          `json:"attemptId"`
	DataKey   string          `json:"dataKey"`
	SealedKey string          `json:"sealedKey"`
	Layout    vectorLayout    `json:"layout"`
	Parts     []vectorPart    `json:"parts"`
	CRC64NVME string          `json:"crc64nvme"`
}

type vectorPlaintext struct {
	Size int64  `json:"size"`
	Byte string `json:"byte"`
}

type vectorLayout struct {
	Segments        int64 `json:"segments"`
	SegmentsPerPart int64 `json:"segmentsPerPart"`
	PartCount       int64 `json:"partCount"`
	PartSizeBytes   int64 `json:"partSizeBytes"`
	CiphertextBytes int64 `json:"ciphertextBytes"`
}

type vectorPart struct {
	PartNumber int64  `json:"partNumber"`
	Length     int64  `json:"length"`
	Salt       string `json:"salt"`
	SHA256     string `json:"sha256"`
	Ciphertext string `json:"ciphertext"`
}

type vectorNeg struct {
	Name     string `json:"name"`
	Mutation string `json:"mutation"`
	Expect   string `json:"expect"`
}

// loadedCase is a vector file case decoded and sealed: its identity, keys and parts.
type loadedCase struct {
	vectorCase
	plain    []byte
	identity FileIdentity
	dataKey  []byte
	sealed   []byte
	layout   Layout
	parts    [][]byte
}

type loadedVectors struct {
	file       vectorFile
	org        [16]byte
	keyPair    [16]byte
	session    [16]byte
	sessionKey []byte
	cases      map[string]*loadedCase
}

func loadVectors(t *testing.T) *loadedVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/format-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectorFile
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	lv := &loadedVectors{
		file:       v,
		org:        uuidBytes(t, v.KeyPair.Organization),
		keyPair:    uuidBytes(t, v.KeyPair.KeyPairID),
		session:    uuidBytes(t, v.Session.UploadID),
		sessionKey: unhex(t, v.Session.SessionKey),
		cases:      map[string]*loadedCase{},
	}
	for i := range v.Files {
		f := &v.Files[i]
		if want := fmt.Sprintf("plaintext[j] = (j + %d) mod 251", i); f.Plaintext.Byte != want {
			t.Fatalf("%s: plaintext rule %q, want %q", f.Name, f.Plaintext.Byte, want)
		}
		c := &loadedCase{
			vectorCase: *f,
			plain:      make([]byte, f.Plaintext.Size),
			dataKey:    unhex(t, f.DataKey),
			sealed:     unhex(t, f.SealedKey),
		}
		for j := range c.plain {
			c.plain[j] = byte((int64(j) + int64(i)) % 251)
		}
		c.identity = FileIdentity{
			Organization: lv.org,
			SHA256:       sha256.Sum256(c.plain),
			Size:         uint64(f.Plaintext.Size),
		}
		copy(c.identity.Attempt[:], unhex(t, f.AttemptID))
		if got := hex.EncodeToString(c.identity.SHA256[:]); got != f.SHA256 {
			t.Fatalf("%s: plaintext sha256 %s, want %s", f.Name, got, f.SHA256)
		}
		if c.layout, err = PlanLayout(f.Plaintext.Size); err != nil {
			t.Fatal(err)
		}
		for _, p := range f.Parts {
			var salt [SaltBytes]byte
			copy(salt[:], unhex(t, p.Salt))
			s, err := newPartSealerWithSalt(c.dataKey, c.identity, c.layout, p.PartNumber, salt)
			if err != nil {
				t.Fatal(err)
			}
			off, n, err := c.layout.PlaintextRange(p.PartNumber)
			if err != nil {
				t.Fatal(err)
			}
			part, err := s.Seal(c.plain[off : off+n])
			if err != nil {
				t.Fatal(err)
			}
			c.parts = append(c.parts, part)
		}
		lv.cases[f.Name] = c
	}
	return lv
}

func TestVectorConstants(t *testing.T) {
	c := loadVectors(t).file.Constants
	got := vectorConstants{
		KEMIdentifier:         KEMIdentifier,
		MaxParts:              MaxParts,
		MinSegmentsPerPart:    MinSegmentsPerPart,
		SaltBytes:             SaltBytes,
		SegmentBytes:          SegmentBytes,
		SegmentPlaintextBytes: SegmentPlaintextBytes,
	}
	if got != c {
		t.Fatalf("constants %+v, vectors %+v", got, c)
	}
}

// TestVectorKeyPair: X-Wing key generation as the provisioner does it, from the
// vectors' fixed input key material.
func TestVectorKeyPair(t *testing.T) {
	kp := loadVectors(t).file.KeyPair
	sk, err := hpke.MLKEM768X25519().DeriveKeyPair(unhex(t, kp.DeriveKeyPairIKM))
	if err != nil {
		t.Fatal(err)
	}
	priv, err := sk.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(priv) != kp.PrivateKey {
		t.Fatalf("private key %x", priv)
	}
	if hex.EncodeToString(sk.PublicKey().Bytes()) != kp.PublicKey {
		t.Fatal("public key differs")
	}
	if len(priv) != PrivateKeyBytes || len(sk.PublicKey().Bytes()) != PublicKeyBytes {
		t.Fatalf("key sizes %d and %d", len(priv), len(sk.PublicKey().Bytes()))
	}
}

// TestVectorSession: the decrypting side's decapsulation of the vectors' encapsulation gives
// the vectors' sealing key, and an encapsulation the CLI makes to the vectors' public
// key opens to the key the CLI derives. Encapsulation is randomized, so the CLI side
// is checked by that round trip against the fixed key pair.
func TestVectorSession(t *testing.T) {
	lv := loadVectors(t)
	kp, s := lv.file.KeyPair, lv.file.Session
	if s.Organization != kp.Organization || s.KeyPairID != kp.KeyPairID {
		t.Fatal("session and key pair disagree")
	}
	priv, enc := unhex(t, kp.PrivateKey), unhex(t, s.Encapsulation)
	if len(enc) != EncapsulationBytes {
		t.Fatalf("encapsulation is %d bytes", len(enc))
	}
	got, err := OpenSessionKey(priv, enc, lv.org, lv.keyPair, lv.session)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, lv.sessionKey) {
		t.Fatalf("session key %x", got)
	}

	sealer, err := Encapsulate(unhex(t, kp.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	cli, err := sealer.SessionKey(lv.org, lv.keyPair, lv.session)
	if err != nil {
		t.Fatal(err)
	}
	prep, err := OpenSessionKey(priv, sealer.Encapsulation(), lv.org, lv.keyPair, lv.session)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cli, prep) || len(cli) != sessionKeyBytes {
		t.Fatal("CLI and decrypting side derive different session keys")
	}
	other := lv.session
	other[0] ^= 1
	if wrong, _ := OpenSessionKey(priv, sealer.Encapsulation(), lv.org, lv.keyPair, other); bytes.Equal(wrong, cli) {
		t.Fatal("another session id derives the same key")
	}
}

// TestVectorFiles: layouts, sealed keys and every part's bytes, salt included, match
// the vectors, and every part opens both whole and streamed.
func TestVectorFiles(t *testing.T) {
	lv := loadVectors(t)
	for _, f := range lv.file.Files {
		c := lv.cases[f.Name]
		t.Run(f.Name, func(t *testing.T) {
			got := vectorLayout{
				Segments:        c.layout.Segments,
				SegmentsPerPart: c.layout.SegmentsPerPart,
				PartCount:       c.layout.PartCount,
				PartSizeBytes:   c.layout.PartSize,
				CiphertextBytes: c.layout.CiphertextSize,
			}
			if got != f.Layout {
				t.Fatalf("layout %+v, want %+v", got, f.Layout)
			}
			if int64(len(f.Parts)) != c.layout.PartCount {
				t.Fatalf("%d parts in the vectors, layout has %d", len(f.Parts), c.layout.PartCount)
			}

			key, err := UnwrapDataKey(lv.sessionKey, lv.session, c.identity, c.sealed)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(key, c.dataKey) {
				t.Fatal("sealed key opens to another data key")
			}
			var n [sealedKeyNonceBytes]byte
			copy(n[:], c.sealed)
			rewrapped, err := wrapDataKeyWithNonce(lv.sessionKey, lv.session, c.identity, c.dataKey, n)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(rewrapped, c.sealed) {
				t.Fatal("wrapping does not reproduce the sealed key")
			}

			crc := NewCRC64NVME()
			var total int64
			for i, p := range f.Parts {
				part := c.parts[i]
				sum := sha256.Sum256(part)
				if int64(len(part)) != p.Length || hex.EncodeToString(sum[:]) != p.SHA256 {
					t.Fatalf("part %d: %d bytes, sha256 %x; want %d, %s", p.PartNumber, len(part), sum, p.Length, p.SHA256)
				}
				if p.Ciphertext != "" && hex.EncodeToString(part) != p.Ciphertext {
					t.Fatalf("part %d ciphertext %x", p.PartNumber, part)
				}
				if want, _ := c.layout.PartLength(p.PartNumber); want != p.Length {
					t.Fatalf("part %d: PartLength %d", p.PartNumber, want)
				}
				_, _ = crc.Write(part) // a hash never fails a write
				total += int64(len(part))

				off, n, _ := c.layout.PlaintextRange(p.PartNumber)
				plain, err := OpenPart(c.dataKey, c.identity, c.layout, p.PartNumber, part)
				if err != nil || !bytes.Equal(plain, c.plain[off:off+n]) {
					t.Fatalf("part %d does not open: %v", p.PartNumber, err)
				}
				var streamed bytes.Buffer
				if _, err := OpenPartTo(&streamed, bytes.NewReader(part), c.dataKey, c.identity, c.layout, p.PartNumber); err != nil || !bytes.Equal(streamed.Bytes(), c.plain[off:off+n]) {
					t.Fatalf("part %d does not stream open: %v", p.PartNumber, err)
				}
			}
			// Segment-aligned ranged reads of the whole object, part by part.
			object := concat(c.parts...)
			var ranged []byte
			for p := int64(1); p <= c.layout.PartCount; p++ {
				start, _ := c.layout.PartOffset(p)
				o, err := NewPartOpener(c.dataKey, c.identity, c.layout, p, object[start:start+SaltBytes])
				if err != nil {
					t.Fatal(err)
				}
				count, _ := c.layout.SegmentsInPart(p)
				for i := range count {
					off, n, err := c.layout.SealedSegmentRange(p, i)
					if err != nil {
						t.Fatal(err)
					}
					if ranged, err = o.OpenSegment(ranged, i, object[off:off+n]); err != nil {
						t.Fatalf("part %d segment %d: %v", p, i, err)
					}
				}
			}
			if !bytes.Equal(ranged, c.plain) {
				t.Fatal("ranged reads do not reassemble the plaintext")
			}
			if total != c.layout.CiphertextSize {
				t.Fatalf("parts total %d bytes, layout says %d", total, c.layout.CiphertextSize)
			}
			if got := hex.EncodeToString(binary.BigEndian.AppendUint64(nil, crc.Sum64())); got != f.CRC64NVME {
				t.Fatalf("crc64nvme %s, want %s", got, f.CRC64NVME)
			}
		})
	}
}

// TestVectorNegatives reproduces every negative case of the vectors: each fails with
// ErrAuthentication, a whole part returns no plaintext, and a streamed open writes
// no byte of an unauthenticated segment.
func TestVectorNegatives(t *testing.T) {
	lv := loadVectors(t)
	two, tiny, parts := lv.cases["two-segments"], lv.cases["tiny"], lv.cases["two-parts"]
	p1 := two.parts[0]

	// openCase opens a part as part number n of file c, whole and streamed.
	type openCase struct {
		c    *loadedCase
		id   FileIdentity
		n    int64
		data []byte
	}
	partCases := map[string]openCase{
		"flip-bit": {
			c:    two,
			id:   two.identity,
			n:    1,
			data: mutate(p1, func(b []byte) []byte { b[100] ^= 1; return b }),
		},
		"swap-segments": {
			c:    two,
			id:   two.identity,
			n:    1,
			data: concat(p1[:SaltBytes], p1[SaltBytes+SegmentBytes:], p1[SaltBytes:SaltBytes+SegmentBytes]),
		},
		"truncate": {
			c:    two,
			id:   two.identity,
			n:    1,
			data: p1[:len(p1)-16],
		},
		"extend": {
			c:    two,
			id:   two.identity,
			n:    1,
			data: concat(p1, make([]byte, 16)),
		},
		"part-number": {
			c:    parts,
			id:   parts.identity,
			n:    2,
			data: parts.parts[0],
		},
		"replace-salt": {
			c:    two,
			id:   two.identity,
			n:    1,
			data: concat(tiny.parts[0][:SaltBytes], p1[SaltBytes:]),
		},
		"wrong-attempt": {
			c:    two,
			id:   withAttempt(two.identity, tiny.identity.Attempt),
			n:    1,
			data: p1,
		},
	}
	keyCases := map[string]func() error{
		"swap-sealed-key": func() error {
			_, err := UnwrapDataKey(lv.sessionKey, lv.session, two.identity, tiny.sealed)
			return err
		},
		"wrong-session": func() error {
			_, err := UnwrapDataKey(lv.sessionKey, lv.keyPair, tiny.identity, tiny.sealed)
			return err
		},
	}
	if len(lv.file.Negatives) != len(partCases)+len(keyCases) {
		t.Fatalf("%d negatives in the vectors, %d reproduced", len(lv.file.Negatives), len(partCases)+len(keyCases))
	}
	for _, neg := range lv.file.Negatives {
		t.Run(neg.Name, func(t *testing.T) {
			if fn, ok := keyCases[neg.Name]; ok {
				if err := fn(); !errors.Is(err, ErrAuthentication) {
					t.Fatalf("unwrap: %v", err)
				}
				return
			}
			oc, ok := partCases[neg.Name]
			if !ok {
				t.Fatalf("negative %q (%s) is not reproduced", neg.Name, neg.Mutation)
			}
			plain, err := OpenPart(oc.c.dataKey, oc.id, oc.c.layout, oc.n, oc.data)
			if !errors.Is(err, ErrAuthentication) || plain != nil {
				t.Fatalf("OpenPart: %v, %d plaintext bytes", err, len(plain))
			}
			var w bytes.Buffer
			n, err := OpenPartTo(&w, bytes.NewReader(oc.data), oc.c.dataKey, oc.id, oc.c.layout, oc.n)
			if !errors.Is(err, ErrAuthentication) {
				t.Fatalf("OpenPartTo: %v", err)
			}
			if !strings.Contains(neg.Expect, "no plaintext") {
				t.Fatalf("unexpected expectation %q", neg.Expect)
			}
			// The streamed open releases a segment only once it authenticates, so the
			// only plaintext it may have written is an authentic prefix of the part.
			off, _, _ := oc.c.layout.PlaintextRange(oc.n)
			if n != int64(w.Len()) || !bytes.HasPrefix(oc.c.plain[off:], w.Bytes()) {
				t.Fatalf("OpenPartTo wrote %d bytes that are not authentic plaintext", w.Len())
			}
			// Trailing bytes are found before the part's last segment is written.
			if neg.Name == "extend" && w.Len() != SegmentPlaintextBytes {
				t.Fatalf("extended part wrote %d bytes, want only its first segment", w.Len())
			}
		})
	}
}

func withAttempt(id FileIdentity, attempt [16]byte) FileIdentity {
	id.Attempt = attempt
	return id
}

func mutate(b []byte, f func([]byte) []byte) []byte { return f(append([]byte(nil), b...)) }

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func uuidBytes(t *testing.T, s string) [16]byte {
	t.Helper()
	var out [16]byte
	b := unhex(t, strings.ReplaceAll(s, "-", ""))
	if len(b) != 16 {
		t.Fatalf("uuid %q", s)
	}
	copy(out[:], b)
	return out
}
