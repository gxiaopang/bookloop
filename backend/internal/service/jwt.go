package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const jwtSigningAlgorithm = "HS256"

type JWTManager struct {
	secret    []byte
	issuer    string
	expiresIn time.Duration
	now       func() time.Time
}

func NewJWTManager(secret, issuer string, expiresIn time.Duration) *JWTManager {
	return &JWTManager{
		secret:    []byte(secret),
		issuer:    issuer,
		expiresIn: expiresIn,
		now:       time.Now,
	}
}

func (m *JWTManager) ExpiresIn() time.Duration {
	return m.expiresIn
}

func (m *JWTManager) Issue(userID uint64) (string, error) {
	issuedAt := m.now().UTC()
	jti, err := newJWTID()
	if err != nil {
		return "", fmt.Errorf("generate JWT ID: %w", err)
	}

	claims := jwt.RegisteredClaims{
		Issuer:    m.issuer,
		Subject:   strconv.FormatUint(userID, 10),
		ExpiresAt: jwt.NewNumericDate(issuedAt.Add(m.expiresIn)),
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ID:        jti,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return signedToken, nil
}

func (m *JWTManager) Verify(rawToken string) (uint64, error) {
	claims := new(jwt.RegisteredClaims)
	token, err := jwt.ParseWithClaims(
		rawToken,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method.Alg() != jwtSigningAlgorithm {
				return nil, ErrUnauthorized
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwtSigningAlgorithm}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithStrictDecoding(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) &&
			!errors.Is(err, jwt.ErrTokenSignatureInvalid) &&
			!errors.Is(err, jwt.ErrTokenInvalidIssuer) &&
			!errors.Is(err, jwt.ErrTokenMalformed) &&
			!errors.Is(err, jwt.ErrTokenUnverifiable) {
			return 0, ErrTokenExpired
		}
		return 0, ErrUnauthorized
	}
	if !token.Valid || claims.Subject == "" || claims.IssuedAt == nil ||
		claims.ExpiresAt == nil || claims.ID == "" || claims.Issuer == "" {
		return 0, ErrUnauthorized
	}

	userID, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || userID == 0 {
		return 0, ErrUnauthorized
	}
	return userID, nil
}

func newJWTID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
