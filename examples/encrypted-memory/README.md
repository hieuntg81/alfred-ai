# Encrypted Memory Example

Full security configuration with encryption, sandboxing, and audit logging.

## Prerequisites

- OpenAI API key
- Encryption passphrase (any secure string)

## Setup

```bash
export ENCRYPTION_KEY=my-secure-passphrase-here
export OPENAI_API_KEY=sk-...
../../alfred-ai --config=config.yaml
```

## Security Features

### 1. Content Encryption
- Memory stored with AES-256-GCM
- Passphrase-based key derivation (Argon2id)
- Protects sensitive conversation data

### 2. Sandbox
- File operations restricted to `./workspace/`
- Prevents path traversal attacks
- Validates all file paths

### 3. Audit Logging
- All actions logged to `./audit.log`
- Tamper-evident event log
- Useful for compliance and debugging

## What's Included

- OpenAI GPT-4 provider
- Encrypted markdown memory
- Sandbox for file operations
- Audit logging enabled
- CLI interface

## Notes

- **IMPORTANT**: Keep your `ENCRYPTION_KEY` secure!
- Loss of encryption key = loss of memory data
- Audit logs are not encrypted (by design)
