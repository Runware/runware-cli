package keypair

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/binary"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/config"
)

const testSequence = 3

var testOtherOrg = [16]byte(uuid.MustParse("5d0a7c1e-8b3f-4f62-a9d4-3e6b1c2f7a80"))

type fieldEdit func(fields [][]byte) [][]byte

func setField(index int, value []byte) fieldEdit {
	return func(fields [][]byte) [][]byte {
		fields[index] = value
		return fields
	}
}

func dropField(index int) fieldEdit {
	return func(fields [][]byte) [][]byte { return slices.Delete(fields, index, index+1) }
}

func addField(value []byte) fieldEdit {
	return func(fields [][]byte) [][]byte { return append(fields, value) }
}

func baseSpec(v *vectors) statementSpec {
	return statementSpec{
		version:   testVersion,
		org:       testOrg,
		keyPair:   testKeyPair,
		sequence:  testSequence,
		issuedAt:  testIssuedAt,
		notAfter:  testNotAfter,
		publicKey: v.publicKey,
	}
}

// TestStatementEncodingMatchesVectors pins the test encoder, which the refusal cases sign
// with, to the statement bytes in the shared format vectors.
func TestStatementEncodingMatchesVectors(t *testing.T) {
	v := loadVectors(t)
	if got := join(v.spec().fields()); !bytes.Equal(got, v.statement) {
		t.Fatalf("encoded statement differs from the vectors' (%d bytes, want %d)", len(got), len(v.statement))
	}
}

// TestVerifyStatementGolden accepts the statement and signature in the shared format
// vectors, and reads every field back from it.
func TestVerifyStatementGolden(t *testing.T) {
	v := loadVectors(t)
	verifier, dir := v.verifier(t, v.issuedAt.Add(time.Hour))

	st, err := verifier.Verify(v.statement, v.signature, v.org)
	if err != nil {
		t.Fatal(err)
	}
	if st.SigningKeyVersion != v.raw.Statement.SigningKeyVersion || st.Organization() != v.org || st.KeyPairID() != v.keyPair {
		t.Fatalf("statement ids differ from the vectors: %+v", st)
	}
	if st.Sequence != v.raw.Statement.Sequence || !st.IssuedAt.Equal(v.issuedAt) || !st.NotAfter.Equal(v.notAfter) {
		t.Fatalf("statement numbers differ from the vectors: %+v", st)
	}
	if !bytes.Equal(st.publicKey, v.publicKey) {
		t.Fatal("statement public key differs from the vectors'")
	}
	want := map[string]uint64{orgKey(v.org): v.raw.Statement.Sequence}
	if got := readMarks(t, dir); !maps.Equal(got, want) {
		t.Fatalf("recorded %v, want %v", got, want)
	}
}

// TestVerifiedKeyIsCopiedFromTheCaller: the key a verified statement carries is not the
// caller's buffer, so reusing the buffer Verify read cannot change what Open seals to.
func TestVerifiedKeyIsCopiedFromTheCaller(t *testing.T) {
	v := loadVectors(t)
	verifier, _ := v.verifier(t, v.issuedAt.Add(time.Hour))
	statement := slices.Clone(v.statement)

	st, err := verifier.Verify(statement, v.signature, v.org)
	if err != nil {
		t.Fatal(err)
	}
	clear(statement)

	if !bytes.Equal(st.publicKey, v.publicKey) || st.Organization() != v.org || st.KeyPairID() != v.keyPair {
		t.Fatal("the verified key pair changed with the caller's buffer")
	}
}

// TestVerifyStatementGoldenRefusesAnyChangedByte: the vectors' "any byte of encoded or
// signature changed" case, for every byte.
func TestVerifyStatementGoldenRefusesAnyChangedByte(t *testing.T) {
	v := loadVectors(t)
	verifier, dir := v.verifier(t, v.issuedAt.Add(time.Hour))

	for i := range v.statement {
		changed := slices.Clone(v.statement)
		changed[i] ^= 1
		if _, err := verifier.Verify(changed, v.signature, v.org); !errors.Is(err, ErrStatement) {
			t.Fatalf("statement byte %d changed: error = %v, want ErrStatement", i, err)
		}
	}
	for i := range v.signature {
		changed := slices.Clone(v.signature)
		changed[i] ^= 1
		if _, err := verifier.Verify(v.statement, changed, v.org); !errors.Is(err, ErrStatement) {
			t.Fatalf("signature byte %d changed: error = %v, want ErrStatement", i, err)
		}
	}
	if marks := readMarks(t, dir); marks != nil {
		t.Fatalf("refused statements recorded %v", marks)
	}
}

