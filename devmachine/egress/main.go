package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type proxy struct {
	allow     []string
	deny      []string
	transport *http.Transport
}

var carrierGradeNAT = mustCIDR("100.64.0.0/10")

func main() {
	p := &proxy{allow: domains(os.Getenv("KUAYLE_EGRESS_ALLOWLIST")), deny: domains(os.Getenv("KUAYLE_EGRESS_DENYLIST"))}
	p.transport = &http.Transport{
		Proxy: nil, DialContext: p.dialContext, TLSHandshakeTimeout: 10 * time.Second,
		MaxIdleConns: 100, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second,
	}
	server := &http.Server{Addr: ":3128", Handler: p, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 * 1024}
	log.Fatal(server.ListenAndServe())
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.connect(w, r)
		return
	}
	port := r.URL.Port()
	if port == "" {
		switch strings.ToLower(r.URL.Scheme) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	if (r.URL.Scheme != "http" && r.URL.Scheme != "https") || (port != "80" && port != "443") || !p.allowed(r.URL.Hostname()) {
		http.Error(w, "egress denied", http.StatusForbidden)
		return
	}
	request := r.Clone(r.Context())
	request.RequestURI = ""
	request.Header.Del("Proxy-Authorization")
	removeHopHeaders(request.Header)
	response, err := p.transport.RoundTrip(request)
	if err != nil {
		http.Error(w, "egress unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	removeHopHeaders(response.Header)
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	_, _ = io.Copy(w, response.Body)
}

func (p *proxy) connect(w http.ResponseWriter, r *http.Request) {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil || (port != "443" && port != "80") || !p.allowed(host) {
		http.Error(w, "egress denied", http.StatusForbidden)
		return
	}
	upstream, err := p.dialContext(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, "egress unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "tunneling unavailable", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	_ = buffer.Flush()
	go transfer(upstream, client)
	go transfer(client, upstream)
}

func (p *proxy) dialContext(ctx context.Context, _, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (port != "80" && port != "443") || !p.allowed(host) {
		return nil, fmt.Errorf("egress denied")
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if privateIP(address.IP) {
			continue
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(address.IP.String(), port))
	}
	return nil, fmt.Errorf("no public address for host")
}

func (p *proxy) allowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if net.ParseIP(host) != nil {
		return false
	}
	for _, denied := range p.deny {
		if host == denied || strings.HasSuffix(host, "."+denied) {
			return false
		}
	}
	if len(p.allow) == 0 {
		return true
	}
	for _, allowed := range p.allow {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || carrierGradeNAT.Contains(ip)
}

func mustCIDR(value string) *net.IPNet {
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return network
}

func domains(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(strings.ToLower(item)); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func transfer(destination net.Conn, source net.Conn) {
	defer destination.Close()
	defer source.Close()
	_, _ = io.Copy(destination, bufio.NewReader(source))
}

func removeHopHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Keep-Alive", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}
