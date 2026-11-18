package create

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/Gtollm/TripGO/internal/model"
)

func requestHash(t model.Trip) string {
	fields := []string{
		t.UserID.String(),
		t.DriverID.String(),
		formatCoordinate(t.Start.Latitude),
		formatCoordinate(t.Start.Longitude),
		formatCoordinate(t.End.Latitude),
		formatCoordinate(t.End.Longitude),
		strconv.FormatInt(t.Price, 10),
	}

	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}

func formatCoordinate(value float64) string {
	if value == 0 {
		return "0"
	}

	return strconv.FormatFloat(value, 'g', -1, 64)
}