type refusalCase struct {
	name     string
	reason   string
	edit     fieldEdit
	suffix   []byte
	raw      []byte
	signWith *ecdsa.PrivateKey
	tamper   bool
	unsigned bool
	org      [16]byte
	now      time.Time
	mark     uint64
}

// TestVerifyStatementRefuses: a statement with an unknown signing key, label or KEM, for
// another organization, at or past its not-after, or below the recorded sequence is
// refused, names the failing check, and records nothing.
func TestVerifyStatementRefuses(t *testing.T) {
	const missingVersion = "signing key version is missing"
	v := loadVectors(t)
	other := newReleaseKey(t)
	cases := []refusalCase{
		{
			name:   "empty statement",
			reason: missingVersion,
			raw:    []byte{},
		},
		{
			name:   "one field",
			reason: missingVersion,
			raw:    join(baseSpec(v).fields()[:1]),
		},
		{
			name:   "missing signing key version",
			reason: missingVersion,
			raw:    join(baseSpec(v).fields()[:fieldVersion]),
		},
		{
			name:   "unknown signing key version",
			reason: "unknown signing key version 2",
			edit:   setField(fieldVersion, binary.BigEndian.AppendUint32(nil, 2)),
		},
		{
			name:   "signing key version of the wrong size",
			reason: missingVersion,
			edit:   setField(fieldVersion, []byte{0, 0, 1}),
		},
		{
			name:     "signed by a key that is not a release key",
			reason:   "signature does not verify",
			signWith: other,
		},
		{
			name:   "changed after signing",
			reason: "signature does not verify",
			tamper: true,
		},
		{
			name:     "no signature",
			reason:   "signature does not verify",
			unsigned: true,
		},
		{
			name:   "unknown label",
			reason: "label",
			edit:   setField(fieldLabel, []byte("runware-volumes-v1/other")),
		},
		{
			name:   "unknown KEM identifier",
			reason: "KEM identifier",
			edit:   setField(fieldKEM, []byte("hpke-kem-0x0001")),
		},
		{
			name:   "a field missing",
			reason: "field count",
			edit:   dropField(fieldPublicKey),
		},
		{
			name:   "a field added",
			reason: "field count",
			edit:   addField([]byte{0}),
		},
		{
			name:   "organization of the wrong size",
			reason: "organization is 15 bytes",
			edit:   setField(fieldOrganization, make([]byte, 15)),
		},
		{
			name:   "key pair of the wrong size",
			reason: "key pair is 17 bytes",
			edit:   setField(fieldKeyPair, make([]byte, 17)),
		},
		{
			name:   "sequence of the wrong size",
			reason: "sequence is 7 bytes",
			edit:   setField(fieldSequence, make([]byte, 7)),
		},
		{
			name:   "issued-at of the wrong size",
			reason: "issued-at is 9 bytes",
			edit:   setField(fieldIssuedAt, make([]byte, 9)),
		},
		{
			name:   "not-after of the wrong size",
			reason: "not-after is 7 bytes",
			edit:   setField(fieldNotAfter, make([]byte, 7)),
		},
		{
			name:   "public key too short",
			reason: "public key is 1215 bytes",
			edit:   setField(fieldPublicKey, make([]byte, 1215)),
		},
		{
			name:   "public key too long",
			reason: "public key is 1217 bytes",
			edit:   setField(fieldPublicKey, make([]byte, 1217)),
		},
		{
			name:   "trailing byte",
			reason: "truncated field length",
			suffix: []byte{0},
		},
		{
			name:   "length prefix cut short",
			reason: "truncated field length",
			raw:    []byte{0, 1},
		},
		{
			name:   "field longer than the statement",
			reason: "field 1 runs past the end",
			raw:    []byte{0xff, 0xff, 0xff, 0xff, 0},
		},
		{
			name:   "another organization",
			reason: "organization is not the configured one",
			org:    testOtherOrg,
		},
		{
			name:   "now is the not-after",
			reason: "at or past its not-after",
			now:    testNotAfter,
		},
		{
			name:   "now is past the not-after",
			reason: "at or past its not-after",
			now:    testNotAfter.Add(time.Second),
		},
		{
			name:   "sequence below the recorded one",
			reason: "sequence 3 is below the highest accepted, 4",
			mark:   testSequence + 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := newReleaseKey(t)
			now := testNow
			if !tc.now.IsZero() {
				now = tc.now
			}
			verifier, dir := newTestVerifier(t, testVersion, &key.PublicKey, now)
			wantMarks := map[string]uint64{}
			if tc.mark > 0 {
				if err := verifier.seq.accept(testOrg, tc.mark); err != nil {
					t.Fatal(err)
				}
				wantMarks[orgKey(testOrg)] = tc.mark
			}

			statement, signature := tc.raw, []byte(nil)
			if statement == nil {
				fields := baseSpec(v).fields()
				if tc.edit != nil {
					fields = tc.edit(fields)
				}
				statement = append(join(fields), tc.suffix...)
				signer := key
				if tc.signWith != nil {
					signer = tc.signWith
				}
				signature = sign(t, signer, statement)
				if tc.tamper {
					statement[len(statement)-1] ^= 1
				}
				if tc.unsigned {
					signature = nil
				}
			}
			org := testOrg
			if tc.org != ([16]byte{}) {
				org = tc.org
			}

			st, err := verifier.Verify(statement, signature, org)
			if !errors.Is(err, ErrStatement) {
				t.Fatalf("error = %v, want ErrStatement", err)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %q, want it to name %q", err, tc.reason)
			}
			if st.publicKey != nil || st.Sequence != 0 || st.Organization() != ([16]byte{}) {
				t.Fatalf("a refused statement returned %+v", st)
			}
			if got := readMarks(t, dir); !maps.Equal(got, wantMarks) {
				t.Fatalf("recorded %v, want %v", got, wantMarks)
			}
		})
	}
}

