package machine

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

const machineSessionCookie = "__Host-kuayle-machine"

var routingKeyPattern = regexp.MustCompile(`^[a-z0-9]{12,32}$`)

type GatewayStore interface {
	GetRoute(context.Context, string, string) (*domain.DevMachine, *domain.DevMachineService, error)
	ConsumeAccessTicket(context.Context, string, string) (*domain.DevMachineAccessTicket, error)
	CreateAccessSession(context.Context, *domain.DevMachineAccessSession) error
	GetAccessSession(context.Context, string, string) (*domain.DevMachineAccessSession, error)
	CreateAccessLog(context.Context, *domain.DevMachineAccessLog) error
	TouchMachineActivity(context.Context, uuid.UUID, time.Time) error
}

type Gateway struct {
	store          GatewayStore
	domain         string
	frontendOrigin string
	sessionTTL     time.Duration
	transport      http.RoundTripper
	demoMode       bool
	isSysAdmin     func(uuid.UUID) bool
}

func NewGateway(store GatewayStore, machineDomain string, sessionTTL time.Duration, frontendURL ...string) (*Gateway, error) {
	domain := strings.Trim(strings.ToLower(machineDomain), ".")
	if !validMachineDomain(domain) {
		return nil, fmt.Errorf("invalid machine domain")
	}
	frontendOrigin := ""
	if len(frontendURL) > 0 {
		var err error
		frontendOrigin, err = normalizeConfiguredOrigin(frontendURL[0])
		if err != nil {
			return nil, err
		}
	}
	gw := &Gateway{
		store: store, domain: domain, frontendOrigin: frontendOrigin, sessionTTL: sessionTTL,
		transport: &http.Transport{
			Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2: true, MaxIdleConns: 200, MaxIdleConnsPerHost: 20,
			IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		},
		isSysAdmin: func(uuid.UUID) bool { return true },
	}
	return gw, nil
}

// SetDemoRestriction enables demo-mode access guard on the gateway.  When
// demoMode is true every ticket exchange and existing session is checked
// against isSysAdmin; non-sysadmin users are rejected.  Call this before
// starting the gateway.
func (g *Gateway) SetDemoRestriction(demoMode bool, isSysAdmin func(uuid.UUID) bool) {
	if isSysAdmin == nil {
		isSysAdmin = func(uuid.UUID) bool { return false }
	}
	g.demoMode = demoMode
	g.isSysAdmin = isSysAdmin
}

func validMachineDomain(value string) bool {
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/:@\x00\r\n\t ") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return false
			}
		}
	}
	return true
}

func normalizeConfiguredOrigin(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid frontend origin")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("invalid frontend origin")
	}
	return scheme + "://" + strings.ToLower(parsed.Host), nil
}

