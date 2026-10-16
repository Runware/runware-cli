package installtest_test

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/runware/runware-cli/internal/installtest"
)

// goreleaserConfigPath is the repo's GoReleaser config, relative to this
// package.
var goreleaserConfigPath = filepath.Join("..", "..", ".goreleaser.yaml")

// goreleaserConfig is the part of the GoReleaser config that sets the names
// of the release files.
type goreleaserConfig struct {
	Builds   []goreleaserBuild   `yaml:"builds"`
	Archives []goreleaserArchive `yaml:"archives"`
	Checksum goreleaserChecksum  `yaml:"checksum"`
}

// goreleaserBuild is a builds entry: the binary and the targets it is built
// for.
type goreleaserBuild struct {
	Binary string   `yaml:"binary"`
	Goos   []string `yaml:"goos"`
	Goarch []string `yaml:"goarch"`
}

// goreleaserArchive is an archives entry. Format is the deprecated form of
// Formats, which GoReleaser still reads.
type goreleaserArchive struct {
	NameTemplate    string                     `yaml:"name_template"`
	Format          string                     `yaml:"format"`
	Formats         []string                   `yaml:"formats"`
	FormatOverrides []goreleaserFormatOverride `yaml:"format_overrides"`
}

// goreleaserFormatOverride sets the archive formats of one OS. Format is the
// deprecated form of Formats.
type goreleaserFormatOverride struct {
	Goos    string   `yaml:"goos"`
	Format  string   `yaml:"format"`
	Formats []string `yaml:"formats"`
}

// goreleaserChecksum is the checksum entry.
type goreleaserChecksum struct {
	NameTemplate string `yaml:"name_template"`
}

// goreleaserDefaultFormat is the archive format GoReleaser uses when the
// config names none.
const goreleaserDefaultFormat = "tar.gz"

// TestReleaseArchives_MatchTheGoReleaserConfig checks that the fake release
// serves the archives that a real release has, each with the binary at the
// same place, and the same checksum file. The install scripts build these
// names from the OS and the architecture. When the config changes them, a
// real install fails, so this test must fail too.
func TestReleaseArchives_MatchTheGoReleaserConfig(t *testing.T) {
	t.Parallel()

	cfg := readGoreleaserConfig(t)

	want := goreleaserArchives(t, cfg)
	got := slices.SortedFunc(slices.Values(installtest.ReleaseArchives()), compareArchives)
	if !slices.Equal(got, want) {
		t.Errorf("archives in the fake release do not match %s:\n got %q\nwant %q", goreleaserConfigPath, got, want)
	}
	if got, want := installtest.ChecksumsName, cfg.Checksum.NameTemplate; got != want {
		t.Errorf("checksum file in the fake release = %q, want %q from %s", got, want, goreleaserConfigPath)
	}
}

// readGoreleaserConfig reads the repo's GoReleaser config.
func readGoreleaserConfig(t *testing.T) goreleaserConfig {
	t.Helper()

	data, err := os.ReadFile(goreleaserConfigPath)
	if err != nil {
		t.Fatalf("read GoReleaser config: %v", err)
	}
	var cfg goreleaserConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v", goreleaserConfigPath, err)
	}
	return cfg
}

// goreleaserArchives returns the archives that GoReleaser makes from cfg,
// sorted by name. It knows only one archives entry, so that an archive which
// selects some of the builds cannot slip past it.
func goreleaserArchives(t *testing.T, cfg goreleaserConfig) []installtest.ReleaseArchive {
	t.Helper()

	if len(cfg.Archives) != 1 {
		t.Fatalf("%s has %d archives entries, this test knows how to read only 1", goreleaserConfigPath, len(cfg.Archives))
	}
	archive := cfg.Archives[0]
	// missingkey=error makes a template that uses more than the OS and the
	// architecture fail here, because the install scripts know only those.
	tmpl, err := template.New("name_template").Option("missingkey=error").Parse(archive.NameTemplate)
	if err != nil {
		t.Fatalf("parse archive name_template %q: %v", archive.NameTemplate, err)
	}

	var archives []installtest.ReleaseArchive
	for _, build := range cfg.Builds {
		for _, goos := range build.Goos {
			binary := build.Binary
			if goos == "windows" {
				binary += ".exe"
			}
			for _, goarch := range build.Goarch {
				var name strings.Builder
				fields := map[string]string{
					"Os":   goos,
					"Arch": goarch,
				}
				if err := tmpl.Execute(&name, fields); err != nil {
					t.Fatalf("expand archive name_template %q: %v", archive.NameTemplate, err)
				}
				for _, format := range archiveFormats(archive, goos) {
					archives = append(archives, installtest.ReleaseArchive{
						Name:   name.String() + "." + format,
						Binary: binary,
					})
				}
			}
		}
	}
	slices.SortFunc(archives, compareArchives)
	return archives
}

// archiveFormats returns the formats GoReleaser archives goos in.
func archiveFormats(archive goreleaserArchive, goos string) []string {
	for _, o := range archive.FormatOverrides {
		if o.Goos == goos {
			return formats(o.Formats, o.Format)
		}
	}
	if f := formats(archive.Formats, archive.Format); len(f) > 0 {
		return f
	}
	return []string{
		goreleaserDefaultFormat,
	}
}

// formats returns the formats a config entry sets, from its formats list or
// else from its deprecated format field.
func formats(list []string, single string) []string {
	if len(list) > 0 || single == "" {
		return list
	}
	return []string{
		single,
	}
}

// compareArchives orders archives by name, then by binary.
func compareArchives(a, b installtest.ReleaseArchive) int {
	return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Binary, b.Binary))
}
