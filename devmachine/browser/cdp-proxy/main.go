package main

import (
	"crypto/subtle"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

func main() {
	token := os.Getenv("KUAYLE_BROWSER_CDP_TOKEN")
	listenAddress := os.Getenv("KUAYLE_BROWSER_CDP_LISTEN")
	if token == "" || listenAddress == "" {
		log.Fatal("browser CDP token and listen address are required")
	}

	upstream, _ := url.Parse("http://127.0.0.1:9222")
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           newCDPProxyHandler(token, upstream),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}
	log.Fatal(server.ListenAndServe())
}

func newCDPProxyHandler(token string, upstream *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		request.Host = upstream.Host
		request.Header.Del("Authorization")
	}
	proxy.Transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: 5 * time.Second,
	}
	proxy.ErrorHandler = func(response http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(response, "browser debugging endpoint unavailable", http.StatusBadGateway)
	}

	expectedAuthorization := []byte("Bearer " + token)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		providedAuthorization := []byte(request.Header.Get("Authorization"))
		if len(providedAuthorization) != len(expectedAuthorization) || subtle.ConstantTimeCompare(providedAuthorization, expectedAuthorization) != 1 {
			response.Header().Set("WWW-Authenticate", `Bearer realm="browser-cdp"`)
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if request.URL.Path != "/json" || request.URL.RawQuery != "" {
			http.NotFound(response, request)
			return
		}
		proxy.ServeHTTP(response, request)
	})
}
