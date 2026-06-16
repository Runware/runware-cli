package run

// Field name constants for API payload keys used throughout the run package.
const (
	fieldTaskType = "taskType"
	fieldTaskUUID = "taskUUID"
	fieldImageURL = "imageURL"
	fieldVideoURL = "videoURL"
	fieldAudioURL = "audioURL"
	fieldModelURL = "modelURL"
	fieldText     = "text"
	fieldStatus   = "status"

	// taskType constants for known task type values used by the run command.
	taskTypeImage    = "imageInference"
	taskTypeVideo    = "videoInference"
	taskTypeAudio    = "audioInference"
	taskTypeText     = "textInference"
	taskType3D       = "3dInference"
	taskTypeTraining = "training"

	// fieldOutputs is the top-level result field used by 3D inference responses.
	// Its value is an object with a "files" array: {"files":[{"url":"...","uuid":"..."}]}.
	fieldOutputs     = "outputs"
	fieldOutputFiles = "files"
	fieldOutputURL   = "url"
)
