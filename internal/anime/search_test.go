package anime

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnimeSearchCoalescesAndSurvivesReaderCancellation(t *testing.T) {
	var requests atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-release:
			w.Write([]byte("snapshot"))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	owner, cancelOwner := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelOwner()
	client := &Client{Context: owner, HTTP: server.Client()}
	reader, cancelReader := context.WithCancel(owner)
	first := make(chan error, 1)
	go func() { _, err := client.searchRequest(reader, "GET", server.URL, nil); first <- err }()
	select {
	case <-entered:
	case <-owner.Done():
		t.Fatal("request did not start")
	}
	cancelReader()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	results := make(chan error, 20)
	for range 20 {
		go func() {
			data, err := client.searchRequest(owner, "GET", server.URL, nil)
			if err == nil && string(data) != "snapshot" {
				err = errors.New("lost shared result")
			}
			results <- err
		}()
	}
	close(release)
	for range 20 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatal("duplicate upstream requests", requests.Load())
	}
}
