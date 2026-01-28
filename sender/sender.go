package sender

import "context"

// Sender defines the contract for delivering log batches to a destination.
// Implementations should block in Start until the context is canceled.
type Sender interface {
	Start(ctx context.Context)
}
