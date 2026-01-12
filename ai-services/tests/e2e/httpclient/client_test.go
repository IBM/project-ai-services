package httpclient

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHTTPClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTP Client Suite")
}

var _ = Describe("HTTP Client", func() {
	var testServer *httptest.Server
	BeforeEach(func() {
		testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			var err error
			switch r.Method {
			case http.MethodGet:
				_, err = w.Write([]byte(`{"status":"ok"}`))
			case http.MethodPost:
				_, err = w.Write([]byte(`{"created":true}`))
			case http.MethodPut:
				_, err = w.Write([]byte(`{"updated":true}`))
			case http.MethodDelete:
				_, err = w.Write([]byte(`{"deleted":true}`))
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)

				return
			}
			Expect(err).NotTo(HaveOccurred())
		}))
		err := os.Setenv("AI_SERVICES_BASE_URL", testServer.URL)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		testServer.Close()
	})

	It("GET works", func() {
		client := NewHTTPClient()
		resp, err := client.Get("/test")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				fmt.Printf("[WARNING] failed to close response body: %v\n", cerr)
			}
		}()
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring("ok"))
	})

	It("POST works", func() {
		client := NewHTTPClient()
		resp, err := client.Post("/test", map[string]string{"a": "b"})
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				fmt.Printf("[WARNING] failed to close response body: %v\n", cerr)
			}
		}()
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring("created"))
	})

	It("PUT works", func() {
		client := NewHTTPClient()
		resp, err := client.Put("/test", map[string]string{"x": "y"})
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				fmt.Printf("[WARNING] failed to close response body: %v\n", cerr)
			}
		}()
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring("updated"))
	})

	It("DELETE works", func() {
		client := NewHTTPClient()
		resp, err := client.Delete("/test")
		Expect(err).NotTo(HaveOccurred())
		defer func() {
			if cerr := resp.Body.Close(); cerr != nil {
				fmt.Printf("[WARNING] failed to close response body: %v\n", cerr)
			}
		}()
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(body)).To(ContainSubstring("deleted"))
	})
})