func TestExpiredStatementShowsBothTimesInUTC(t *testing.T) {
	v := loadVectors(t)
	now := v.notAfter.Add(time.Second)
	verifier, _ := v.verifier(t, now.In(time.FixedZone("local", 2*60*60)))

	_, err := verifier.Verify(v.statement, v.signature, v.org)
	if !errors.Is(err, ErrStatement) {
		t.Fatalf("error = %v, want ErrStatement", err)
	}
	for _, clock := range []time.Time{v.notAfter, now} {
		want := clock.Format(time.RFC3339)
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, missing %s", err, want)
		}
	}
}

type acceptCase struct {
	name      string
	now       time.Time
	mark      uint64
	otherMark uint64
}

// TestVerifyStatementAccepts keeps the refusals honest: the boundary statements are accepted
// and the sequence is recorded, unchanged for a weekly re-sign.
func TestVerifyStatementAccepts(t *testing.T) {
	v := loadVectors(t)
	cases := []acceptCase{
		{
			name: "the first statement for the organization",
		},
		{
			name: "one second before the not-after",
			now:  testNotAfter.Add(-time.Second),
		},
		{
			name: "the sequence already recorded, as after a re-sign",
			mark: testSequence,
		},
		{
			name: "a higher sequence than the recorded one",
			mark: testSequence - 1,
		},
		{
			name:      "another organization's higher sequence",
			otherMark: 99,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := newReleaseKey(t)
			now := testNow
			if !tc.now.IsZero() {
				now = tc.now
			}
			verifier, dir := newTestVerifier(t, testVersion, &key.PublicKey, now)
			want := map[string]uint64{orgKey(testOrg): testSequence}
			if tc.mark > 0 {
				if err := verifier.seq.accept(testOrg, tc.mark); err != nil {
					t.Fatal(err)
				}
			}
			if tc.otherMark > 0 {
				if err := verifier.seq.accept(testOtherOrg, tc.otherMark); err != nil {
					t.Fatal(err)
				}
				want[orgKey(testOtherOrg)] = tc.otherMark
			}

			statement := join(baseSpec(v).fields())
			st, err := verifier.Verify(statement, sign(t, key, statement), testOrg)
			if err != nil {
				t.Fatal(err)
			}
			if st.SigningKeyVersion != testVersion || st.Organization() != testOrg || st.KeyPairID() != testKeyPair || st.Sequence != testSequence {
				t.Fatalf("accepted %+v", st)
			}
			if !st.IssuedAt.Equal(testIssuedAt) || !st.NotAfter.Equal(testNotAfter) || !bytes.Equal(st.publicKey, v.publicKey) {
				t.Fatalf("accepted %+v", st)
			}
			if got := readMarks(t, dir); !maps.Equal(got, want) {
				t.Fatalf("recorded %v, want %v", got, want)
			}
		})
	}
}

type rolloverCase struct {
	name    string
	version uint32
	signer  *ecdsa.PrivateKey
	refused bool
}

