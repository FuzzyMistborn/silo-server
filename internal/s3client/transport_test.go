package s3client

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSharedTransportCapsConnections(t *testing.T) {
	tr := sharedTransport()
	if tr.MaxConnsPerHost <= 0 || tr.MaxConnsPerHost != tr.MaxIdleConnsPerHost {
		t.Fatalf("connection caps = %d/%d", tr.MaxConnsPerHost, tr.MaxIdleConnsPerHost)
	}
	a := NewClient(BucketConfig{Role: "a"})
	b := NewClient(BucketConfig{Role: "b"})
	ta := a.s3Client.Options().HTTPClient.(observedHTTPClient).inner.(*http.Client).Transport
	tb := b.s3Client.Options().HTTPClient.(observedHTTPClient).inner.(*http.Client).Transport
	if ta != tb {
		t.Fatal("clients do not share transport")
	}
}

func TestBurstNeverClosesUnusedConnections(t *testing.T) {
	type connectionState struct {
		active bool
		closed bool
		state  http.ConnState
	}
	var mu sync.Mutex
	connections := make(map[net.Conn]*connectionState)
	inHandler := 0
	release := make(chan struct{})
	changed := make(chan struct{}, 1)
	notify := func() {
		select {
		case changed <- struct{}{}:
		default:
		}
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			mu.Lock()
			ok := cond()
			mu.Unlock()
			if ok {
				return
			}
			select {
			case <-changed:
			case <-deadline:
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}

	// Every handler parks until the test releases the round, so the cap's
	// worth of connections are provably busy at the same time and the rest of
	// the burst has to queue for one instead of dialing.
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inHandler++
		gate := release
		mu.Unlock()
		notify()
		<-gate
		w.WriteHeader(http.StatusOK)
	}))
	srv.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		mu.Lock()
		cs := connections[conn]
		if cs == nil {
			cs = &connectionState{}
			connections[conn] = cs
		}
		cs.state = state
		switch state {
		case http.StateActive:
			cs.active = true
		case http.StateClosed, http.StateHijacked:
			cs.closed = true
		}
		mu.Unlock()
		notify()
	}
	srv.Start()
	defer srv.Close()

	c := NewClient(BucketConfig{Endpoint: srv.URL, Bucket: "b", AccessKey: "k", SecretKey: "s", PathStyle: true, Role: "burst"})
	for round := 0; round < 4; round++ {
		mu.Lock()
		inHandler = 0
		release = make(chan struct{})
		gate := release
		mu.Unlock()

		var wg sync.WaitGroup
		for i := 0; i < 3*s3MaxConnsPerHost; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := c.ObjectExists(t.Context(), "b", "object"); err != nil {
					t.Errorf("ObjectExists: %v", err)
				}
			}()
		}
		waitFor("the cap's worth of requests to be in flight", func() bool { return inHandler >= s3MaxConnsPerHost })
		mu.Lock()
		open := 0
		for _, cs := range connections {
			if !cs.closed {
				open++
			}
		}
		mu.Unlock()
		// Release before reporting so a failure never leaves handlers parked
		// and the server's shutdown waiting on them.
		close(gate)
		wg.Wait()
		if open != s3MaxConnsPerHost {
			t.Fatalf("round %d: %d open connections with %d requests parked, want exactly %d", round, open, s3MaxConnsPerHost, s3MaxConnsPerHost)
		}
		// Let every connection return to the idle pool so the next round races
		// pending dials against a full pool, which is where surplus dials came from.
		waitFor("all connections to go idle", func() bool {
			for _, cs := range connections {
				if !cs.closed && cs.state != http.StateIdle {
					return false
				}
			}
			return true
		})
	}

	mu.Lock()
	defer mu.Unlock()
	if len(connections) > s3MaxConnsPerHost {
		t.Fatalf("saw %d connections, want at most %d", len(connections), s3MaxConnsPerHost)
	}
	for conn, cs := range connections {
		if !cs.active {
			t.Errorf("connection %v was dialed but never carried a request", conn.RemoteAddr())
		}
	}
}
