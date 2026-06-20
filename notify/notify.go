// Package notify sends fire-and-forget alerts after sensitive actions from
// unrecognized devices.
package notify

import (
	"context"
	"log"
	"sync"
	"time"
)

// Event describes a sensitive action notification.
type Event struct {
	AccountID         string
	Action            string
	DeviceFingerprint string
	IPAddress         string
	At                time.Time
}

// Sender delivers notifications (email/SMS). Implementations should be non-blocking.
type Sender interface {
	Send(ctx context.Context, e Event) error
}

// Notifier queues sensitive-action alerts asynchronously.
type Notifier struct {
	sender Sender
	log    *log.Logger
	ch     chan Event
	once   sync.Once
}

// New creates a Notifier. If sender is nil, events are logged only.
func New(sender Sender, logger *log.Logger) *Notifier {
	if logger == nil {
		logger = log.Default()
	}
	n := &Notifier{
		sender: sender,
		log:    logger,
		ch:     make(chan Event, 256),
	}
	n.start()
	return n
}

func (n *Notifier) start() {
	n.once.Do(func() {
		go func() {
			for e := range n.ch {
				n.deliver(e)
			}
		}()
	})
}

func (n *Notifier) deliver(e Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if n.sender == nil {
		n.log.Printf("[notify] account=%s action=%s ip=%s device=%s", e.AccountID, e.Action, e.IPAddress, e.DeviceFingerprint)
		return
	}
	if err := n.sender.Send(ctx, e); err != nil {
		n.log.Printf("[notify] delivery failed account=%s action=%s: %v", e.AccountID, e.Action, err)
	}
}

// AfterSensitiveAction enqueues an alert. It never blocks the HTTP response path.
func (n *Notifier) AfterSensitiveAction(ctx context.Context, accountID, action, deviceFingerprint, ipAddress string) {
	e := Event{
		AccountID:         accountID,
		Action:            action,
		DeviceFingerprint: deviceFingerprint,
		IPAddress:         ipAddress,
		At:                time.Now(),
	}
	select {
	case n.ch <- e:
	default:
		n.log.Printf("[notify] queue full, dropping alert account=%s action=%s", accountID, action)
	}
}

// StubSender records sent events for tests.
type StubSender struct {
	mu     sync.Mutex
	Events []Event
	Err    error
}

func (s *StubSender) Send(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Events = append(s.Events, e)
	return s.Err
}

func (s *StubSender) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Events)
}
