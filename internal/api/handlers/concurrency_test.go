//go:build integration
package handlers_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestIntegration_Concurrency_Race(t *testing.T) {
	app, _, _ := setupTestApp(t)
	
	const numRequests = 50
	var wg sync.WaitGroup
	wg.Add(numRequests)

	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer wg.Done()
			
			// Hit different endpoints concurrently to stress shared state
			paths := []string{
				"/v1/models",
				"/v1/models/1",
				"/v1/providers",
				"/v1/changes",
			}
			path := paths[id%len(paths)]
			
			req := httptest.NewRequest("GET", path, nil)
			// Use unique keys to bypass rate limits even if SKIP_RATE_LIMIT=0
			req.Header.Set("Authorization", "Bearer test-concurrency-"+fmt.Sprint(id))
			
			resp, err := app.Test(req, 10000)
			if err != nil {
				errors <- fmt.Errorf("client %d: request failed: %v", id, err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("client %d: %s returned %d", id, path, resp.StatusCode)
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrency error: %v", err)
	}
}
