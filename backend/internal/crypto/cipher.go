package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const encPrefix = "enc:v1:"

type Cipher struct {
	key []byte
}

func NewCipher(secret string, keyFilePath string) (*Cipher, error) {
	if strings.TrimSpace(secret) != "" {
		sum := sha256.Sum256([]byte(secret))
		return &Cipher{key: sum[:]}, nil
	}

	if err := os.MkdirAll(filepath.Dir(keyFilePath), 0o700); err != nil {
		return nil, fmt.Errorf("create key dir: %w", err)
	}

	raw, err := os.ReadFile(keyFilePath)
	if err == nil {
		decoded, decErr := hex.DecodeString(strings.TrimSpace(string(raw)))
		if decErr == nil && len(decoded) == 32 {
			return &Cipher{key: decoded}, nil
		}
		return nil, errors.New("corrupt master key file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if err := os.WriteFile(keyFilePath, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	return &Cipher{key: key}, nil
}

func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

func (c *Cipher) Decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, encPrefix) {
		return "", errors.New("value is not encrypted with supported prefix")
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encPrefix))
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := payload[:nonceSize], payload[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt ciphertext: %w", err)
	}
	return string(plain), nil
}

func (c *Cipher) SignSession(username string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(username)) + "." + strconv.FormatInt(exp, 10)
	sig := c.computeHMAC(payload)
	return payload + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (c *Cipher) VerifySession(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	payload := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", false
	}
	expected := c.computeHMAC(payload)
	if !hmac.Equal(sig, expected) {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	rawUser, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	return string(rawUser), true
}

func (c *Cipher) computeHMAC(payload string) []byte {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte("session:" + payload))
	return mac.Sum(nil)
}
