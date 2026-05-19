//go:build fixture

package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
)

func UsedImports(ctx context.Context, payload map[string]string) string {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(payload)
	fmt.Fprint(io.Discard, ctx.Err(), math.Max(1, 2), http.MethodGet, strings.TrimSpace(buf.String()))
	return buf.String()
}
