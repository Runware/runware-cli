package api

// TaskType identifies the Runware API task. Set by client methods; not exposed to callers.
type TaskType string

const (
	taskTypeImageInference    TaskType = "imageInference"
	taskTypeVideoInference    TaskType = "videoInference"
	taskTypeAudioInference    TaskType = "audioInference"
	taskTypeTextInference     TaskType = "textInference"
	taskTypeModelSearch       TaskType = "modelSearch"
	taskTypePing              TaskType = "ping"
	taskTypeGetResponse       TaskType = "getResponse"
	taskTypeAccountManagement TaskType = "accountManagement"
)

// DeliveryMethod specifies how task results are delivered.
type DeliveryMethod string

const DeliveryMethodAsync DeliveryMethod = "async"

// OutputFormat specifies the media output format.
type OutputFormat string

const (
	OutputFormatPNG  OutputFormat = "png"
	OutputFormatJPG  OutputFormat = "jpg"
	OutputFormatJPEG OutputFormat = "jpeg"
	OutputFormatWebP OutputFormat = "webp"
	OutputFormatMP3  OutputFormat = "mp3"
	OutputFormatMP4  OutputFormat = "mp4"
)

const (
	invalidAPIKeyCode = "invalidApiKey"
	jsonKeyTaskType   = "taskType"
	jsonKeyTaskUUID   = "taskUUID"
	jsonKeyOperation  = "operation"
)
