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

func decodeCreateTrip(w http.ResponseWriter, r *http.Request) (api.TripData, error) {
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		return api.TripData{}, err
	}

	var top map[string]json.RawMessage
	if err := decodeOne(payload, &top); err != nil {
		return api.TripData{}, err
	}

	if err := allowedKeys(top, "user_id", "driver_id", "start_point", "end_point", "price"); err != nil {
		return api.TripData{}, err
	}

	if err := requirePoint(top["start_point"], "start_point"); err != nil {
		return api.TripData{}, err
	}

	if err := requirePoint(top["end_point"], "end_point"); err != nil {
		return api.TripData{}, err
	}

	var body api.TripData
	if err := decodeOne(payload, &body); err != nil {
		return api.TripData{}, err
	}

	return body, nil
}

func requirePoint(raw json.RawMessage, field string) error {
	var point map[string]json.RawMessage
	if err := json.Unmarshal(raw, &point); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}

	if err := allowedKeys(point, "latitude", "longitude"); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}

	return nil
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
