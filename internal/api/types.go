package api

import (
	"encoding/json"

	"github.com/google/uuid"
)

// APIResponse is the top-level response from the Runware API.
type APIResponse struct {
	Data   []json.RawMessage `json:"data"`
	Errors []APIError        `json:"errors,omitempty"`
}

// ImageInferenceRequest contains fields for the imageInference task type.
type ImageInferenceRequest struct {
	TaskType       TaskType     `json:"taskType"`
	TaskUUID       uuid.UUID    `json:"taskUUID"`
	PositivePrompt string       `json:"positivePrompt"`
	NegativePrompt string       `json:"negativePrompt,omitempty"`
	Model          string       `json:"model"`
	Width          int          `json:"width"`
	Height         int          `json:"height"`
	Steps          int          `json:"steps,omitempty"`
	NumberResults  int          `json:"numberResults"`
	CFGScale       float64      `json:"CFGScale,omitempty"`
	Scheduler      string       `json:"scheduler,omitempty"`
	Seed           int64        `json:"seed,omitempty"`
	OutputFormat   OutputFormat `json:"outputFormat,omitempty"`
	InputImage     string       `json:"inputImage,omitempty"`
	Strength       float64      `json:"strength,omitempty"`
	MaskImage      string       `json:"maskImage,omitempty"`
}

// ImageInferenceResult is a single image result from the API.
type ImageInferenceResult struct {
	TaskType  TaskType  `json:"taskType"`
	TaskUUID  uuid.UUID `json:"taskUUID"`
	ImageUUID uuid.UUID `json:"imageUUID"`
	ImageURL  string    `json:"imageURL"`
	Seed      int64     `json:"seed"`
}

// PingResult is the response from a ping task.
type PingResult struct {
	TaskType TaskType `json:"taskType"`
	Pong     bool     `json:"pong"`
}

// AccountResult is the response from accountManagement getDetails.
type AccountResult struct {
	TaskType         TaskType     `json:"taskType"`
	TaskUUID         uuid.UUID    `json:"taskUUID"`
	OrganizationUUID uuid.UUID    `json:"organizationUUID"`
	OrganizationName string       `json:"organizationName"`
	Balance          float64      `json:"balance"`
	Usage            AccountUsage `json:"usage"`
}

type AccountUsage struct {
	Total      UsagePeriod `json:"total"`
	Today      UsagePeriod `json:"today"`
	Last7Days  UsagePeriod `json:"last7Days"`
	Last30Days UsagePeriod `json:"last30Days"`
}

type UsagePeriod struct {
	Credits  float64 `json:"credits"`
	Requests int     `json:"requests"`
}

// VideoInferenceRequest contains fields for the videoInference task type.
type VideoInferenceRequest struct {
	TaskType       TaskType       `json:"taskType"`
	TaskUUID       uuid.UUID      `json:"taskUUID"`
	Model          string         `json:"model"`
	PositivePrompt string         `json:"positivePrompt,omitempty"`
	NegativePrompt string         `json:"negativePrompt,omitempty"`
	Width          int            `json:"width,omitempty"`
	Height         int            `json:"height,omitempty"`
	Duration       float64        `json:"duration,omitempty"`
	Steps          int            `json:"steps,omitempty"`
	CFGScale       float64        `json:"CFGScale,omitempty"`
	Seed           int64          `json:"seed,omitempty"`
	NumberResults  int            `json:"numberResults,omitempty"`
	OutputFormat   OutputFormat   `json:"outputFormat,omitempty"`
	DeliveryMethod DeliveryMethod `json:"deliveryMethod,omitempty"`
	FrameImages    []FrameImage   `json:"frameImages,omitempty"`
	IncludeCost    bool           `json:"includeCost,omitempty"`
}

// FrameImage constrains a specific frame with an input image.
type FrameImage struct {
	InputImage string `json:"inputImage"`
	Frame      any    `json:"frame"` // "first", "last", or int
}

// VideoInferenceResult is a single video result from the API.
type VideoInferenceResult struct {
	TaskType  TaskType  `json:"taskType"`
	TaskUUID  uuid.UUID `json:"taskUUID"`
	Status    string    `json:"status,omitempty"`
	VideoUUID uuid.UUID `json:"videoUUID"`
	VideoURL  string    `json:"videoURL,omitempty"`
	MediaUUID uuid.UUID `json:"mediaUUID"`
	MediaURL  string    `json:"mediaURL,omitempty"`
	Seed      int64     `json:"seed,omitempty"`
	Cost      float64   `json:"cost,omitempty"`
}

// GetResponseRequest is used to poll for async task results.
type GetResponseRequest struct {
	TaskType TaskType  `json:"taskType"`
	TaskUUID uuid.UUID `json:"taskUUID"`
}

