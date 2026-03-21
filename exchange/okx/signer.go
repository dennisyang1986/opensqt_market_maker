package okx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"time"
)

type Signer struct {
	apiKey     string
	secretKey  string
	passphrase string
}

func NewSigner(apiKey, secretKey, passphrase string) *Signer {
	return &Signer{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
	}
}

func (s *Signer) GetAPIKey() string {
	return s.apiKey
}

func (s *Signer) GetPassphrase() string {
	return s.passphrase
}

func (s *Signer) Timestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (s *Signer) Sign(timestamp, method, requestPath, body string) string {
	payload := timestamp + method + requestPath + body
	mac := hmac.New(sha256.New, []byte(s.secretKey))
	mac.Write([]byte(payload))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
