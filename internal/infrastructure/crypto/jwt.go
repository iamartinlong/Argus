package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/iamartinlong/Argus/internal/domain"
)

type JWTService struct {
	secret []byte
	ttl    time.Duration
}

func NewJWTService(secret string, ttl time.Duration) (*JWTService, error) {
	if len(secret) < 32 {
		return nil, errors.New("jwt secret 长度至少 32 字符")
	}
	return &JWTService{secret: []byte(secret), ttl: ttl}, nil
}

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func (s *JWTService) IssueAccessToken(user *domain.User) (string, error) {
	now := time.Now()
	claims := claims{
		Role: string(user.Role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *JWTService) VerifyAccessToken(tokenStr string) (*domain.User, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, domain.ErrUnauthorized
	}
	c, ok := token.Claims.(*claims)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	user := &domain.User{ID: c.Subject, Role: domain.Role(c.Role)}
	if !user.Role.Valid() {
		return nil, domain.ErrUnauthorized
	}
	return user, nil
}

func (s *JWTService) IssueRefreshToken() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	raw := hex.EncodeToString(b)
	return raw, hex.EncodeToString(sha256Sum([]byte(raw))), nil
}