func (g *Gateway) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	started := time.Now()
	host := normalizedHost(request.Host)
	routingKey, serviceType, ok := g.parseHost(host)
	if !ok {
		http.Error(writer, "not found", http.StatusNotFound)
		return
	}
	machine, service, err := g.store.GetRoute(request.Context(), routingKey, serviceType)
	if err != nil {
		log.WithError(err).WithField("event_type", "gateway.route_error").Error("machine route lookup failed")
		http.Error(writer, "gateway unavailable", http.StatusServiceUnavailable)
		return
	}
	if machine == nil || service == nil {
		g.audit(request, nil, nil, nil, "denied", "route_not_found", http.StatusNotFound)
		http.Error(writer, "not found", http.StatusNotFound)
		return
	}
	if machine.Status != domain.DevMachineStatusRunning || machine.DesiredStatus != domain.DevMachineStatusRunning || !machine.ExpiresAt.After(time.Now().UTC()) {
		g.audit(request, machine, service, nil, "denied", "machine_not_running", http.StatusServiceUnavailable)
		http.Error(writer, "machine is not running", http.StatusServiceUnavailable)
		return
	}

	if ticket := request.URL.Query().Get("ticket"); ticket != "" {
		if request.Method != http.MethodGet {
			g.audit(request, machine, service, nil, "denied", "ticket_method", http.StatusMethodNotAllowed)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if service.ServiceType == "terminal" && isWebSocketRequest(request) {
			g.proxyTerminalTicket(writer, request, host, machine, service, ticket, started)
			return
		}
		g.exchangeTicket(writer, request, host, machine, service, ticket)
		return
	}

	session, ok := g.authorize(request, host, machine, service)
	if !ok {
		g.audit(request, machine, service, nil, "denied", "invalid_session", http.StatusUnauthorized)
		http.Error(writer, "unauthorized", http.StatusUnauthorized)
		return
	}
	if g.demoMode && !g.isSysAdmin(session.UserID) {
		g.audit(request, machine, service, &session.UserID, "denied", "demo_user_forbidden", http.StatusForbidden)
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	if !validMachineOrigin(request, host) {
		g.audit(request, machine, service, &session.UserID, "denied", "invalid_origin", http.StatusForbidden)
		http.Error(writer, "forbidden origin", http.StatusForbidden)
		return
	}
	_ = g.store.TouchMachineActivity(request.Context(), machine.ID, time.Now().UTC())
	activityDone := make(chan struct{})
	defer close(activityDone)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-activityDone:
				return
			case <-request.Context().Done():
				return
			case at := <-ticker.C:
				_ = g.store.TouchMachineActivity(context.Background(), machine.ID, at.UTC())
			}
		}
	}()
	upstream, _ := url.Parse(fmt.Sprintf("http://%s:%d", service.InternalHost, service.InternalPort))
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = g.transport
	originalDirector := proxy.Director
	proxy.Director = func(proxyRequest *http.Request) {
		originalDirector(proxyRequest)
		proxyRequest.Host = request.Host
		stripCredentials(proxyRequest.Header)
		proxyRequest.Header.Set("X-Forwarded-Host", request.Host)
		proxyRequest.Header.Set("X-Forwarded-Proto", forwardedProto(request))
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Set("Referrer-Policy", "no-referrer")
		response.Header.Set("X-Content-Type-Options", "nosniff")
		sanitizeUpstreamCookies(response.Header)
		return nil
	}
	statusWriter := &gatewayResponseWriter{ResponseWriter: writer, status: http.StatusOK}
	proxy.ErrorHandler = func(responseWriter http.ResponseWriter, proxyRequest *http.Request, proxyErr error) {
		log.WithFields(log.Fields{"workspace_id": machine.WorkspaceID, "machine_id": machine.ID, "event_type": "gateway.proxy_error"}).WithError(proxyErr).Warn("machine proxy failed")
		http.Error(responseWriter, "machine service unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(statusWriter, request)
	g.audit(request, machine, service, &session.UserID, "allowed", "", statusWriter.status)
	log.WithFields(log.Fields{
		"workspace_id": machine.WorkspaceID, "machine_id": machine.ID, "user_id": session.UserID,
		"event_type": "gateway.request", "service": service.ServiceType, "status": statusWriter.status,
		"duration_ms": time.Since(started).Milliseconds(),
	}).Info("machine request proxied")
}

func (g *Gateway) exchangeTicket(writer http.ResponseWriter, request *http.Request, host string, machine *domain.DevMachine, service *domain.DevMachineService, rawTicket string) {
	if len(rawTicket) != 64 {
		g.audit(request, machine, service, nil, "denied", "invalid_ticket", http.StatusUnauthorized)
		http.Error(writer, "invalid ticket", http.StatusUnauthorized)
		return
	}
	ticket, err := g.store.ConsumeAccessTicket(request.Context(), hashToken(rawTicket), host)
	if err != nil || ticket == nil || ticket.MachineID != machine.ID || ticket.ServiceID != service.ID {
		g.audit(request, machine, service, nil, "denied", "invalid_ticket", http.StatusUnauthorized)
		http.Error(writer, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}
	if g.demoMode && !g.isSysAdmin(ticket.UserID) {
		g.audit(request, machine, service, &ticket.UserID, "denied", "demo_user_forbidden", http.StatusForbidden)
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	rawSession, err := randomToken()
	if err != nil {
		http.Error(writer, "gateway unavailable", http.StatusServiceUnavailable)
		return
	}
	expiresAt := time.Now().UTC().Add(g.sessionTTL)
	if machine.ExpiresAt.Before(expiresAt) {
		expiresAt = machine.ExpiresAt
	}
	session := &domain.DevMachineAccessSession{
		ID: uuid.New(), WorkspaceID: ticket.WorkspaceID, MachineID: ticket.MachineID, ServiceID: ticket.ServiceID,
		UserID: ticket.UserID, TokenHash: hashToken(rawSession), BoundHost: host,
		ExpiresAt: expiresAt,
	}
	if err := g.store.CreateAccessSession(request.Context(), session); err != nil {
		log.WithError(err).WithField("event_type", "gateway.session_create_failed").Error("create machine session")
		http.Error(writer, "gateway unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(writer, &http.Cookie{
		Name: machineSessionCookie, Value: rawSession, Path: "/", Secure: true, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: session.ExpiresAt, MaxAge: int(time.Until(session.ExpiresAt).Seconds()),
	})
	writer.Header().Set("Referrer-Policy", "no-referrer")
	query := request.URL.Query()
	query.Del("ticket")
	cleanURL := request.URL.Path
	if encoded := query.Encode(); encoded != "" {
		cleanURL += "?" + encoded
	}
	g.audit(request, machine, service, &ticket.UserID, "allowed", "ticket_exchanged", http.StatusSeeOther)
	http.Redirect(writer, request, cleanURL, http.StatusSeeOther)
}

func (g *Gateway) proxyTerminalTicket(writer http.ResponseWriter, request *http.Request, host string, machine *domain.DevMachine, service *domain.DevMachineService, rawTicket string, started time.Time) {
	if g.frontendOrigin == "" {
		g.audit(request, machine, service, nil, "denied", "terminal_origin_unconfigured", http.StatusForbidden)
		http.Error(writer, "terminal origin is not configured", http.StatusForbidden)
		return
	}
	origin := requestOrigin(request)
	if origin != g.frontendOrigin {
		g.audit(request, machine, service, nil, "denied", "invalid_origin", http.StatusForbidden)
		http.Error(writer, "forbidden origin", http.StatusForbidden)
		return
	}
	query := request.URL.Query()
	sessionName := query.Get("session")
	workingDirectory := query.Get("cwd")
	if len(rawTicket) != 64 || !validTerminalSessionName(sessionName) || !validTerminalWorkingDirectory(workingDirectory) || request.URL.Path != "/ws" {
		g.audit(request, machine, service, nil, "denied", "invalid_terminal_ticket", http.StatusUnauthorized)
		http.Error(writer, "invalid terminal ticket", http.StatusUnauthorized)
		return
	}
	ticket, err := g.store.ConsumeAccessTicket(request.Context(), terminalTicketHash(rawTicket, origin, sessionName, workingDirectory), host)
	if err != nil || ticket == nil || ticket.MachineID != machine.ID || ticket.ServiceID != service.ID {
		g.audit(request, machine, service, nil, "denied", "invalid_ticket", http.StatusUnauthorized)
		http.Error(writer, "invalid or expired ticket", http.StatusUnauthorized)
		return
	}
	if g.demoMode && !g.isSysAdmin(ticket.UserID) {
		g.audit(request, machine, service, &ticket.UserID, "denied", "demo_user_forbidden", http.StatusForbidden)
		http.Error(writer, "forbidden", http.StatusForbidden)
		return
	}
	_ = g.store.TouchMachineActivity(request.Context(), machine.ID, time.Now().UTC())
	activityDone := make(chan struct{})
	defer close(activityDone)
	go touchActivityUntilDone(activityDone, request.Context(), g.store, machine.ID)

	upstream, _ := url.Parse(fmt.Sprintf("http://%s:%d", service.InternalHost, service.InternalPort))
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.Transport = g.transport
	originalDirector := proxy.Director
	proxy.Director = func(proxyRequest *http.Request) {
		originalDirector(proxyRequest)
		proxyRequest.Host = request.Host
		proxyRequest.URL.Path = "/ws"
		args := url.Values{}
		args.Add("arg", sessionName)
		args.Add("arg", workingDirectory)
		proxyRequest.URL.RawQuery = args.Encode()
		stripCredentials(proxyRequest.Header)
		proxyRequest.Header.Set("X-Forwarded-Host", request.Host)
		proxyRequest.Header.Set("X-Forwarded-Proto", forwardedProto(request))
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Set("Referrer-Policy", "no-referrer")
		response.Header.Set("X-Content-Type-Options", "nosniff")
		sanitizeUpstreamCookies(response.Header)
		return nil
	}
	statusWriter := &gatewayResponseWriter{ResponseWriter: writer, status: http.StatusSwitchingProtocols}
	proxy.ErrorHandler = func(responseWriter http.ResponseWriter, proxyRequest *http.Request, proxyErr error) {
		log.WithFields(log.Fields{"workspace_id": machine.WorkspaceID, "machine_id": machine.ID, "event_type": "gateway.terminal_proxy_error"}).WithError(proxyErr).Warn("terminal websocket proxy failed")
		http.Error(responseWriter, "terminal unavailable", http.StatusBadGateway)
	}
	proxy.ServeHTTP(statusWriter, request)
	g.audit(request, machine, service, &ticket.UserID, "allowed", "terminal_ticket_exchanged", statusWriter.status)
	log.WithFields(log.Fields{
		"workspace_id": machine.WorkspaceID, "machine_id": machine.ID, "user_id": ticket.UserID,
		"event_type": "gateway.terminal_ws", "service": service.ServiceType, "status": statusWriter.status,
		"duration_ms": time.Since(started).Milliseconds(),
	}).Info("terminal websocket proxied")
}

func (g *Gateway) authorize(request *http.Request, host string, machine *domain.DevMachine, service *domain.DevMachineService) (*domain.DevMachineAccessSession, bool) {
	cookie, err := request.Cookie(machineSessionCookie)
	if err != nil || len(cookie.Value) != 64 {
		return nil, false
	}
	session, err := g.store.GetAccessSession(request.Context(), hashToken(cookie.Value), host)
	if err != nil || session == nil {
		return nil, false
	}
	return session, session.MachineID == machine.ID && session.ServiceID == service.ID
}

func (g *Gateway) parseHost(host string) (string, string, bool) {
	suffix := "." + g.domain
	if !strings.HasSuffix(host, suffix) {
		return "", "", false
	}
	label := strings.TrimSuffix(host, suffix)
	serviceType := "ide"
	if strings.HasSuffix(label, "-browser") {
		serviceType = "browser"
		label = strings.TrimSuffix(label, "-browser")
	} else if strings.HasSuffix(label, "-terminal") {
		serviceType = "terminal"
		label = strings.TrimSuffix(label, "-terminal")
	}
	return label, serviceType, routingKeyPattern.MatchString(label)
}

func (g *Gateway) audit(request *http.Request, machine *domain.DevMachine, service *domain.DevMachineService, userID *uuid.UUID, decision, reason string, status int) {
	accessLog := &domain.DevMachineAccessLog{
		UserID: userID, Decision: decision, Method: request.Method, Path: request.URL.Path,
		ResponseStatus: &status, UserAgent: stringPointer(request.UserAgent()),
	}
	if reason != "" {
		accessLog.Reason = &reason
	}
	if machine != nil {
		accessLog.WorkspaceID = &machine.WorkspaceID
		accessLog.MachineID = &machine.ID
	}
	if service != nil {
		accessLog.ServiceID = &service.ID
	}
	if remoteIP := requestRemoteIP(request); remoteIP != "" {
		accessLog.RemoteIP = &remoteIP
	}
	if err := g.store.CreateAccessLog(request.Context(), accessLog); err != nil {
		log.WithError(err).WithField("event_type", "gateway.audit_failed").Error("persist machine access audit")
	}
}

type gatewayResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *gatewayResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *gatewayResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacking is not supported")
	}
	return hijacker.Hijack()
}

func (w *gatewayResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func stripCredentials(header http.Header) {
	header.Del("Cookie")
	header.Del("Authorization")
	header.Del("Proxy-Authorization")
	for name := range header {
		if strings.HasPrefix(strings.ToLower(name), "x-kuayle-") {
			header.Del(name)
		}
	}
}

func touchActivityUntilDone(done <-chan struct{}, requestContext context.Context, store GatewayStore, machineID uuid.UUID) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-requestContext.Done():
			return
		case at := <-ticker.C:
			_ = store.TouchMachineActivity(context.Background(), machineID, at.UTC())
		}
	}
}

