package recognize

import "context"

// Result represents a recognition result with platform routing info.
type Result struct {
	Platform string
	TrackID  string
	URL      string
}

// Service defines audio recognition behavior.
type Service interface {
	Start(ctx context.Context) error
	Stop() error
	Recognize(ctx context.Context, audioData []byte) (*Result, error)
}
