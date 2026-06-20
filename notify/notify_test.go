package notify_test

import (
	"context"
	"testing"
	"time"

	"github.com/duncanmwirigi/floodguard-rate-limiter/notify"
)

func TestAfterSensitiveAction_Async(t *testing.T) {
	stub := &notify.StubSender{}
	n := notify.New(stub, nil)

	n.AfterSensitiveAction(context.Background(), "acct-1", "withdraw", "fp-new", "10.0.0.1")

	deadline := time.After(500 * time.Millisecond)
	for stub.Count() == 0 {
		select {
		case <-deadline:
			t.Fatal("notification not delivered")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}
