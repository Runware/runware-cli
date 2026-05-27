package cmd

// tableHeader represents headers used in table output.
type tableHeader string

const (
	tableHeaderNum  tableHeader = "#"
	tableHeaderSeed tableHeader = "Seed"
	tableHeaderURL  tableHeader = "URL"
	tableHeaderFile tableHeader = "File"
	tableHeaderCost tableHeader = "Cost"
)
