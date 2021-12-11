package jwt

import (
	"encoding/json"
	"fmt"

	"github.com/dgrijalva/jwt-go"
	"github.com/trapped/sx/pkg/tricks"
	"gopkg.in/square/go-jose.v2"
)

type Verifier interface {
	Verify(token string) error
}

type SingleKeyVerifier struct {
	key *jose.JSONWebKey
}

func (v *SingleKeyVerifier) Verify(token string) error {
	if v.key == nil {
		return fmt.Errorf("no key configured")
	}
	t, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		return v.key.Key, nil
	})
	if err != nil {
		return fmt.Errorf("JWT is not valid: %v", err)
	}
	if !t.Valid {
		return fmt.Errorf("JWT is not valid")
	}
	return nil
}

func NewSingleKeyVerifier(key string) (*SingleKeyVerifier, error) {
	v := &SingleKeyVerifier{
		key: new(jose.JSONWebKey),
	}
	if err := json.Unmarshal(tricks.StringToBytes(key), v.key); err != nil {
		return nil, fmt.Errorf("key is not a valid JWK: %v", err)
	}
	return v, nil
}
