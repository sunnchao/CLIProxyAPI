package executor

const (
	// streamScannerBufferSize is the maximum buffer size for scanning streaming responses.
	// Set to 20MB to handle large response chunks from AI providers.
	streamScannerBufferSize = 20 * 1024 * 1024 // 20MB
)
