// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package cdp

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// tokenExpiryBuffer is subtracted from the token expiry so we refresh early.
const tokenExpiryBuffer = 5 * time.Minute

// TokenProvider manages Auth0 M2M access tokens using private key JWT
// (client assertion). Tokens are cached in-process via oauth2.ReuseTokenSource
// with a 5-minute early-expiry buffer.
type TokenProvider struct {
	tokenSource oauth2.TokenSource
}

// TokenProviderConfig holds the configuration for creating a TokenProvider.
type TokenProviderConfig struct {
	IssuerBaseURL    string
	ClientID         string
	Audience         string
	Scopes           string // space-separated scopes to request
	PrivateKeyBase64 string
}

// NewTokenProvider creates a TokenProvider by decoding the base64 private key.
func NewTokenProvider(cfg TokenProviderConfig) (*TokenProvider, error) {
	privateKey, err := decodePrivateKey(cfg.PrivateKeyBase64)
	if err != nil {
		return nil, err
	}

	tokenURL := strings.TrimRight(cfg.IssuerBaseURL, "/") + "/oauth/token"

	var scopes []string
	if cfg.Scopes != "" {
		scopes = strings.Split(cfg.Scopes, " ")
	}

	src := &assertionTokenSource{
		clientID:   cfg.ClientID,
		audience:   cfg.Audience,
		tokenURL:   tokenURL,
		scopes:     scopes,
		privateKey: privateKey,
	}

	return &TokenProvider{
		tokenSource: oauth2.ReuseTokenSourceWithExpiry(nil, src, tokenExpiryBuffer),
	}, nil
}

// Token returns a valid access token, using the cache when possible.
func (tp *TokenProvider) Token(ctx context.Context) (string, error) {
	tok, err := tp.tokenSource.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// assertionTokenSource implements oauth2.TokenSource by signing a fresh
// client_assertion JWT and exchanging it via the client_credentials grant.
type assertionTokenSource struct {
	clientID   string
	audience   string
	tokenURL   string
	scopes     []string
	privateKey *rsa.PrivateKey
}

func (s *assertionTokenSource) Token() (*oauth2.Token, error) {
	assertion, err := s.signAssertion()
	if err != nil {
		return nil, fmt.Errorf("sign client assertion: %w", err)
	}

	cfg := clientcredentials.Config{
		ClientID:  s.clientID,
		TokenURL:  s.tokenURL,
		Scopes:    s.scopes,
		AuthStyle: oauth2.AuthStyleInParams,
		EndpointParams: url.Values{
			"audience":              {s.audience},
			"client_assertion_type": {"urn:ietf:params:oauth:client-assertion-type:jwt-bearer"},
			"client_assertion":      {assertion},
		},
	}

	tok, err := cfg.Token(context.Background())
	if err != nil {
		slog.Error("Auth0 token request failed", "error", err, "audience", s.audience)
		return nil, err
	}

	slog.Debug("token refreshed",
		"audience", s.audience,
		"expires", tok.Expiry,
	)

	return tok, nil
}

func (s *assertionTokenSource) signAssertion() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    s.clientID,
		Subject:   s.clientID,
		Audience:  jwt.ClaimStrings{s.tokenURL},
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        uuid.NewString(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(s.privateKey)
}

func decodePrivateKey(b64 string) (*rsa.PrivateKey, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode private key base64: %w", err)
	}

	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in decoded private key")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return nil, fmt.Errorf("parse PKCS8 private key: %w", parseErr)
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS8 key is not RSA")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}
}
