package sitemap

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
)

// testServer creates a test server with a custom request handler that serves static files and dynamically replaces
// the "HOST" string in the response with the value of the request's Host header. The server handles the following routes:
//   - "/" returns a 404 Not Found response.
//   - "/example" returns a 200 OK response with the content "example content".
//   - other routes serve static files located in the "./test" directory. If a file is gzip-encoded, it will be decompressed,
//     and if it contains the "HOST" string, it will be replaced with the request's Host value. The modified response will be
//     sent back to the client.
//
// It returns an httptest.Server instance, which can be used to make HTTP requests to the test server.
func testServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.RequestURI == "/" {
			// index page is always not found
			http.NotFound(w, r)
			return
		}
		if r.RequestURI == "/example" {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "example content")
			return
		}

		res, err := os.ReadFile("./test" + r.RequestURI)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		strRes := string(res)
		if strings.Contains(strRes, "\x1f\x8b\x08") {
			s := &S{}
			resUncompressed, err := s.unzip(res)
			if err != nil {
				_, _ = fmt.Fprintf(w, "error: %v\n", err)
				return
			}
			strRes = strings.Replace(string(resUncompressed), "HOST", r.Host, -1)

			resCompressed, err := s.zip([]byte(strRes))
			if err != nil {
				_, _ = fmt.Fprintf(w, "error: %v\n", err)
				return
			}
			strRes = string(resCompressed)
		} else {
			strRes = strings.Replace(strRes, "HOST", r.Host, -1)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, strRes)
	}))
}
