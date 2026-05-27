package cmd

// defaultRequestTimeoutSeconds is the default HTTP request timeout applied to all API calls.
// Override per-invocation with the --request-timeout flag.
const defaultRequestTimeoutSeconds = 120

// defaultDownloadTimeoutSeconds is the default per-file download timeout for image inference results.
// Override per-invocation with the --download-timeout flag on imageInference.
const defaultDownloadTimeoutSeconds = 60
