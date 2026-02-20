# Security Documentation

alfred-ai implements multiple layers of security to protect sensitive data and prevent common vulnerabilities.

## Table of Contents

- [Encryption](#encryption)
- [Secret Scanning](#secret-scanning)
- [Key Rotation](#key-rotation)
- [SSRF Protection](#ssrf-protection)
- [Sandbox Execution](#sandbox-execution)
- [Context Window Guard](#context-window-guard)
- [Rate Limiting](#rate-limiting)
- [Audit Logging](#audit-logging)
- [Security Best Practices](#security-best-practices)

---

## Encryption

### Content Encryption (AES-256-GCM)

alfred-ai uses **AES-256-GCM** (Galois/Counter Mode) for encrypting sensitive content. This provides both **confidentiality** and **integrity** protection.

**Implementation:** `internal/security/encryption.go`

#### Key Features

- **Algorithm:** AES-256-GCM (authenticated encryption)
- **Key Derivation:** Argon2id (memory-hard, GPU/ASIC resistant)
- **Random Nonce:** Each encryption operation uses a unique random nonce
- **Thread-Safe:** Concurrent encryption/decryption is safe
- **Key Rotation:** Supports key rotation via `Rotate()` method with secure zeroization

#### Encryption Salt Strategy

**IMPORTANT: Ephemeral Encryption Model**

The `AESContentEncryptor` uses an **ephemeral salt strategy**:

```go
func NewAESContentEncryptor(passphrase string) (*AESContentEncryptor, error) {
    // Random salt generated on each initialization
    salt := make([]byte, 16)
    if _, err := io.ReadFull(rand.Reader, salt); err != nil {
        return nil, fmt.Errorf("generate salt: %w", err)
    }

    key := deriveContentKey(passphrase, salt)
    return &AESContentEncryptor{key: key, salt: salt}, nil
}
```

**Implications:**

1. **Per-Session Encryption:** Each bot session generates a new random salt
2. **Non-Persistent Salt:** The salt is **not saved to disk**
3. **Content Lifecycle:** Encrypted content can only be decrypted within the same session
4. **Session Restart Behavior:** After bot restart, previously encrypted content **cannot be decrypted** (different salt = different key)

**Use Case:**

This design is suitable for:
- ✅ In-memory secrets during bot runtime
- ✅ Temporary sensitive data (API keys, tokens) within a session
- ✅ Privacy-sensitive conversation content (auto-deleted on restart)

**NOT suitable for:**
- ❌ Long-term data persistence across restarts
- ❌ Database encryption requiring data recovery

**Alternative for Persistent Encryption:**

If you need to decrypt data across bot restarts, you must:
1. **Persist the salt** alongside encrypted data, OR
2. Use a **fixed salt** (less secure - enables rainbow table attacks if passphrase is weak), OR
3. Implement a **key management system** (e.g., HashiCorp Vault, AWS KMS)

**Example - Persistent Salt Modification:**

```go
type PersistentAESEncryptor struct {
    *AESContentEncryptor
    saltFilePath string
}

func NewPersistentAESEncryptor(passphrase, saltFile string) (*PersistentAESEncryptor, error) {
    // Try to load existing salt from file
    salt, err := os.ReadFile(saltFile)
    if err != nil || len(salt) != 16 {
        // Generate new salt and save
        salt = make([]byte, 16)
        io.ReadFull(rand.Reader, salt)
        os.WriteFile(saltFile, salt, 0600)
    }

    key := deriveContentKey(passphrase, salt)
    return &PersistentAESEncryptor{
        AESContentEncryptor: &AESContentEncryptor{key: key, salt: salt},
        saltFilePath:        saltFile,
    }, nil
}
```

**Security Note:** Persisting the salt reduces security to the strength of the passphrase. Always use a strong passphrase (minimum 20 characters, high entropy).

#### Argon2id Parameters

```go
// Default parameters (internal/security/encryption.go)
argon2.IDKey(
    []byte(passphrase),
    salt,
    1,        // time=1 iteration (fast for interactive use)
    64*1024,  // memory=64 MB (resist GPU attacks)
    4,        // threads=4 (parallel execution)
    32,       // keyLen=32 bytes (AES-256)
)
```

**Rationale:**
- **Time=1:** Optimized for real-time encryption during bot operations
- **Memory=64MB:** Balance between security (GPU resistance) and resource usage
- **Threads=4:** Utilize modern multi-core CPUs

**Tuning:** For higher security in low-resource environments, increase `time` parameter. For servers with more RAM, increase `memory` parameter.

#### Key Zeroization

Keys are securely zeroized from memory when:
- `Rotate()` is called (old key is zeroed before replacement)
- `Zeroize()` is explicitly called during shutdown

```go
func (e *AESContentEncryptor) Zeroize() {
    e.mu.Lock()
    defer e.mu.Unlock()
    for i := range e.key {
        e.key[i] = 0
    }
}
```

**Best Practice:** Always call `defer encryptor.Zeroize()` when the encryptor is no longer needed.

---

## Secret Scanning

The **SecretScanner** detects leaked secrets in messages and memory content, preventing accidental exposure of API keys, tokens, and private keys.

**Implementation:** `internal/security/secret_scanner.go`

### Actions

When a secret pattern is matched, one of three actions is taken:

| Action | Behavior |
|--------|----------|
| `redact` | Replace the matched text with `[REDACTED:<pattern-name>]` |
| `warn` | Log a warning but pass the content through unchanged |
| `block` | Reject the entire message |

### Default Patterns

The scanner ships with 5 built-in patterns:

| Pattern | Regex | Default Action |
|---------|-------|----------------|
| AWS Access Key | `AKIA[0-9A-Z]{16}` | `redact` |
| GitHub Token | `ghp_[a-zA-Z0-9]{36}` | `redact` |
| OpenAI Key | `sk-[a-zA-Z0-9]{20,}` | `redact` |
| Private Key Header | `-----BEGIN (RSA \|EC )?PRIVATE KEY-----` | `block` |
| Generic API Key | `(?i)(api[_-]?key\|apikey)\s*[:=]\s*['"]?([a-zA-Z0-9]{20,})` | `warn` |

### Custom Patterns

Add custom patterns via your config YAML. Custom patterns are appended after defaults:

```yaml
security:
  secret_scanning:
    enabled: true
    custom_patterns:
      - name: "Internal Token"
        pattern: "itk_[a-zA-Z0-9]{32}"
        action: "redact"
```

### Integration

The scanner is applied in the router to LLM outputs via `Apply()`, which returns the cleaned text, a blocked flag, and all matches. This ensures secrets are caught before being relayed to channels or stored in memory.

```go
scanner := security.NewSecretScanner(customPatterns, logger)

cleaned, blocked, matches := scanner.Apply(llmOutput)
if blocked {
    // Reject the message entirely (e.g., private key detected)
}
// Use cleaned text (secrets redacted)
```

---

## Key Rotation

The **KeyRotator** handles periodic rotation of encryption keys, reducing the blast radius of a compromised key.

**Implementation:** `internal/security/key_rotation.go`

### Architecture

- **`KeyStore` interface** — abstracts key storage with three methods:
  - `CurrentKey(ctx)` — returns the active encryption key
  - `Rotate(ctx)` — generates a new key and replaces the current one
  - `ListExpiring(ctx, within)` — returns keys expiring within a given duration
- **`EncryptorKeyStore`** — wraps the existing `AESContentEncryptor` to implement `KeyStore`, generating a random passphrase on each rotation and calling `encryptor.Rotate()` (which securely zeroizes the old key)
- **`KeyRotator`** — runs a periodic loop calling `Rotate()` at the configured interval

### Configuration

```yaml
security:
  key_rotation:
    enabled: true
    interval: "720h"  # 30 days
```

### Callback Mechanism

Register a callback to be notified after each successful rotation (e.g., to re-encrypt cached data or update downstream services):

```go
rotator := security.NewKeyRotator(keyStore, 720*time.Hour, logger)
rotator.SetOnRotate(func(newKey []byte) {
    // Handle key change — re-encrypt cached data, notify peers, etc.
})
go rotator.Start(ctx)
```

### Manual Rotation

Trigger an immediate rotation outside the scheduled interval:

```go
err := rotator.RotateNow(ctx)
```

---

## SSRF Protection

**Server-Side Request Forgery (SSRF)** protection prevents the bot from making requests to internal/private networks.

**Implementation:** `internal/security/ssrf.go`

### Protection Mechanisms

#### 1. URL Scheme Whitelist

Only `http` and `https` schemes are allowed:

```go
switch strings.ToLower(u.Scheme) {
case "http", "https":
    // OK
default:
    return ErrSSRFBlocked // Rejects file://, gopher://, etc.
}
```

**Blocked schemes:**
- `file://` (local file access)
- `gopher://` (internal service exploitation)
- `dict://`, `ftp://`, etc.

#### 2. Private IP Blocking

All private/reserved IP ranges are blocked:

```go
var privateRanges = []string{
    "10.0.0.0/8",       // Private
    "172.16.0.0/12",    // Private
    "192.168.0.0/16",   // Private
    "127.0.0.0/8",      // Loopback
    "169.254.0.0/16",   // Link-local
    "0.0.0.0/8",        // Current network
    "::1/128",          // IPv6 loopback
    "fc00::/7",         // IPv6 private
    "fe80::/10",        // IPv6 link-local
}
```

#### 3. DNS Rebinding Protection (TOCTOU Defense)

**Attack scenario:**
1. Attacker sets DNS for `evil.com` with TTL=0
2. Validation resolves DNS → returns public IP → ✅ passes
3. HTTP client resolves DNS again → returns `127.0.0.1` → 🔥 SSRF!

**Defense - Custom Dialer:**

```go
func NewSSRFSafeTransport() *http.Transport {
    return &http.Transport{
        DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
            // Resolve DNS ONCE
            ips, _ := net.DefaultResolver.LookupIPAddr(ctx, host)

            // Validate ALL resolved IPs
            for _, ip := range ips {
                if IsPrivateIP(ip.IP) {
                    return nil, ErrSSRFBlocked
                }
            }

            // Connect directly to validated IP (no second DNS lookup)
            return dialer.DialContext(ctx, network,
                net.JoinHostPort(ips[0].IP.String(), port))
        },
    }
}
```

**Key:** DNS resolution and validation happen **atomically** in the same dialer call.

#### 4. IPv4-mapped IPv6 Normalization

Prevents bypassing `127.0.0.0/8` block using `::ffff:127.0.0.1`:

```go
func IsPrivateIP(ip net.IP) bool {
    // Normalize IPv4-mapped IPv6 to IPv4
    if v4 := ip.To4(); v4 != nil {
        ip = v4
    }
    // ... check against private ranges
}
```

### Usage

**For web tools:**

```go
import "alfred-ai/internal/security"

// Validate URL before making request
if err := security.ValidateURL(targetURL); err != nil {
    return fmt.Errorf("SSRF check failed: %w", err)
}

// Use SSRF-safe transport for HTTP client
client := &http.Client{
    Transport: security.NewSSRFSafeTransport(),
    Timeout:   30 * time.Second,
}
```

**Example - web_tool.go:**

See `internal/adapter/tool/web_tool.go` for production implementation.

---

## Sandbox Execution

**Filesystem sandbox** restricts file operations to a designated root directory.

**Implementation:** `internal/security/sandbox.go`

### Features

- **Path Validation:** All file paths are validated before access
- **Symlink Resolution:** `filepath.EvalSymlinks()` prevents symlink escapes
- **Directory Containment:** Operations are restricted to sandbox root
- **Parent Directory Validation:** Handles non-existent file creation safely

### Attack Prevention

#### Path Traversal Prevention

```go
func (s *Sandbox) ValidatePath(requested string) (string, error) {
    abs, _ := filepath.Abs(requested)
    resolved, err := filepath.EvalSymlinks(abs)

    if err != nil {
        // File doesn't exist - validate parent directory
        parent := filepath.Dir(abs)
        resolvedParent, _ := filepath.EvalSymlinks(parent)
        resolved = filepath.Join(resolvedParent, filepath.Base(abs))
    }

    if !s.isWithinRoot(resolved) {
        return "", ErrPathOutsideSandbox
    }
    return resolved, nil
}
```

**Blocked attacks:**
- `../../etc/passwd` (directory traversal)
- `/etc/passwd` (absolute path escape)
- Symlink to `/etc` inside sandbox (symlink escape)

### Usage

```go
sandbox := security.NewSandbox("/app/data")

// Validate before file operations
safePath, err := sandbox.ValidatePath(userProvidedPath)
if err != nil {
    return fmt.Errorf("invalid path: %w", err)
}

// Now safe to use
data, _ := os.ReadFile(safePath)
```

---

## Context Window Guard

The **Context Window Guard** prevents context overflow by monitoring token usage and proactively compressing conversation history before hitting model limits.

**Implementation:** `internal/usecase/context_guard.go`

### How It Works

1. **Token Counting:** After each user message and after each tool result, the guard counts the total tokens in the session
2. **Threshold Check:** Compares token count against `max_tokens × (1 - safety_margin) - reserve_tokens`
3. **Proactive Compression:** If over the threshold, triggers the conversation compressor to summarize older messages
4. **Hard Limit:** If compression fails or is insufficient, returns `ErrContextOverflow` to prevent API errors

### Token Counting Strategy

alfred-ai uses a **hybrid token counter** to accurately estimate token usage across providers:

| Provider | Method | Library |
|----------|--------|---------|
| OpenAI | Tiktoken (exact) | `pkoukk/tiktoken-go` |
| OpenRouter | Tiktoken (exact) | `pkoukk/tiktoken-go` |
| Anthropic | Character-based (`len/3`) | Built-in |
| Gemini | Character-based (`len/3`) | Built-in |
| Ollama | Character-based (`len/3`) | Built-in |

The character-based fallback adds a per-message overhead of 4 tokens to account for message framing.

### Configuration

```yaml
agent:
  context_guard:
    enabled: true
    max_tokens: 128000      # Model's context window size
    reserve_tokens: 1000    # Reserved for the next response
    safety_margin: 0.15     # 15% buffer before threshold
```

**Effective threshold** = `max_tokens × (1 - safety_margin) - reserve_tokens`

With defaults: `128000 × 0.85 - 1000 = 107,800 tokens`

### Security Implications

- **Denial of Service Prevention:** Prevents runaway tool loops from exhausting the context window
- **Cost Control:** Limits token consumption per session by forcing compression
- **Graceful Degradation:** Sessions that exceed limits get a clean error rather than a cryptic API failure
- **Two Check Points:** Guards both user input and tool output paths in the agent loop

### Tuning Guidelines

| Model | Recommended `max_tokens` |
|-------|--------------------------|
| GPT-4o | 128000 |
| GPT-4 Turbo | 128000 |
| GPT-3.5 Turbo | 16385 |
| Claude 3.5 Sonnet | 200000 |
| Gemini Pro | 32000 |

Set `safety_margin` higher (e.g., `0.20`) if your agent uses many tools that return large results.

---

## Rate Limiting

alfred-ai does not include built-in HTTP rate limiting. For production deployments, place a reverse proxy in front of the HTTP channel.

### Recommended Setup

**nginx:**
```nginx
http {
    limit_req_zone $binary_remote_addr zone=alfred:10m rate=10r/s;

    server {
        location /api/ {
            limit_req zone=alfred burst=20 nodelay;
            proxy_pass http://localhost:8080;
        }
    }
}
```

**Caddy:**
```
:443 {
    rate_limit {
        zone dynamic_zone {
            key {remote_host}
            events 10
            window 1s
        }
    }
    reverse_proxy localhost:8080
}
```

### LLM Provider Rate Limits

Most LLM providers enforce their own rate limits. alfred-ai handles provider rate limit errors (HTTP 429) with exponential backoff via the circuit breaker in `internal/adapter/llm/circuitbreaker.go`.

### Gateway Rate Limiting

The Gateway API (WebSocket/SSE streaming) should also be protected:
```yaml
gateway:
  enabled: true
  addr: ":8090"
```

Use your reverse proxy to limit concurrent WebSocket connections per IP.

---

## Audit Logging

Audit logs record security-relevant events for compliance and forensics.

**Implementation:** `internal/security/audit.go`, `internal/domain/audit.go`

### Logged Events

- **LLM API calls** (without prompt/completion content - privacy)
- **Tool executions** (tool name, success/failure - no arguments/results)
- **Authentication events** (if multi-user mode is enabled)
- **Configuration changes**
- **Errors and security violations**
- **Access events** (SOC2/GDPR compliance)
- **Data events** (data creation, modification, deletion, export)
- **Secret detection events** (when SecretScanner finds a match)
- **Session lifecycle** (create/delete)

### Compliance Fields (SOC2/GDPR)

Each `AuditEvent` supports structured compliance fields:

| Field | Description | Example |
|-------|-------------|---------|
| `Actor` | Who performed the action | `"user:alice"`, `"system:cron"` |
| `Resource` | What was acted upon | `"memory:proj-notes"`, `"session:abc123"` |
| `Action` | What was done | `"read"`, `"delete"`, `"export"` |
| `Outcome` | Result of the action | `"success"`, `"denied"`, `"error"` |

### Convenience Methods

Two helper methods simplify logging common compliance events:

```go
// Log an access event (e.g., user read a resource).
logger.LogAccess(ctx, "user:alice", "memory:project-notes", "read", "success")

// Log a data event with arbitrary metadata.
logger.LogDataEvent(ctx, "system:cron", "memory:archive", "export", map[string]string{
    "format": "json",
    "records": "150",
})
```

### Format

```json
{
  "timestamp": "2026-02-13T12:00:00Z",
  "type": "access",
  "actor": "user:alice",
  "resource": "memory:project-notes",
  "action": "read",
  "outcome": "success",
  "detail": {}
}
```

**Privacy:** Message content is **never logged** - only metadata.

### Retention Policy

The `FileAuditLogger` supports programmatic retention enforcement via `SetRetention()` and `EnforceRetention()`:

```yaml
security:
  audit:
    enabled: true
    path: "/var/log/alfredai/audit.jsonl"
    retention:
      max_age: "2160h"   # 90 days
      max_size: "500MB"
```

- **`max_age`** — entries older than this duration are removed
- **`max_size`** — if the log file exceeds this size, oldest entries are trimmed first
- `EnforceRetention()` rewrites the log file in-place, safe to call while the logger is active

### External Rotation

Audit logs also support rotation via external tools (e.g., `logrotate`):

```
/var/log/alfred-ai/audit.jsonl {
    daily
    rotate 90
    compress
    missingok
    notifempty
}
```

---

## Security Best Practices

### For Developers

1. **Never log sensitive data** (API keys, user messages, tool results)
2. **Validate all external input** (LLM outputs, user messages, API responses)
3. **Use context for timeouts** - prevent hanging operations
4. **Zeroize keys on shutdown** - call `encryptor.Zeroize()`
5. **Review tool implementations** - tools have filesystem/network access

### For Operators

1. **Strong passphrases** - minimum 20 characters for encryption
2. **Restrict sandbox directory** - use minimal permissions (0700)
3. **Monitor audit logs** - detect unusual activity
4. **Network isolation** - run bot in isolated network segment if possible
5. **Regular updates** - keep dependencies patched
6. **Enable context guard** - set `agent.context_guard.enabled: true` to prevent context overflow
7. **Use a reverse proxy** - add rate limiting and TLS termination for HTTP/Gateway channels
8. **Set appropriate `max_tokens`** - match your model's actual context window size

### For Tool Authors

1. **Always use Sandbox.ValidatePath()** before file I/O
2. **Always use SSRFSafeTransport** for HTTP requests
3. **Sanitize command arguments** if using shell execution
4. **Return errors instead of panicking** - agent will handle gracefully

---

## Vulnerability Reporting

If you discover a security vulnerability:

1. **DO NOT** open a public GitHub issue
2. Email: security@byterover.com (GPG key available)
3. Include: description, reproduction steps, impact assessment
4. We aim to respond within 48 hours

---

## Security Audit History

| Date | Auditor | Scope | Findings |
|------|---------|-------|----------|
| 2026-02-13 | Internal (Torvalds/Pike/Ivezic review) | Full codebase | 5 critical bugs fixed, SSRF hardened, path traversal prevented |

---

## References

- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [CWE-918: SSRF](https://cwe.mitre.org/data/definitions/918.html)
- [CWE-22: Path Traversal](https://cwe.mitre.org/data/definitions/22.html)
- [NIST Cryptographic Standards](https://csrc.nist.gov/projects/cryptographic-standards-and-guidelines)
