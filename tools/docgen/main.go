package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/runware/runware-cli/internal/cmd"
	"github.com/spf13/cobra/doc"
)

var (
	outputDir   string
	format      string
	frontmatter bool
)

func main() {
	flag.StringVar(&outputDir, "out", "./docs/cli", "output directory")
	flag.StringVar(&format, "format", "markdown", "markdown|man|rest")
	flag.BoolVar(&frontmatter, "frontmatter", false, "prepend simple YAML front matter to markdown")
	flag.Parse()

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatal(err)
	}

	root := cmd.Root()
	root.DisableAutoGenTag = true // stable, reproducible files (no timestamp footer)

	switch format {
	case "markdown":
		if frontmatter {
			prep := func(filename string) string {
				base := filepath.Base(filename)
				name := strings.TrimSuffix(base, filepath.Ext(base))
				title := strings.ReplaceAll(name, "_", " ")
				return fmt.Sprintf("---\ntitle: %q\nslug: %q\ndescription: \"CLI reference for %s\"\n---\n\n", title, name, title)
			}
			if err := doc.GenMarkdownTreeCustom(root, outputDir, prep, strings.ToLower); err != nil {
				log.Fatal(err)
			}
		} else {
			if err := doc.GenMarkdownTree(root, outputDir); err != nil {
				log.Fatal(err)
			}
		}
	case "man":
		hdr := &doc.GenManHeader{Title: strings.ToUpper(root.Name()), Section: "1"}
		if err := doc.GenManTree(root, hdr, outputDir); err != nil {
			log.Fatal(err)
		}
	case "rest":
		if err := doc.GenReSTTree(root, outputDir); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown format: %s", format)
	}
}
