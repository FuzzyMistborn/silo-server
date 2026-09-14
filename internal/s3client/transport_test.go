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
	}
	var mu sync.Mutex
	connections := make(map[net.Conn]*connectionState)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	srv.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		if state == http.StateNew {
			connections[conn] = &connectionState{}
			return
		}
		cs := connections[conn]
		if cs == nil {
			return
		}
		if state == http.StateActive {
			cs.active = true
		}
		if state == http.StateClosed {
			cs.closed = true
		}
	}
	srv.Start()
	defer srv.Close()

	c := NewClient(BucketConfig{Endpoint: srv.URL, Bucket: "b", AccessKey: "k", SecretKey: "s", PathStyle: true, Role: "burst"})
	for round := 0; round < 4; round++ {
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
		wg.Wait()
		time.Sleep(10 * time.Millisecond)
	}

	deadline := time.Now().Add(time.Second)
	for {
		mu.Lock()
		resolved := true
		for _, cs := range connections {
			if !cs.active && !cs.closed {
				resolved = false
				break
			}
		}
		mu.Unlock()
		if resolved || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	if len(connections) > s3MaxConnsPerHost {
		mu.Unlock()
		t.Fatalf("saw %d connections, want at most %d", len(connections), s3MaxConnsPerHost)
	}
	for conn, cs := range connections {
		if !cs.active && !cs.closed {
			t.Errorf("connection %v remained unresolved", conn)
		}
		if !cs.active {
			t.Errorf("connection %v never reached StateActive", conn)
		}
	}
	mu.Unlock()
	srv.CloseClientConnections()
}
