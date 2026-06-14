package util

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func GetGeolocation(ip string) (string, error) {
	resp, err := http.Get("https://ipapi.co/" + ip + "/json/")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var location map[string]interface{}
	if err := json.Unmarshal(body, &location); err != nil {
		return "", err
	}

	country, ok := location["country_name"].(string)
	if !ok {
		return "", fmt.Errorf("country_name not found")
	}

	return country, nil
}
