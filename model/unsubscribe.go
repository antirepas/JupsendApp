package model

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"emailtracker.com/config"
)

func unsubscribeSecret() []byte {
	secret := config.SessionSecret
	if secret == "" {
		secret = "dev-insecure-unsubscribe-key"
	}
	return []byte(secret)
}

func UnsubscribeToken(userID, contactID int64) string {
	payload := fmt.Sprintf("%d:%d", userID, contactID)
	mac := hmac.New(sha256.New, unsubscribeSecret())
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig))
}

func VerifyUnsubscribeToken(token string) (userID, contactID int64, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return 0, 0, errors.New("invalid token")
	}
	parts := strings.SplitN(string(raw), ":", 3)
	if len(parts) != 3 {
		return 0, 0, errors.New("invalid token")
	}
	userID, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil || userID <= 0 {
		return 0, 0, errors.New("invalid token")
	}
	contactID, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil || contactID <= 0 {
		return 0, 0, errors.New("invalid token")
	}
	payload := parts[0] + ":" + parts[1]
	mac := hmac.New(sha256.New, unsubscribeSecret())
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return 0, 0, errors.New("invalid token")
	}
	return userID, contactID, nil
}

func UnsubscribeURL(baseURL string, userID, contactID int64) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = config.BaseURL
	}
	return baseURL + "/u/" + UnsubscribeToken(userID, contactID)
}
