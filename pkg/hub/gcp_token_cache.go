// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package hub

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// CachedGCPTokenGenerator wraps a GCPTokenGenerator and caches access tokens
// per service account + scopes combination. This avoids redundant IAM API calls
// when multiple agents share the same service account.
type CachedGCPTokenGenerator struct {
	inner GCPTokenGenerator

	mu    sync.RWMutex
	cache map[string]*cachedTokenEntry

	group singleflight.Group
}

type cachedTokenEntry struct {
	token     *GCPAccessToken
	fetchedAt time.Time
}

// NewCachedGCPTokenGenerator wraps an existing generator with a per-SA cache.
func NewCachedGCPTokenGenerator(inner GCPTokenGenerator) *CachedGCPTokenGenerator {
	return &CachedGCPTokenGenerator{
		inner: inner,
		cache: make(map[string]*cachedTokenEntry),
	}
}

func tokenCacheKey(email string, scopes []string) string {
	sorted := make([]string, len(scopes))
	copy(sorted, scopes)
	sort.Strings(sorted)
	return email + "|" + strings.Join(sorted, ",")
}

func (c *CachedGCPTokenGenerator) GenerateAccessToken(ctx context.Context, serviceAccountEmail string, scopes []string) (*GCPAccessToken, error) {
	key := tokenCacheKey(serviceAccountEmail, scopes)

	// Check cache
	c.mu.RLock()
	entry := c.cache[key]
	c.mu.RUnlock()

	if entry != nil {
		elapsed := time.Since(entry.fetchedAt)
		remaining := time.Duration(entry.token.ExpiresIn)*time.Second - elapsed
		if remaining > 5*time.Minute {
			return &GCPAccessToken{
				AccessToken: entry.token.AccessToken,
				ExpiresIn:   int(remaining.Seconds()),
				TokenType:   entry.token.TokenType,
			}, nil
		}
	}

	// Singleflight: deduplicate concurrent requests for the same SA+scopes
	result, err, _ := c.group.Do(key, func() (interface{}, error) {
		token, err := c.inner.GenerateAccessToken(ctx, serviceAccountEmail, scopes)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.cache[key] = &cachedTokenEntry{
			token:     token,
			fetchedAt: time.Now(),
		}
		c.mu.Unlock()
		return token, nil
	})
	if err != nil {
		return nil, err
	}

	orig := result.(*GCPAccessToken)
	return &GCPAccessToken{
		AccessToken: orig.AccessToken,
		ExpiresIn:   orig.ExpiresIn,
		TokenType:   orig.TokenType,
	}, nil
}

func (c *CachedGCPTokenGenerator) GenerateIDToken(ctx context.Context, serviceAccountEmail string, audience string) (*GCPIDToken, error) {
	return c.inner.GenerateIDToken(ctx, serviceAccountEmail, audience)
}

func (c *CachedGCPTokenGenerator) VerifyImpersonation(ctx context.Context, serviceAccountEmail string) error {
	return c.inner.VerifyImpersonation(ctx, serviceAccountEmail)
}

func (c *CachedGCPTokenGenerator) ServiceAccountEmail() string {
	return c.inner.ServiceAccountEmail()
}
