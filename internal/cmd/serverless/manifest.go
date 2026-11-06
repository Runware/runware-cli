package serverless

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// manifestFile is the project file a packaged source carries at its root. It
// records the deploy options a person would otherwise retype as flags, so a
// client can offer the deploy without someone first reading how.
const manifestFile = "runware.yaml"

// manifestVersion is the only configVersion this CLI understands. The field
// exists so a later format can be recognized rather than misread, and it is
// counted separately from container.yaml's own version.
const manifestVersion = 1

// manifestAppIDPattern is the server's AppId rule, which also fixes the length
// at 6 to 30 characters. It is checked here because with --from the id comes
// out of a file the person deploying did not write, and the API's own answer
// names the field without naming the rule it broke.
var manifestAppIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)

// deployManifest is runware.yaml. Every field maps to a deploy flag, so the
// file adds no behavior of its own: reading it and passing the flags by hand
// produce the same application.
//
// It holds no secret values. `secrets` names what the app needs, and the
// secret has to already exist in the environment.
type deployManifest struct {
	ConfigVersion int               `yaml:"configVersion"`
	ID            string            `yaml:"id"`
	Name          string            `yaml:"name"`
	Source        manifestSource    `yaml:"source"`
	Compute       manifestCompute   `yaml:"compute"`
	Build         manifestBuild     `yaml:"build"`
	Volumes       []string          `yaml:"volumes"`
	EnvVars       map[string]string `yaml:"envVars"`
	Secrets       []manifestSecret  `yaml:"secrets"`
}

type manifestSource struct {
	Type  string `yaml:"type"`
	Entry string `yaml:"entry"`
}

type manifestCompute struct {
	GpuType             string `yaml:"gpuType"`
	FallbackGpuType     string `yaml:"fallbackGpuType"`
	GpusPerWorker       *int32 `yaml:"gpusPerWorker"`
	MinWorkers          *int32 `yaml:"minWorkers"`
	MaxWorkers          *int32 `yaml:"maxWorkers"`
	MinAvailableWorkers *int32 `yaml:"minAvailableWorkers"`
	AvailableWorkersPct *int32 `yaml:"availableWorkersPct"`
	IdleTTL             *int32 `yaml:"idleTtl"`
	ScalingDelay        *int32 `yaml:"scalingDelay"`
}

type manifestBuild struct {
	BaseImage    string   `yaml:"baseImage"`
	Requirements []string `yaml:"requirements"`
}

type manifestSecret struct {
	Name       string `yaml:"name"`
	EnvVarName string `yaml:"envVarName"`
}

// validate checks the two fields as the two fields they are. Rendering them
// into `--secret` and parsing that back would read a name holding an `=` as a
// name and an alias, so a manifest naming one secret would attach a different
// one under a variable nobody wrote.
func (s manifestSecret) validate() error {
	if s.Name == "" || !envNamePattern.MatchString(s.Name) {
		return fmt.Errorf("secret name %q must be POSIX-style: letters, digits and underscore, not starting with a digit", s.Name)
	}
	if s.EnvVarName != "" && !envNamePattern.MatchString(s.EnvVarName) {
		return fmt.Errorf("secret %s names the environment variable %q, which must be POSIX-style", s.Name, s.EnvVarName)
	}
	return nil
}

// flag renders the secret the way --secret takes it.
func (s manifestSecret) flag() string {
	if s.EnvVarName == "" {
		return s.Name
	}
	return s.Name + "=" + s.EnvVarName
}

