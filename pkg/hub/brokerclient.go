// Package hub provides the Scion Hub API server.
package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ptone/scion-agent/pkg/apiclient"
	"github.com/ptone/scion-agent/pkg/store"
)

// AuthenticatedHostClient is an HTTP-based RuntimeBrokerClient that signs
// outgoing requests with HMAC authentication. This allows the Hub to make
// authenticated requests to Runtime Brokers.
type AuthenticatedHostClient struct {
	httpClient *http.Client
	store      store.Store
	debug      bool
}

// NewAuthenticatedHostClient creates a new authenticated host client.
func NewAuthenticatedHostClient(s store.Store, debug bool) *AuthenticatedHostClient {
	return &AuthenticatedHostClient{
		httpClient: &http.Client{
			Timeout: 120 * time.Second, // Agent creation can take a while
		},
		store: s,
		debug: debug,
	}
}

// getBrokerSecret retrieves the secret key for a host from the store.
func (c *AuthenticatedHostClient) getBrokerSecret(ctx context.Context, brokerID string) ([]byte, error) {
	secret, err := c.store.GetBrokerSecret(ctx, brokerID)
	if err != nil {
		return nil, fmt.Errorf("failed to get broker secret: %w", err)
	}

	if secret.Status != store.BrokerSecretStatusActive {
		return nil, fmt.Errorf("broker secret is %s", secret.Status)
	}

	if !secret.ExpiresAt.IsZero() && time.Now().After(secret.ExpiresAt) {
		return nil, fmt.Errorf("broker secret has expired")
	}

	return secret.SecretKey, nil
}

// signRequest signs an HTTP request with HMAC authentication.
func (c *AuthenticatedHostClient) signRequest(ctx context.Context, req *http.Request, brokerID string) error {
	secret, err := c.getBrokerSecret(ctx, brokerID)
	if err != nil {
		return err
	}

	// Use the shared HMAC auth implementation
	auth := &apiclient.HMACAuth{
		BrokerID:    brokerID,
		SecretKey: secret,
	}

	return auth.ApplyAuth(req)
}

// doRequest performs an HTTP request with HMAC signing.
func (c *AuthenticatedHostClient) doRequest(ctx context.Context, brokerID, method, endpoint string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Sign the request
	if err := c.signRequest(ctx, req, brokerID); err != nil {
		if c.debug {
			slog.Warn("Failed to sign request", "brokerID", brokerID, "error", err)
		}
		// Continue without authentication - the host may reject or allow depending on its config
	} else if c.debug {
		slog.Debug("Signed request for host", "brokerID", brokerID)
	}

	if c.debug {
		slog.Debug("Outgoing request to host", "method", method, "endpoint", endpoint)
	}

	return c.httpClient.Do(req)
}

// CreateAgent creates an agent on a remote runtime broker with HMAC authentication.
func (c *AuthenticatedHostClient) CreateAgent(ctx context.Context, brokerID, hostEndpoint string, req *RemoteCreateAgentRequest) (*RemoteAgentResponse, error) {
	endpoint := fmt.Sprintf("%s/api/v1/agents", strings.TrimSuffix(hostEndpoint, "/"))

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, brokerID, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("runtime broker returned error %d: %s", resp.StatusCode, string(respBody))
	}

	var result RemoteAgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// StartAgent starts an agent on a remote runtime broker with HMAC authentication.
func (c *AuthenticatedHostClient) StartAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	endpoint := fmt.Sprintf("%s/api/v1/agents/%s/start", strings.TrimSuffix(hostEndpoint, "/"), url.PathEscape(agentID))

	resp, err := c.doRequest(ctx, brokerID, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("runtime broker returned error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// StopAgent stops an agent on a remote runtime broker with HMAC authentication.
func (c *AuthenticatedHostClient) StopAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	endpoint := fmt.Sprintf("%s/api/v1/agents/%s/stop", strings.TrimSuffix(hostEndpoint, "/"), url.PathEscape(agentID))

	resp, err := c.doRequest(ctx, brokerID, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("runtime broker returned error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// RestartAgent restarts an agent on a remote runtime broker with HMAC authentication.
func (c *AuthenticatedHostClient) RestartAgent(ctx context.Context, brokerID, hostEndpoint, agentID string) error {
	endpoint := fmt.Sprintf("%s/api/v1/agents/%s/restart", strings.TrimSuffix(hostEndpoint, "/"), url.PathEscape(agentID))

	resp, err := c.doRequest(ctx, brokerID, http.MethodPost, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("runtime broker returned error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// DeleteAgent deletes an agent from a remote runtime broker with HMAC authentication.
func (c *AuthenticatedHostClient) DeleteAgent(ctx context.Context, brokerID, hostEndpoint, agentID string, deleteFiles, removeBranch bool) error {
	endpoint := fmt.Sprintf("%s/api/v1/agents/%s?deleteFiles=%t&removeBranch=%t",
		strings.TrimSuffix(hostEndpoint, "/"), url.PathEscape(agentID), deleteFiles, removeBranch)

	resp, err := c.doRequest(ctx, brokerID, http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("runtime broker returned error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// MessageAgent sends a message to an agent on a remote runtime broker with HMAC authentication.
func (c *AuthenticatedHostClient) MessageAgent(ctx context.Context, brokerID, hostEndpoint, agentID, message string, interrupt bool) error {
	endpoint := fmt.Sprintf("%s/api/v1/agents/%s/message", strings.TrimSuffix(hostEndpoint, "/"), url.PathEscape(agentID))

	body, err := json.Marshal(map[string]interface{}{
		"message":   message,
		"interrupt": interrupt,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.doRequest(ctx, brokerID, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("runtime broker returned error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
