package channel

const (
	// ResponseStartMarker marks the beginning of a model response in structured output mode.
	ResponseStartMarker = "<<ALFRED_RESPONSE>>"
	// ResponseEndMarker marks the end of a model response in structured output mode.
	ResponseEndMarker = "<<END_RESPONSE>>"
	// EnvCLIResponseMarkers is the environment variable that enables response markers.
	EnvCLIResponseMarkers = "ALFREDAI_CLI_RESPONSE_MARKERS"
)
