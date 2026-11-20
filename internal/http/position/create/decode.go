package create

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	api "github.com/Gtollm/TripGO/internal/generated"
)

const maxRequestBody = 1 << 20

func decodeCreatePosition(w http.ResponseWriter, r *http.Request) (api.PositionData, error) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		return api.PositionData{}, err
	}

	var top map[string]json.RawMessage
	if err := decodeOne(payload, &top); err != nil {
		return api.PositionData{}, err
	}

	if err := allowedKeys(top, "coordinates", "recorded_at"); err != nil {
		return api.PositionData{}, err
	}

	var coords map[string]json.RawMessage
	if err := json.Unmarshal(top["coordinates"], &coords); err != nil {
		return api.PositionData{}, fmt.Errorf("coordinates: %w", err)
	}

	if err := allowedKeys(coords, "latitude", "longitude"); err != nil {
		return api.PositionData{}, fmt.Errorf("coordinates: %w", err)
	}

	var body api.PositionData
	if err := decodeOne(payload, &body); err != nil {
		return api.PositionData{}, err
	}

	return body, nil
}

func allowedKeys(fields map[string]json.RawMessage, required ...string) error {
	allowed := make(map[string]struct{}, len(required))
	for _, key := range required {
		allowed[key] = struct{}{}
		if raw, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("%s is required", key)
		}
	}

	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("%s is not allowed", key)
		}
	}

	return nil
}

func decodeOne(payload []byte, dest any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single json value")
	}

	return nil
}
