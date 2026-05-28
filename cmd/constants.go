package cmd

import "time"

// tableHeader represents headers used in table output.
type tableHeader string

const (
	tableHeaderNum  tableHeader = "#"
	tableHeaderSeed tableHeader = "Seed"
	tableHeaderURL  tableHeader = "URL"
	tableHeaderFile tableHeader = "File"
	tableHeaderCost tableHeader = "Cost"
)

const (
	// Poll intervals.
	defaultPollInterval = 5 * time.Second

	// Generation timeouts.
	defaultAudioGenerationTimeout = 5 * time.Minute
	defaultVideoGenerationTimeout = 10 * time.Minute

	// Download timeouts.
	defaultImageDownloadTimeout = 1 * time.Minute
	defaultAudioDownloadTimeout = 5 * time.Minute
	defaultVideoDownloadTimeout = 10 * time.Minute

	// Audio defaults.
	defaultMinAudioDuration = 10.0
	maxAudioDuration        = 300.0
)
