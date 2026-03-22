package api

import "encoding/json"

// APIResponse is the top-level response from the Runware API.
type APIResponse struct {
	Data   []json.RawMessage `json:"data"`
	Errors []APIError        `json:"errors,omitempty"`
}

// APIError represents an error returned by the API.
type APIError struct {
	Code          string   `json:"code"`
	Message       string   `json:"message"`
	Parameter     string   `json:"parameter,omitempty"`
	Type          string   `json:"type,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	TaskUUID      string   `json:"taskUUID,omitempty"`
	AllowedValues []string `json:"allowedValues,omitempty"`
}

func (e APIError) Error() string {
	return e.Message
}

// ImageInferenceRequest contains fields for the imageInference task type.
type ImageInferenceRequest struct {
	TaskType       string  `json:"taskType"`
	TaskUUID       string  `json:"taskUUID"`
	PositivePrompt string  `json:"positivePrompt"`
	NegativePrompt string  `json:"negativePrompt,omitempty"`
	Model          string  `json:"model"`
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Steps          int     `json:"steps,omitempty"`
	NumberResults  int     `json:"numberResults"`
	CFGScale       float64 `json:"CFGScale,omitempty"`
	Scheduler      string  `json:"scheduler,omitempty"`
	Seed           int64   `json:"seed,omitempty"`
	OutputFormat   string  `json:"outputFormat,omitempty"`
	InputImage     string  `json:"inputImage,omitempty"`
	Strength       float64 `json:"strength,omitempty"`
	MaskImage      string  `json:"maskImage,omitempty"`
}

// ImageInferenceResult is a single image result from the API.
type ImageInferenceResult struct {
	TaskType  string `json:"taskType"`
	TaskUUID  string `json:"taskUUID"`
	ImageUUID string `json:"imageUUID"`
	ImageURL  string `json:"imageURL"`
	Seed      int64  `json:"seed"`
}

// PingResult is the response from a ping task.
type PingResult struct {
	TaskType string `json:"taskType"`
	Pong     bool   `json:"pong"`
}

// AccountResult is the response from accountManagement getDetails.
type AccountResult struct {
	TaskType         string       `json:"taskType"`
	TaskUUID         string       `json:"taskUUID"`
	OrganizationUUID string       `json:"organizationUUID"`
	OrganizationName string       `json:"organizationName"`
	Balance          float64      `json:"balance"`
	Usage            AccountUsage `json:"usage"`
}

type AccountUsage struct {
	Total     UsagePeriod `json:"total"`
	Today     UsagePeriod `json:"today"`
	Last7Days UsagePeriod `json:"last7Days"`
	Last30Days UsagePeriod `json:"last30Days"`
}

type UsagePeriod struct {
	Credits  float64 `json:"credits"`
	Requests int     `json:"requests"`
}
