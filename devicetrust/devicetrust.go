// Package devicetrust tracks known device fingerprints per account to detect
// logins and sensitive actions from unfamiliar contexts.
package devicetrust

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// ErrKeyRequired is returned when accountID or fingerprint is empty.
var ErrKeyRequired = errors.New("devicetrust: account id and fingerprint are required")

// DeviceRecord describes a seen device for an account.
type DeviceRecord struct {
	Fingerprint string
	Trusted     bool
	FirstSeen   time.Time
	LastSeen    time.Time
}

// Store persists device fingerprints per account.
type Store interface {
	Get(ctx context.Context, accountID, fingerprint string) (DeviceRecord, bool, error)
	UpsertSeen(ctx context.Context, accountID, fingerprint string, at time.Time) error
	MarkTrusted(ctx context.Context, accountID, fingerprint string, at time.Time) error
}

// Client extracts device identity from HTTP requests.
type Client struct {
	store Store
}

// New creates a Client backed by store.
func New(store Store) *Client {
	if store == nil {
		store = NewMemoryStore()
	}
	return &Client{store: store}
}

// Fingerprint hashes stable client signals into a device identifier.
// clientDeviceID is a value from a first-party cookie or localStorage set by the frontend.
func Fingerprint(userAgent, acceptLanguage, clientDeviceID string) string {
	normalized := strings.TrimSpace(userAgent) + "|" +
		strings.TrimSpace(acceptLanguage) + "|" +
		strings.TrimSpace(clientDeviceID)
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// FingerprintFromRequest builds a fingerprint from standard headers.
// Pass the client device ID from cookie X-Device-ID or similar.
func FingerprintFromRequest(r *http.Request, clientDeviceID string) string {
	if clientDeviceID == "" {
		clientDeviceID = r.Header.Get("X-Device-ID")
	}
	return Fingerprint(r.UserAgent(), r.Header.Get("Accept-Language"), clientDeviceID)
}

// IsKnownDevice reports whether fingerprint is trusted for accountID.
func (c *Client) IsKnownDevice(ctx context.Context, accountID, fingerprint string) (bool, error) {
	if accountID == "" || fingerprint == "" {
		return false, ErrKeyRequired
	}
	rec, found, err := c.store.Get(ctx, accountID, fingerprint)
	if err != nil {
		return false, err
	}
	return found && rec.Trusted, nil
}

// RecordSeen updates last-seen for a device (call on login).
func (c *Client) RecordSeen(ctx context.Context, accountID, fingerprint string) error {
	if accountID == "" || fingerprint == "" {
		return ErrKeyRequired
	}
	return c.store.UpsertSeen(ctx, accountID, fingerprint, time.Now())
}

// MarkDeviceTrusted marks a device trusted after email/SMS confirmation.
func (c *Client) MarkDeviceTrusted(ctx context.Context, accountID, fingerprint string) error {
	if accountID == "" || fingerprint == "" {
		return ErrKeyRequired
	}
	return c.store.MarkTrusted(ctx, accountID, fingerprint, time.Now())
}

// Lookup returns device metadata for accountID and fingerprint.
func (c *Client) Lookup(ctx context.Context, accountID, fingerprint string) (DeviceRecord, bool, error) {
	if accountID == "" || fingerprint == "" {
		return DeviceRecord{}, false, ErrKeyRequired
	}
	return c.store.Get(ctx, accountID, fingerprint)
}
