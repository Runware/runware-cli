package serverless

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"gopkg.in/yaml.v3"
)

const (
	colDescription    = "Description"
	colSize           = "Size"
	colFiles          = "Files"
	colFilesDigest    = "Files digest"
	colUsedBy         = "Used by"
	colRollbackFor    = "Rollback of"
	colKind           = "Kind"
	colEffect         = "On delete"
	colPath           = "Path"
	colMode           = "Mode"
	colSHA256         = "SHA-256"
	colVolume         = "Volume"
	colDeleted        = "Deleted"
	colLostRollback   = "Lost rollback"
	colAvailable      = "Enabled"
	colMaxStored      = "Max stored"
	colMaxFileSize    = "Max file size"
	colMaxFiles       = "Max files per Volume"
	colMaxMounted     = "Max mounted per app version"
	colUploadDeadline = "Upload session lifetime"

	// shortIDLength is how much of an id or digest a list table shows.
	shortIDLength = 8
	// effectBlocks and effectLosesRollback say what a reference does to a
	// delete.
	effectBlocks        = "blocks it"
	effectLosesRollback = "loses rollback"
	// deletedLogically is what a deleted Volume's table says about its bytes.
	deletedLogically = "yes; stored bytes are reclaimed later"
)

// apiYAML is v as YAML with the API's field names. yaml.v3 would otherwise
// lowercase the generated types' Go field names, which carry JSON tags only.
func apiYAML(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	blockStyle(doc.Content[0])
	return doc.Content[0], nil
}

// blockStyle drops the flow and quoting styles that decoding JSON leaves on
// every node, so the encoder writes ordinary block YAML and quotes only the
// strings that need it.
func blockStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		blockStyle(c)
	}
}

// shortID is the start of an id or digest, for a table.
func shortID(s string) string {
	if len(s) <= shortIDLength {
		return s
	}
	return s[:shortIDLength]
}

// volumeResult is one Volume. Its table adds the uses of the Volume when show
// read them; JSON and YAML print the API's Volume object only.
type volumeResult struct {
	volume serverlessapi.Volume
	// withUses adds the uses to the table.
	withUses bool
	// references are the Volume's uses, or nil when they could not be read.
	references *serverlessapi.VolumeReferences
	// referencesErr says why the uses could not be read.
	referencesErr string
}

func (r volumeResult) Headers() []string {
	return []string{colField, colValue}
}

func (r volumeResult) Rows() [][]any {
	v := r.volume
	digest := noValue
	if v.ManifestSha256 != nil {
		digest = *v.ManifestSha256
	}
	rows := [][]any{
		{colID, v.Id.String()},
		{colName, v.Name},
		{colEnvironment, v.Environment},
		{colDescription, formatOptionalString(v.Description)},
		{colStatus, string(v.Status)},
		{colSize, humanBytes(v.LogicalBytes)},
		{colFiles, v.FileCount},
		{colFilesDigest, digest},
		{colCreated, v.CreatedAt.Format(time.RFC3339)},
		{colUpdated, v.UpdatedAt.Format(time.RFC3339)},
	}
	if !r.withUses {
		return rows
	}
	usedBy, rollbackOf := r.referencesErr, r.referencesErr
	if r.references != nil {
		usedBy = describeOrNone(r.references.Blocking)
		rollbackOf = describeOrNone(r.references.RollbackTargets)
	}
	return append(rows,
		[]any{colUsedBy, usedBy},
		[]any{colRollbackFor, rollbackOf},
	)
}

func (r volumeResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.volume)
}

func (r volumeResult) MarshalYAML() (any, error) {
	return apiYAML(r.volume)
}

func describeOrNone(refs []serverlessapi.VolumeReference) string {
	if len(refs) == 0 {
		return noValue
	}
	return describeVolumeReferences(refs)
}

// volumesResult is every Volume of an environment. JSON and YAML print it as
// the API's page, holding every Volume and no next cursor.
type volumesResult []serverlessapi.Volume

func (r volumesResult) Headers() []string {
	return []string{colID, colName, colSize, colFiles, colDescription, colUpdated}
}

func (r volumesResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		v := &r[i]
		rows[i] = []any{
			shortID(v.Id.String()),
			v.Name,
			humanBytes(v.LogicalBytes),
			v.FileCount,
			formatOptionalString(v.Description),
			v.UpdatedAt.Format(time.RFC3339),
		}
	}
	return rows
}