func isWebSocketRequest(request *http.Request) bool {
	return strings.EqualFold(request.Header.Get("Upgrade"), "websocket")
}

func requestOrigin(request *http.Request) string {
	origin, err := url.Parse(strings.TrimSpace(request.Header.Get("Origin")))
	if err != nil || origin.Scheme == "" || origin.Host == "" || origin.User != nil || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
		return ""
	}
	scheme := strings.ToLower(origin.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + strings.ToLower(origin.Host)
}

func terminalTicketHash(rawTicket, frontendOrigin, runtimeSessionName, workingDirectory string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{rawTicket, frontendOrigin, runtimeSessionName, workingDirectory}, "\n")))
	return hex.EncodeToString(hash[:])
}

func validTerminalSessionName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func validTerminalWorkingDirectory(value string) bool {
	if value == "/workspace" || value == "/workspace/tasks" {
		return true
	}
	if !strings.HasPrefix(value, "/workspace/tasks/") || strings.Contains(value, "..") || strings.Contains(value, "//") {
		return false
	}
	name := strings.TrimPrefix(value, "/workspace/tasks/")
	if name == "" || strings.Contains(name, "/") || name == "." || name == ".." {
		return false
	}
	for _, character := range name {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' && character != '.' {
			return false
		}
	}
	return true
}

