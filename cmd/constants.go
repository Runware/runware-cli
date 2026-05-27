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
	defaultImageDownloadTimeout = 1 * time.Minute
	defaultAudioDownloadTimeout = 5 * time.Minute
	defaultVideoDownloadTimeout = 10 * time.Minute
)
