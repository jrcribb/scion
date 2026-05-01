# Investigation: GCP Auth Token Failure on Hub Host

**Status:** Open — primary hypothesis not confirmed by iptables diagnostics  
**Date:** 2026-05-01  
**Symptom:** Hub host unable to obtain GCP access token for its own service account. The real GCP metadata server at `169.254.169.254` returns (or appears to return) `502 Bad Gateway` with body `"token generation failed"` after a 30-second timeout.

## Observed Behavior

```
$ time curl -i -H "Metadata-Flavor: Google" \
    "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token"

HTTP/1.1 502 Bad Gateway
Content-Type: text/plain; charset=utf-8
Metadata-Flavor: Google

token generation failed

real    0m30.010s
```

Key observations:
- The response body `"token generation failed"` matches the exact error string in `pkg/sciontool/metadata/server.go:411`, not anything the real GCE metadata server would return.
- The 30-second delay matches the HTTP client timeout in the scion metadata server (`server.go:139`).
- The response includes `Metadata-Flavor: Google` header, which the scion metadata server also sets.
- The symptom appeared after the hub had been running successfully for many hours with ~20 agents.

## Hypothesis 1: iptables REDIRECT leak from host-networked containers

### Theory

When the hub runs on localhost, `ResolveDockerNetworking()` (`pkg/runtime/common.go:538`) returns `"host"`, causing agent containers to run with `--network=host`. In host network mode, the container shares the host's network namespace.

The scion metadata sidecar (inside each container) adds an iptables rule to the nat OUTPUT chain:

```
iptables -t nat -A OUTPUT -d 169.254.169.254 -p tcp --dport 80 -j REDIRECT --to-port 18380
```

This rule has **no UID-based exclusion** (`iptables.go:35-43`). In host network mode, it would capture ALL outgoing traffic to the metadata IP from any process on the host — including the Hub's own ADC credential requests.

This creates a circular dependency:
1. Hub's GCP SDK (ADC) tries `169.254.169.254` to refresh its token
2. iptables redirects to `localhost:18380` (agent's scion metadata sidecar)
3. Scion metadata sidecar calls Hub API `/api/v1/agent/gcp-token`
4. Hub calls GCP IAM `GenerateAccessToken` which needs ADC credentials
5. ADC tries `169.254.169.254` again → intercepted → circular timeout

### Why it would appear to work for hours then break "suddenly"

The Hub initializes its IAM credentials client at startup, before any agents exist. The GCP Go SDK caches access tokens internally (~3600s / 1 hour). Agents start afterward and add iptables rules. The Hub doesn't notice because its SDK is using cached credentials. When the SDK's internal token expires and it tries to refresh (after ~55-60 minutes), the refresh hits the iptables rule and fails. All agents lose GCP access simultaneously.

The apparent "randomness" comes from jitter in the SDK's refresh timing and which IAM call happens to be first after expiry.

### Diagnostic commands

```bash
# Check for leaked REDIRECT rules in host nat OUTPUT chain
sudo iptables -t nat -L OUTPUT -n --line-numbers

# Check for REJECT rules in host filter OUTPUT chain (block mode)
sudo iptables -L OUTPUT -n --line-numbers

# Count rules targeting the metadata IP
sudo iptables -t nat -L OUTPUT -n | grep -c 18380

# Emergency fix: remove the redirect (repeat until all copies gone)
sudo iptables -t nat -D OUTPUT -d 169.254.169.254 -p tcp --dport 80 -j REDIRECT --to-port 18380

# Verify real metadata server is reachable after cleanup
curl -s -H "Metadata-Flavor: Google" \
  "http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token" | head -c 100
```

### Status

**Not confirmed.** Running the diagnostic commands on the host did not show leaked iptables rules at the time of investigation. This could mean:
- The rules were already cleaned up (containers restarted or stopped)
- The containers were not using host networking at the time
- The root cause is something else

The hypothesis remains plausible for future occurrences — the code path exists and is unguarded.

## Hypothesis 2: Token caching thundering herd (secondary)

Independently of the iptables issue, the metadata sidecar has a missing singleflight bug. The struct declares fields for deduplication (`server.go:104-106`) but they are never used:

```go
// Declared but unused
fetchMu       sync.Mutex
fetchInFlight bool
fetchDone     chan struct{}
```

When a cached token expires (remaining < 60s), every concurrent request independently calls `fetchAccessToken()`, each making a full HTTP POST to the Hub. The Hub then fans these out to the GCP IAM API.

With 20 agents, each potentially making concurrent token requests, this could produce bursts of IAM API calls. While unlikely to cause a total auth failure on its own (the hub-side rate limiter at 1 req/sec burst 10 per agent would throttle), it could contribute to rate limiting at the GCP IAM API level.

## Suggested Code Fixes

### Fix 1: Skip iptables in host network mode

The `SCION_NETWORK_MODE` env var is already set to `"host"` in the container (`run.go:661`). The metadata server should check it:

```go
// In configureMetadataInterception:
if os.Getenv("SCION_NETWORK_MODE") == "host" {
    log.Info("Skipping iptables metadata interception: host network mode " +
        "(GCE_METADATA_HOST env var provides interception)")
    return
}
```

The `GCE_METADATA_HOST` / `GCE_METADATA_ROOT` env vars (set at `start_context.go:300-303`) are already the primary interception mechanism for GCP SDKs. iptables is defense-in-depth and is actively harmful in host network mode.

### Fix 2: Implement singleflight for token fetches

Use the already-declared `fetchMu`/`fetchInFlight`/`fetchDone` fields so that concurrent cache misses coalesce into a single upstream call.

## Related Code Locations

| File | Lines | Description |
|------|-------|-------------|
| `pkg/sciontool/metadata/server.go` | 387-417 | `handleToken` — cache check + fetch, no singleflight |
| `pkg/sciontool/metadata/server.go` | 448-486 | `fetchAccessToken` — HTTP call to Hub |
| `pkg/sciontool/metadata/server.go` | 104-106 | Unused singleflight fields |
| `pkg/sciontool/metadata/server.go` | 534-564 | `proactiveRefreshLoop` |
| `pkg/sciontool/metadata/iptables.go` | 31-52 | `setupIPTablesRedirect` — no UID filter |
| `pkg/runtime/common.go` | 538-573 | `ResolveDockerNetworking` — returns "host" for localhost hub |
| `pkg/agent/run.go` | 655-661 | Sets `SCION_NETWORK_MODE` and `--network=host` |
| `pkg/runtimebroker/start_context.go` | 293-304 | Sets `GCE_METADATA_HOST` env var |
| `pkg/hub/gcp_token_iam.go` | 41-55 | `NewIAMTokenGenerator` — uses ADC + metadata server |
| `pkg/hub/gcp_ratelimit.go` | 22-92 | Per-agent rate limiter (1/sec, burst 10) |
