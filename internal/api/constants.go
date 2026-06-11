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

const (
	DeliveryMethodAsync DeliveryMethod = "async"
	DeliveryMethodSync  DeliveryMethod = "sync"
)

// OutputFormat specifies the media output format.
type OutputFormat string

const (
	OutputFormatPNG  OutputFormat = "png"
	OutputFormatJPG  OutputFormat = "jpg"
	OutputFormatJPEG OutputFormat = "jpeg"
	OutputFormatWebP OutputFormat = "webp"
	OutputFormatMP3  OutputFormat = "mp3"
	OutputFormatMP4  OutputFormat = "mp4"
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
)

// Payload field name constants used internally by Client methods.
const (
	fieldModel          = "model"
	fieldTaskType       = "taskType"
	fieldTaskUUID       = "taskUUID"
	fieldDeliveryMethod = "deliveryMethod"
)
