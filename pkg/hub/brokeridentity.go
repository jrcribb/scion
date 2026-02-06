// Package hub provides the Scion Hub API server.
package hub

import (
	"context"
)

// BrokerIdentity represents an authenticated Runtime Broker.
type BrokerIdentity interface {
	Identity
	BrokerID() string
}

// brokerIdentityImpl implements BrokerIdentity.
type brokerIdentityImpl struct {
	brokerID string
}

// ID returns the host ID.
func (h *brokerIdentityImpl) ID() string { return h.brokerID }

// Type returns the identity type ("host").
func (h *brokerIdentityImpl) Type() string { return "host" }

// HostID returns the host ID.
func (h *brokerIdentityImpl) BrokerID() string { return h.brokerID }

// NewBrokerIdentity creates a new BrokerIdentity.
func NewBrokerIdentity(brokerID string) BrokerIdentity {
	return &brokerIdentityImpl{brokerID: brokerID}
}

// brokerIdentityContextKey is the context key for BrokerIdentity.
type brokerIdentityContextKey struct{}

// GetBrokerIdentityFromContext returns the BrokerIdentity from the context, if present.
func GetBrokerIdentityFromContext(ctx context.Context) BrokerIdentity {
	if identity, ok := ctx.Value(brokerIdentityContextKey{}).(BrokerIdentity); ok {
		return identity
	}
	// Also check the generic identity key
	if identity, ok := ctx.Value(identityContextKey{}).(BrokerIdentity); ok {
		return identity
	}
	return nil
}

// contextWithBrokerIdentity returns a new context with the BrokerIdentity set.
func contextWithBrokerIdentity(ctx context.Context, broker BrokerIdentity) context.Context {
	return context.WithValue(ctx, brokerIdentityContextKey{}, broker)
}
