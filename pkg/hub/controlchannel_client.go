package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/ptone/scion-agent/pkg/wsprotocol"
)

// ControlChannelHostClient implements RuntimeBrokerClient by tunneling requests
// through the control channel WebSocket connection.
type ControlChannelHostClient struct {
	manager *ControlChannelManager
	debug   bool
}

// NewControlChannelHostClient creates a new control channel host client.
func NewControlChannelHostClient(manager *ControlChannelManager, debug bool) *ControlChannelHostClient {
	return &ControlChannelHostClient{
		manager: manager,
		debug:   debug,
	}
}

// CreateAgent creates an agent via control channel.
func (c *ControlChannelHostClient) CreateAgent(ctx context.Context, brokerID, hostEndpoint string, req *RemoteCreateAgentRequest) (*RemoteAgentResponse, error) {
	_ = hostEndpoint // Unused - we tunnel through control channel

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, brokerID, "POST", "/api/v1/agents", "", body)
	if err != nil {
		return nil, err
	}

	var result RemoteAgentResponse
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// StartAgent starts an agent via control channel.
func (c *ControlChannelHostClient) StartAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	_ = hostEndpoint
	path := fmt.Sprintf("/api/v1/agents/%s/start", agentID)
	_, err := c.doRequest(ctx, brokerID, "POST", path, "", nil)
	return err
}

// StopAgent stops an agent via control channel.
func (c *ControlChannelHostClient) StopAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	_ = hostEndpoint
	path := fmt.Sprintf("/api/v1/agents/%s/stop", agentID)
	_, err := c.doRequest(ctx, brokerID, "POST", path, "", nil)
	return err
}

// RestartAgent restarts an agent via control channel.
func (c *ControlChannelHostClient) RestartAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	_ = hostEndpoint
	path := fmt.Sprintf("/api/v1/agents/%s/restart", agentID)
	_, err := c.doRequest(ctx, brokerID, "POST", path, "", nil)
	return err
}

// DeleteAgent deletes an agent via control channel.
func (c *ControlChannelHostClient) DeleteAgent(ctx context.Context, brokerID, hostEndpoint, agentID string, deleteFiles, removeBranch bool) error {
	_ = hostEndpoint
	path := fmt.Sprintf("/api/v1/agents/%s", agentID)
	query := fmt.Sprintf("deleteFiles=%t&removeBranch=%t", deleteFiles, removeBranch)
	resp, err := c.doRequest(ctx, brokerID, "DELETE", path, query, nil)
	if err != nil {
		return err
	}
	// Allow 404 for idempotent delete
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return nil
}

// MessageAgent sends a message to an agent via control channel.
func (c *ControlChannelHostClient) MessageAgent(ctx context.Context, brokerID, hostEndpoint, agentID, message string, interrupt bool) error {
	_ = hostEndpoint
	path := fmt.Sprintf("/api/v1/agents/%s/message", agentID)

	body, err := json.Marshal(map[string]interface{}{
		"message":   message,
		"interrupt": interrupt,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	_, err = c.doRequest(ctx, brokerID, "POST", path, "", body)
	return err
}

// doRequest tunnels an HTTP request through the control channel.
func (c *ControlChannelHostClient) doRequest(ctx context.Context, brokerID, method, path, query string, body []byte) (*wsprotocol.ResponseEnvelope, error) {
	if !c.manager.IsConnected(brokerID) {
		return nil, fmt.Errorf("host %s not connected via control channel", brokerID)
	}

	headers := map[string]string{
		"Content-Type": "application/json",
	}

	req := wsprotocol.NewRequestEnvelope(uuid.New().String(), method, path, query, headers, body)
	resp, err := c.manager.TunnelRequest(ctx, brokerID, req)
	if err != nil {
		return nil, fmt.Errorf("control channel request failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("runtime broker returned error %d: %s", resp.StatusCode, string(resp.Body))
	}

	return resp, nil
}

// HybridHostClient tries control channel first, falls back to HTTP.
type HybridHostClient struct {
	controlChannel *ControlChannelHostClient
	httpClient     RuntimeBrokerClient
	debug          bool
}

// NewHybridHostClient creates a hybrid client that prefers control channel.
func NewHybridHostClient(manager *ControlChannelManager, httpClient RuntimeBrokerClient, debug bool) *HybridHostClient {
	return &HybridHostClient{
		controlChannel: NewControlChannelHostClient(manager, debug),
		httpClient:     httpClient,
		debug:          debug,
	}
}

// useControlChannel returns true if we should use control channel for this host.
func (c *HybridHostClient) useControlChannel(brokerID string) bool {
	return c.controlChannel.manager.IsConnected(brokerID)
}

// CreateAgent creates an agent, preferring control channel.
func (c *HybridHostClient) CreateAgent(ctx context.Context, brokerID, hostEndpoint string, req *RemoteCreateAgentRequest) (*RemoteAgentResponse, error) {
	if c.useControlChannel(brokerID) {
		return c.controlChannel.CreateAgent(ctx, brokerID, hostEndpoint, req)
	}
	return c.httpClient.CreateAgent(ctx, brokerID, hostEndpoint, req)
}

// StartAgent starts an agent, preferring control channel.
func (c *HybridHostClient) StartAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	if c.useControlChannel(brokerID) {
		return c.controlChannel.StartAgent(ctx, brokerID, hostEndpoint, agentID)
	}
	return c.httpClient.StartAgent(ctx, brokerID, hostEndpoint, agentID)
}

// StopAgent stops an agent, preferring control channel.
func (c *HybridHostClient) StopAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	if c.useControlChannel(brokerID) {
		return c.controlChannel.StopAgent(ctx, brokerID, hostEndpoint, agentID)
	}
	return c.httpClient.StopAgent(ctx, brokerID, hostEndpoint, agentID)
}

// RestartAgent restarts an agent, preferring control channel.
func (c *HybridHostClient) RestartAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	if c.useControlChannel(brokerID) {
		return c.controlChannel.RestartAgent(ctx, brokerID, hostEndpoint, agentID)
	}
	return c.httpClient.RestartAgent(ctx, brokerID, hostEndpoint, agentID)
}

// DeleteAgent deletes an agent, preferring control channel.
func (c *HybridHostClient) DeleteAgent(ctx context.Context, brokerID, hostEndpoint, agentID string, deleteFiles, removeBranch bool) error {
	if c.useControlChannel(brokerID) {
		return c.controlChannel.DeleteAgent(ctx, brokerID, hostEndpoint, agentID, deleteFiles, removeBranch)
	}
	return c.httpClient.DeleteAgent(ctx, brokerID, hostEndpoint, agentID, deleteFiles, removeBranch)
}

// MessageAgent sends a message to an agent, preferring control channel.
func (c *HybridHostClient) MessageAgent(ctx context.Context, brokerID, hostEndpoint, agentID, message string, interrupt bool) error {
	if c.useControlChannel(brokerID) {
		return c.controlChannel.MessageAgent(ctx, brokerID, hostEndpoint, agentID, message, interrupt)
	}
	return c.httpClient.MessageAgent(ctx, brokerID, hostEndpoint, agentID, message, interrupt)
}
