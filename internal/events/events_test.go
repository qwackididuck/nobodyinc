package events_test

import (
	"testing"

	"github.com/qwackididuck/nobodyinc/internal/events"
)

func TestBusDeliversToEverySubscriber(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	a, stopA := bus.Subscribe()
	b, stopB := bus.Subscribe()

	defer stopA()
	defer stopB()

	bus.Publish(events.Event{Type: events.OrgChanged})

	for name, ch := range map[string]<-chan events.Event{"a": a, "b": b} {
		if e := <-ch; e.Type != events.OrgChanged {
			t.Errorf("subscriber %s got %q, want %q", name, e.Type, events.OrgChanged)
		}
	}
}

func TestBusLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// act runs after the subscription and returns the subscriber's channel.
		act func(bus *events.Bus, ch <-chan events.Event, stop func())
	}{
		{
			name: "unsubscribe closes the channel and stops delivery",
			act: func(bus *events.Bus, _ <-chan events.Event, stop func()) {
				stop()
				stop() // twice is harmless
				bus.Publish(events.Event{Type: events.Hello})
			},
		},
		{
			name: "close ends every subscription",
			act: func(bus *events.Bus, _ <-chan events.Event, _ func()) {
				bus.Close()
				bus.Close()
			},
		},
		{
			name: "a subscriber that falls behind is dropped, the publisher never blocks",
			act: func(bus *events.Bus, ch <-chan events.Event, _ func()) {
				for range 10_000 {
					bus.Publish(events.Event{Type: events.TicketsChanged})
				}
				// Drain what was buffered before the drop.
				for range len(ch) {
					<-ch
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bus := events.NewBus()
			ch, stop := bus.Subscribe()
			tt.act(bus, ch, stop)

			if _, open := <-ch; open {
				t.Fatal("channel still open")
			}
		})
	}
}

func TestSubscribeAfterCloseIsClosed(t *testing.T) {
	t.Parallel()

	bus := events.NewBus()
	bus.Close()

	ch, stop := bus.Subscribe()
	defer stop()

	if _, open := <-ch; open {
		t.Fatal("subscription to a closed bus should be closed")
	}
}

func TestNilBusIgnoresEverything(t *testing.T) {
	t.Parallel()

	var bus *events.Bus

	bus.Publish(events.Event{Type: events.Hello})
	bus.Close()
}