func validMachineOrigin(request *http.Request, host string) bool {
	upgrade := isWebSocketRequest(request)
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		if !upgrade {
			return true
		}
	}
	rawOrigin := strings.TrimSpace(request.Header.Get("Origin"))
	if rawOrigin == "" {
		return false
	}
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.User != nil || origin.Hostname() == "" || (origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	return normalizedHost(origin.Host) == host && strings.EqualFold(origin.Scheme, forwardedProto(request))
}

func sanitizeUpstreamCookies(header http.Header) {
	values := header.Values("Set-Cookie")
	header.Del("Set-Cookie")
	for _, value := range values {
		cookie, err := http.ParseSetCookie(value)
		if err != nil || cookie.Name == machineSessionCookie || cookie.Domain != "" {
			continue
		}
		header.Add("Set-Cookie", cookie.String())
	}
}

func normalizedHost(host string) string {
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return strings.Trim(strings.ToLower(host), ".")
}

func forwardedProto(request *http.Request) string {
	if request.Header.Get("X-Forwarded-Proto") == "https" || request.TLS != nil {
		return "https"
	}
	return "http"
}

func requestRemoteIP(request *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(request.Header.Get("X-Forwarded-For"), ",")[0]); net.ParseIP(forwarded) != nil {
		return forwarded
	}
	host, _, _ := net.SplitHostPort(request.RemoteAddr)
	if net.ParseIP(host) != nil {
		return host
	}
	return ""
}

func randomToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func hashToken(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

var _ GatewayStore = (*repository.DevMachineRepository)(nil)
