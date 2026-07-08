package server

import (
	"context"

	"github.com/PineappleBond/xyncra-server/pkg/protocol"
)

// NodeBroadcaster handles cross-node message routing.
// In single-node deployments, it is a no-op.
// In multi-node deployments, it uses Redis Pub/Sub to fan out updates.
// See PRODUCT_DECISIONS.md D-018 for details.
type NodeBroadcaster interface {
	// Publish broadcasts updates for a user to all nodes.
	// The sourceNodeID is included to allow the receiving node to skip
	// messages it originated (avoiding duplicate delivery).
	Publish(ctx context.Context, userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) error

	// Subscribe starts listening for broadcast messages from other nodes.
	// The callback is invoked for each received broadcast.
	// It blocks until ctx is cancelled.
	Subscribe(ctx context.Context, callback func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string)) error

	// Close releases Pub/Sub resources.
	Close() error
}

// NoopBroadcaster is a NodeBroadcaster that does nothing.
// Used in single-node deployments where cross-node routing is not needed.
type NoopBroadcaster struct{}

// Ensure NoopBroadcaster implements NodeBroadcaster at compile time.
var _ NodeBroadcaster = (*NoopBroadcaster)(nil)

// Publish is a no-op. It always returns nil.
func (n *NoopBroadcaster) Publish(ctx context.Context, userID string, updates *protocol.PackageDataUpdates, sourceNodeID string) error {
	return nil
}

// Subscribe blocks until ctx is cancelled, then returns ctx.Err().
func (n *NoopBroadcaster) Subscribe(ctx context.Context, callback func(userID string, updates *protocol.PackageDataUpdates, sourceNodeID string)) error {
	<-ctx.Done()
	return ctx.Err()
}

// Close is a no-op. It always returns nil.
func (n *NoopBroadcaster) Close() error {
	return nil
}
