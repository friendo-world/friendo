package network

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// rfc3339Z is the timestamp format used across the network state stores.
const rfc3339Z = "2006-01-02T15:04:05Z"

func nowRFC() string { return time.Now().UTC().Format(rfc3339Z) }

// newToken returns a hex-encoded random token with nBytes of entropy.
func newToken(nBytes int) string {
	b := make([]byte, nBytes)
	rand.Read(b)
	return hex.EncodeToString(b)
}
