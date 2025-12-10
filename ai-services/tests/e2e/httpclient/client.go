package httpclient

import (
    "bytes"
    "encoding/json"
    "io"
    "net/http"
    "os"
)

type HttpClient struct {
    BaseURL string
    Client  *http.Client
}

func NewHttpClient() *HttpClient {
    baseURL := os.Getenv("API_BASE_URL")
    if baseURL == "" {
        baseURL = "http://localhost:8080"
    }
    return &HttpClient{
        BaseURL: baseURL,
        Client:  &http.Client{},
    }
}

func (c *HttpClient) Get(path string) (*http.Response, error) {
    req, err := http.NewRequest("GET", c.BaseURL+path, nil)
    if err != nil {
        return nil, err
    }
    return c.Client.Do(req)
}

func (c *HttpClient) Post(path string, body interface{}) (*http.Response, error) {
    var buf io.Reader
    if body != nil {
        b, _ := json.Marshal(body)
        buf = bytes.NewBuffer(b)
    }
    req, err := http.NewRequest("POST", c.BaseURL+path, buf)
    if err != nil {
        return nil, err
    }
    req.Header.Set("Content-Type", "application/json")
    return c.Client.Do(req)
}