// TestVerifyChecksTheKeyOfTheStatementsVersion: with the current and next release keys loaded,
// a statement verifies under the key its version names and under no other, so a rollover
// accepts the next key's statements and never swaps the two keys.
func TestVerifyChecksTheKeyOfTheStatementsVersion(t *testing.T) {
	v := loadVectors(t)
	current, next := newReleaseKey(t), newReleaseKey(t)
	keys := map[uint32]*ecdsa.PublicKey{
		testVersion:     &current.PublicKey,
		testNextVersion: &next.PublicKey,
	}
	cases := []rolloverCase{
		{
			name:    "the current version signed by the current key",
			version: testVersion,
			signer:  current,
		},
		{
			name:    "the next version signed by the next key",
			version: testNextVersion,
			signer:  next,
		},
		{
			name:    "the next version signed by the current key",
			version: testNextVersion,
			signer:  current,
			refused: true,
		},
		{
			name:    "the current version signed by the next key",
			version: testVersion,
			signer:  next,
			refused: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verifier, dir := newVerifierWithKeys(t, keys, testNow)
			spec := baseSpec(v)
			spec.version = tc.version
			statement := join(spec.fields())

			st, err := verifier.Verify(statement, sign(t, tc.signer, statement), testOrg)

			marks := readMarks(t, dir)
			if tc.refused {
				if !errors.Is(err, ErrStatement) || !strings.Contains(err.Error(), "signature does not verify") {
					t.Fatalf("error = %v, want ErrStatement for a bad signature", err)
				}
				if marks != nil {
					t.Fatalf("a refused statement recorded %v", marks)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if st.SigningKeyVersion != tc.version {
				t.Fatalf("accepted signing key version %d, want %d", st.SigningKeyVersion, tc.version)
			}
			if want := map[string]uint64{orgKey(testOrg): testSequence}; !maps.Equal(marks, want) {
				t.Fatalf("recorded %v, want %v", marks, want)
			}
		})
	}
}

// TestUnknownPlatformRefusesEverything: a platform this release holds no key for, shipped or
// not, refuses even a statement that is validly signed.
func TestUnknownPlatformRefusesEverything(t *testing.T) {
	v := loadVectors(t)
	for _, base := range []string{
		config.DefaultServerlessBaseURL,
		"https://api.example.com",
		"https://example.com",
		"http://localhost:8080",
		"",
	} {
		t.Run(base, func(t *testing.T) {
			dir := t.TempDir()
			verifier, err := NewVerifier(base, dir)
			if err != nil {
				t.Fatal(err)
			}
			_, err = verifier.Verify(v.statement, v.signature, v.org)
			if !errors.Is(err, ErrStatement) || !strings.Contains(err.Error(), "no verification keys") {
				t.Fatalf("error = %v, want ErrStatement naming the missing keys", err)
			}
			if marks := readMarks(t, dir); marks != nil {
				t.Fatalf("recorded %v", marks)
			}
		})
	}
}

func TestNewVerifierNeedsAConfigDirectory(t *testing.T) {
	if _, err := NewVerifier(config.DefaultServerlessBaseURL, ""); err == nil {
		t.Fatal("NewVerifier accepted an empty config directory")
	}
}

func TestNewVerifierAcceptsAStatementSignedByAPlatformKey(t *testing.T) {
	original := verificationKeys
	t.Cleanup(func() { verificationKeys = original })
	signer := newReleaseKey(t)
	verificationKeys = map[string]map[uint32]string{
		testHost: {
			testVersion: pemOf(t, &signer.PublicKey),
		},
	}

	dir := t.TempDir()
	verifier, err := NewVerifier("https://"+testHost, dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	spec := baseSpec(loadVectors(t))
	spec.issuedAt = now.Add(-time.Minute)
	spec.notAfter = now.Add(time.Hour)
	raw := join(spec.fields())
	if _, err := verifier.Verify(raw, sign(t, signer, raw), spec.org); err != nil {
		t.Fatal(err)
	}
	if mark := readMarks(t, dir)[orgKey(spec.org)]; mark != spec.sequence {
		t.Fatalf("recorded sequence = %d, want %d", mark, spec.sequence)
	}
}

// TestNewVerifierKeysTheRecordByPlatformHost: sequences are recorded under the host that
// selects the key set, however the base URL spells it.
func TestNewVerifierKeysTheRecordByPlatformHost(t *testing.T) {
	for _, base := range []string{
		config.DefaultServerlessBaseURL,
		config.DefaultServerlessBaseURL + ".",
	} {
		verifier, err := NewVerifier(base, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if verifier.seq.host != productionHost {
			t.Fatalf("%s: record host = %q, want %q", base, verifier.seq.host, productionHost)
		}
	}
}
