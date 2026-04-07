// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package cdp

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenProvider manages Auth0 M2M access tokens using private key JWT
// (client assertion). Tokens are cached in-process with a 5-minute buffer
// before expiry.
type TokenProvider struct {
	issuerBaseURL string
	clientID      string
	audience      string
	privateKey    *rsa.PrivateKey
	httpClient    *http.Client

	mu          sync.RWMutex
	cachedToken string
	expiresAt   time.Time
}

// TokenProviderConfig holds the configuration for creating a TokenProvider.
type TokenProviderConfig struct {
	IssuerBaseURL       string
	ClientID            string
	Audience            string
	PrivateKeyBase64    string
	HTTPClient          *http.Client
}

// NewTokenProvider creates a TokenProvider by decoding the base64 private key.
func NewTokenProvider(cfg TokenProviderConfig) (*TokenProvider, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(cfg.PrivateKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode private key base64: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in decoded private key")
	}

	var privateKey *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse PKCS8 private key: %w", parseErr)
		}
		var ok bool
		privateKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not RSA")
		}
	default:
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &TokenProvider{
		issuerBaseURL: strings.TrimRight(cfg.IssuerBaseURL, "/"),
		clientID:      cfg.ClientID,
		audience:      cfg.Audience,
		privateKey:    privateKey,
		httpClient:    httpClient,
	}, nil
}

// Token returns a valid access token, using the cache when possible.
func (tp *TokenProvider) Token(ctx context.Context) (string, error) {
	tp.mu.RLock()
	if tp.cachedToken != "" && time.Now().Before(tp.expiresAt) {
		token := tp.cachedToken
		tp.mu.RUnlock()
		return token, nil
	}
	tp.mu.RUnlock()

	return tp.refresh(ctx)
}

func (tp *TokenProvider) refresh(ctx context.Context) (string, error) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	// Double-check after acquiring write lock.
	if tp.cachedToken != "" && time.Now().Before(tp.expiresAt) {
		return tp.cachedToken, nil
	}

	tokenURL := tp.issuerBaseURL + "/oauth/token"

	// Build client assertion JWT.
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    tp.clientID,
		Subject:   tp.clientID,
		Audience:  jwt.ClaimStrings{tokenURL},
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        uuid.NewString(),
	}
	assertion := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedAssertion, err := assertion.SignedString(tp.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign client assertion: %w", err)
	}

	body := fmt.Sprintf(
		"grant_type=client_credentials&client_id=%s&client_assertion_type=%s&client_assertion=%s&audience=%s&scope=%s",
		tp.clientID,
		"urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		signedAssertion,
		tp.audience,
		"read:members read:project-affiliations read:maintainer-roles",
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := tp.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "Auth0 token request failed",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return "", fmt.Errorf("token request returned %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("unmarshal token response: %w", err)
	}

	// Cache with 5-minute buffer before expiry.
	tp.cachedToken = tokenResp.AccessToken
	tp.expiresAt = now.Add(time.Duration(tokenResp.ExpiresIn-300) * time.Second)

	slog.DebugContext(ctx, "CDP token refreshed",
		"expires_in", tokenResp.ExpiresIn,
	)

	return tp.cachedToken, nil
}