// AudioInferenceRequest contains fields for the audioInference task type.
type AudioInferenceRequest struct {
	TaskType       TaskType       `json:"taskType"`
	TaskUUID       uuid.UUID      `json:"taskUUID"`
	Model          string         `json:"model"`
	PositivePrompt string         `json:"positivePrompt,omitempty"`
	Duration       float64        `json:"duration"`
	NumberResults  int            `json:"numberResults"`
	OutputFormat   OutputFormat   `json:"outputFormat,omitempty"`
	DeliveryMethod DeliveryMethod `json:"deliveryMethod,omitempty"`
	IncludeCost    bool           `json:"includeCost,omitempty"`
	AudioSettings  *AudioSettings `json:"audioSettings,omitempty"`
}

// AudioSettings configures technical audio quality parameters.
type AudioSettings struct {
	SampleRate int `json:"sampleRate,omitempty"`
	Bitrate    int `json:"bitrate,omitempty"`
}

// AudioInferenceResult is a single audio result from the API.
type AudioInferenceResult struct {
	TaskType  TaskType  `json:"taskType"`
	TaskUUID  uuid.UUID `json:"taskUUID"`
	Status    string    `json:"status,omitempty"`
	AudioUUID uuid.UUID `json:"audioUUID"`
	AudioURL  string    `json:"audioURL,omitempty"`
	Cost      float64   `json:"cost,omitempty"`
}

// Message represents a single message in a text inference conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// TextInferenceRequest contains fields for the textInference task type.
type TextInferenceRequest struct {
	TaskType      TaskType     `json:"taskType"`
	TaskUUID      uuid.UUID    `json:"taskUUID"`
	Model         string       `json:"model"`
	Messages      []Message    `json:"messages"`
	MaxTokens     int          `json:"maxTokens,omitempty"`
	Temperature   float64      `json:"temperature,omitempty"`
	TopP          float64      `json:"topP,omitempty"`
	TopK          int          `json:"topK,omitempty"`
	Seed          int64        `json:"seed,omitempty"`
	StopSequences []string     `json:"stopSequences,omitempty"`
	NumberResults int          `json:"numberResults,omitempty"`
	SystemPrompt  string       `json:"systemPrompt,omitempty"`
	OutputFormat  OutputFormat `json:"outputFormat,omitempty"`
	IncludeCost   bool         `json:"includeCost,omitempty"`
}

// TextInferenceUsage contains token usage statistics.
type TextInferenceUsage struct {
	InputTokens  int `json:"inputTokens,omitempty"`
	OutputTokens int `json:"outputTokens,omitempty"`
	TotalTokens  int `json:"totalTokens,omitempty"`
}

// TextInferenceResult is a single text result from the API.
type TextInferenceResult struct {
	TaskType     TaskType           `json:"taskType"`
	TaskUUID     uuid.UUID          `json:"taskUUID"`
	Text         string             `json:"text"`
	FinishReason string             `json:"finishReason,omitempty"`
	Usage        TextInferenceUsage `json:"usage"`
	Cost         float64            `json:"cost,omitempty"`
}

// ModelSearchRequest contains fields for the modelSearch task type.
type ModelSearchRequest struct {
	TaskType     TaskType  `json:"taskType"`
	TaskUUID     uuid.UUID `json:"taskUUID"`
	Search       string    `json:"search,omitempty"`
	Category     string    `json:"category,omitempty"`
	Architecture string    `json:"architecture,omitempty"`
	Limit        int       `json:"limit,omitempty"`
	Offset       int       `json:"offset,omitempty"`
}

// ModelSearchResponse is the response wrapper from modelSearch.
type ModelSearchResponse struct {
	TaskType     TaskType      `json:"taskType"`
	TaskUUID     uuid.UUID     `json:"taskUUID"`
	Results      []ModelResult `json:"results"`
	TotalResults int           `json:"totalResults"`
}

// ModelResult is a single model from the search results.
type ModelResult struct {
	Name                 string   `json:"name"`
	AIR                  string   `json:"air"`
	Tags                 []string `json:"tags"`
	HeroImage            string   `json:"heroImage"`
	Category             string   `json:"category"`
	Private              bool     `json:"private"`
	Version              string   `json:"version"`
	Architecture         string   `json:"architecture"`
	NSFWLevel            int      `json:"nsfwLevel"`
	Type                 string   `json:"type,omitempty"`
	DefaultWeight        float64  `json:"defaultWeight,omitempty"`
	DefaultWidth         int      `json:"defaultWidth,omitempty"`
	DefaultHeight        int      `json:"defaultHeight,omitempty"`
	DefaultSteps         int      `json:"defaultSteps,omitempty"`
	DefaultScheduler     string   `json:"defaultScheduler,omitempty"`
	DefaultCFG           float64  `json:"defaultCFG,omitempty"`
	DefaultStrength      float64  `json:"defaultStrength,omitempty"`
	PositiveTriggerWords string   `json:"positiveTriggerWords,omitempty"`
	NegativeTriggerWords string   `json:"negativeTriggerWords,omitempty"`
}
