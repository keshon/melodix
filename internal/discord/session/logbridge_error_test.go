package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/rs/zerolog"
)

func TestBridgePreservesErrorText(t *testing.T) {
	err := fmt.Errorf("decode gateway: %w", errors.New("unknown channel type"))
	for _, attached := range []bool{false, true} {
		t.Run(fmt.Sprintf("attached=%t", attached), func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(&bridge{log: zerolog.New(&output)})
			if attached {
				logger.With(slog.Any("err", err)).Error("error while parsing gateway message")
			} else {
				logger.Error("error while parsing gateway message", slog.Any("err", err))
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["err"] != err.Error() {
				t.Fatalf("error text lost: %s", output.String())
			}
			if record["level"] != "error" || record["msg"] != "error while parsing gateway message" || record["message"] != "library_log" {
				t.Fatalf("unexpected log record: %s", output.String())
			}
		})
	}
}