func (r volumesResult) page() serverlessapi.Page[serverlessapi.Volume] {
	return serverlessapi.Page[serverlessapi.Volume]{
		Data: r,
	}
}

func (r volumesResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.page())
}

func (r volumesResult) MarshalYAML() (any, error) {
	return apiYAML(r.page())
}

// volumeReferencesResult is the uses of a Volume: one row per app version.
type volumeReferencesResult serverlessapi.VolumeReferences

func (r volumeReferencesResult) Headers() []string {
	return []string{colApp, colVersion, colKind, colEffect}
}

func (r volumeReferencesResult) Rows() [][]any {
	rows := make([][]any, 0, len(r.Blocking)+len(r.RollbackTargets))
	for _, ref := range r.Blocking {
		rows = append(rows, []any{ref.AppId, ref.AppVersionNumber, volumeReferenceKind(ref.Kind), effectBlocks})
	}
	for _, ref := range r.RollbackTargets {
		rows = append(rows, []any{ref.AppId, ref.AppVersionNumber, volumeReferenceKind(ref.Kind), effectLosesRollback})
	}
	return rows
}

func (r volumeReferencesResult) api() serverlessapi.VolumeReferences {
	return serverlessapi.VolumeReferences{
		Blocking:        nonNilReferences(r.Blocking),
		RollbackTargets: nonNilReferences(r.RollbackTargets),
	}
}

func (r volumeReferencesResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.api())
}

func (r volumeReferencesResult) MarshalYAML() (any, error) {
	return apiYAML(r.api())
}

// volumeFilesResult is every one of a Volume's latest files. JSON and YAML
// print it as the API's page, with the digest of the file list it came from.
type volumeFilesResult serverlessapi.VolumeFilePage

func (r volumeFilesResult) Headers() []string {
	return []string{colPath, colSize, colMode, colSHA256}
}

func (r volumeFilesResult) Rows() [][]any {
	rows := make([][]any, len(r.Data))
	for i, f := range r.Data {
		rows[i] = []any{
			f.Path,
			humanBytes(f.Size),
			fmt.Sprintf("%#o", f.Mode),
			shortID(f.Sha256),
		}
	}
	return rows
}

func (r volumeFilesResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(serverlessapi.VolumeFilePage(r))
}

func (r volumeFilesResult) MarshalYAML() (any, error) {
	return apiYAML(serverlessapi.VolumeFilePage(r))
}

// volumeDeletedResult reports a deleted Volume and the app versions that lost
// their rollback with it.
type volumeDeletedResult struct {
	Name         string                          `json:"name"         yaml:"name"`
	Environment  string                          `json:"environment"  yaml:"environment"`
	LostRollback []serverlessapi.VolumeReference `json:"lostRollback" yaml:"lostRollback"`
}

func (r volumeDeletedResult) Headers() []string {
	return []string{colField, colValue}
}

func (r volumeDeletedResult) Rows() [][]any {
	return [][]any{
		{colVolume, r.Name},
		{colEnvironment, r.Environment},
		{colDeleted, deletedLogically},
		{colLostRollback, describeOrNone(r.LostRollback)},
	}
}

func (r volumeDeletedResult) MarshalYAML() (any, error) {
	type plain volumeDeletedResult
	return apiYAML(plain(r))
}

// volumeSettingsResult is the organization's Shared Volume access and limits.
type volumeSettingsResult serverlessapi.VolumeSettings

func (r volumeSettingsResult) Headers() []string {
	return []string{colField, colValue}
}

func (r volumeSettingsResult) Rows() [][]any {
	l := r.Limits
	return [][]any{
		{colAvailable, formatYesNo(r.Available)},
		{colMaxStored, humanBytes(l.MaxStoredBytes)},
		{colMaxFileSize, humanBytes(l.MaxFileBytes)},
		{colMaxFiles, strconv.FormatInt(l.MaxFilesPerVolume, 10)},
		{colMaxMounted, humanBytes(l.MaxMountedBytesPerAppVersion)},
		{colUploadDeadline, (time.Duration(l.UploadSessionTtlSeconds) * time.Second).String()},
	}
}

func (r volumeSettingsResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(serverlessapi.VolumeSettings(r))
}

func (r volumeSettingsResult) MarshalYAML() (any, error) {
	return apiYAML(serverlessapi.VolumeSettings(r))
}