// readDeployManifest reads and validates the manifest at the root of dir.
//
// Unknown keys are rejected rather than ignored. A manifest is hand-written,
// and a misspelled key that silently does nothing surfaces later as a missing
// required flag, which says nothing about the typo that caused it.
func readDeployManifest(dir string) (deployManifest, error) {
	path := filepath.Join(dir, manifestFile)

	raw, err := os.ReadFile(path) //nolint:gosec // the path is manifestFile under a directory we chose
	if err != nil {
		if os.IsNotExist(err) {
			return deployManifest{}, fmt.Errorf("no %s at the root of the source; there is nothing to deploy it with", manifestFile)
		}
		return deployManifest{}, fmt.Errorf("read %s: %w", manifestFile, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)

	var manifest deployManifest
	if err := decoder.Decode(&manifest); err != nil {
		if errors.Is(err, io.EOF) {
			return deployManifest{}, fmt.Errorf("%s is empty", manifestFile)
		}
		return deployManifest{}, fmt.Errorf("read %s: %w", manifestFile, err)
	}

	// A file holding more than one document would otherwise be read down to its
	// first `---` and the rest dropped without a word, which is the silent
	// omission KnownFields exists to prevent one key at a time.
	if err := decoder.Decode(new(deployManifest)); !errors.Is(err, io.EOF) {
		return deployManifest{}, fmt.Errorf("%s holds more than one document, and only the first would be deployed", manifestFile)
	}

	if err := manifest.validate(); err != nil {
		return deployManifest{}, fmt.Errorf("%s: %w", manifestFile, err)
	}
	return manifest, nil
}

func (m deployManifest) validate() error {
	if m.ConfigVersion != manifestVersion {
		if m.ConfigVersion == 0 {
			return fmt.Errorf("configVersion is required and must be %d", manifestVersion)
		}
		return fmt.Errorf("configVersion %d is not supported; this CLI understands %d", m.ConfigVersion, manifestVersion)
	}
	if m.ID == "" {
		return fmt.Errorf("id is required")
	}
	if !manifestAppIDPattern.MatchString(m.ID) {
		return fmt.Errorf(
			"id %q is not a valid application id (6 to 30 characters, lowercase letters, digits and hyphens, starting with a letter and ending with a letter or digit)",
			m.ID,
		)
	}

	switch m.Source.Type {
	case string(serverlessapi.AppSourceTypeCode):
		if m.Source.Entry == "" {
			return fmt.Errorf("source.entry is required for a %s app", serverlessapi.AppSourceTypeCode)
		}
		if err := validateManifestEntry(m.Source.Entry); err != nil {
			return err
		}
	case string(serverlessapi.AppSourceTypeContainer):
		if m.Source.Entry != "" {
			return fmt.Errorf("source.entry applies to a %s app only", serverlessapi.AppSourceTypeCode)
		}
		if m.Build.BaseImage != "" || len(m.Build.Requirements) > 0 {
			return fmt.Errorf("build applies to a %s app only", serverlessapi.AppSourceTypeCode)
		}
	case "":
		return fmt.Errorf("source.type is required (%s or %s)", serverlessapi.AppSourceTypeCode, serverlessapi.AppSourceTypeContainer)
	default:
		return fmt.Errorf("source.type %q is not %s or %s", m.Source.Type, serverlessapi.AppSourceTypeCode, serverlessapi.AppSourceTypeContainer)
	}

	for _, secret := range m.Secrets {
		if err := secret.validate(); err != nil {
			return err
		}
	}
	if m.Compute.GpusPerWorker != nil {
		if err := validateGPUsPerWorker(*m.Compute.GpusPerWorker); err != nil {
			return fmt.Errorf("compute.gpusPerWorker: %w", err)
		}
	}
	if m.Compute.AvailableWorkersPct != nil {
		if err := validateAvailableWorkersPct(*m.Compute.AvailableWorkersPct); err != nil {
			return fmt.Errorf("compute.availableWorkersPct: %w", err)
		}
	}
	if m.Name != "" {
		if err := validateAppName(m.Name); err != nil {
			return fmt.Errorf("name: %w", err)
		}
	}
	return nil
}

// validateManifestEntry keeps the entry file inside the unpacked source. The
// deploy resolves it against a directory this command created, and a path that
// climbs out of it would read the machine running the deploy.
func validateManifestEntry(entry string) error {
	if filepath.IsAbs(entry) || strings.HasPrefix(entry, "/") {
		return fmt.Errorf("source.entry %q must be relative to the source root", entry)
	}
	// The same test safeArchivePath uses, because this is the same class of
	// path: one an archive we did not write chose. A hand-written prefix check
	// misses a Windows drive-relative name, which is neither absolute nor
	// prefixed with "..", and the two guards must not disagree about that.
	if !filepath.IsLocal(filepath.Clean(filepath.FromSlash(entry))) {
		return fmt.Errorf("source.entry %q must stay inside the source root", entry)
	}
	return nil
}

// localSource says how the deploy reads the unpacked archive: the arguments
// the command would have been given, and the directory flags that go with
// them. A code app names its entry file, a container app names its directory.
func (m deployManifest) localSource(dir string) (args []string, srcDir, containerDir string) {
	if m.Source.Type == string(serverlessapi.AppSourceTypeContainer) {
		return nil, "", dir
	}
	return []string{m.Source.Entry}, dir, ""
}

// secretFlags renders the secrets the way --secret takes them.
func (m deployManifest) secretFlags() []string {
	out := make([]string, 0, len(m.Secrets))
	for _, secret := range m.Secrets {
		out = append(out, secret.flag())
	}
	return out
}

// manifestString is the manifest's value for a flag the command line left
// alone. The four helpers below are the whole merge rule: a flag the person
// passed always wins, and an absent manifest field changes nothing.
func manifestString(cmd *cobra.Command, name, passed, fromManifest string) string {
	if cmd.Flags().Changed(name) || fromManifest == "" {
		return passed
	}
	return fromManifest
}

func manifestInt32(cmd *cobra.Command, name string, passed int32, fromManifest *int32) int32 {
	if cmd.Flags().Changed(name) || fromManifest == nil {
		return passed
	}
	return *fromManifest
}

// manifestInt32Ptr keeps optionalInt32Ptr's contract: nil when neither the
// command line nor the manifest says anything, so the field stays out of the
// request and the server's own default applies.
func manifestInt32Ptr(cmd *cobra.Command, name string, passed int32, fromManifest *int32) *int32 {
	if cmd.Flags().Changed(name) {
		return &passed
	}
	return fromManifest
}

func manifestStringPtr(cmd *cobra.Command, name, passed, fromManifest string) *string {
	if cmd.Flags().Changed(name) {
		return &passed
	}
	return optionalStringPtr(fromManifest)
}
