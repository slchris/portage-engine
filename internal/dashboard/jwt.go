package dashboard

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// This is a minimal, dependency-free HS256 JWT implementation. It is used only
// to authenticate dashboard operators against the configured JWT secret; it is
// deliberately small and does not aim to support the full JWT spec.

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

var (
	errMalformedToken = errors.New("malformed token")
	errBadSignature   = errors.New("invalid token signature")
	errExpiredToken   = errors.New("token expired")
	errWrongAlg       = errors.New("unexpected signing algorithm")
)

// signToken issues an HS256 token for subject valid for ttl, signed with secret.
func signToken(secret, subject string, now time.Time, ttl time.Duration) (string, error) {
	header := jwtHeader{Alg: "HS256", Typ: "JWT"}
	claims := jwtClaims{
		Subject:   subject,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signingInput := b64(headerJSON) + "." + b64(claimsJSON)
	sig := sign(secret, signingInput)
	return signingInput + "." + sig, nil
}

// verifyToken validates the signature and expiry of token against secret.
func verifyToken(secret, token string, now time.Time) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errMalformedToken
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSig := sign(secret, signingInput)
	// Constant-time comparison of the base64 signatures.
	if !hmac.Equal([]byte(expectedSig), []byte(parts[2])) {
		return errBadSignature
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return errMalformedToken
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return errMalformedToken
	}
	if header.Alg != "HS256" {
		return errWrongAlg
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errMalformedToken
	}
	var claims jwtClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return errMalformedToken
	}

	if now.Unix() >= claims.ExpiresAt {
		return errExpiredToken
	}

	return nil
}

func sign(secret, input string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func b64(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
