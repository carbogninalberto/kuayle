package machine

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type gatewayStoreFake struct {
	machine       *domain.DevMachine
	service       *domain.DevMachineService
	session       *domain.DevMachineAccessSession
	ticket        *domain.DevMachineAccessTicket
	expectedHash  string
	expectedHost  string
	consumedHosts []string
	ticketUsed    bool
	accessLogs    []domain.DevMachineAccessLog
}

func (f *gatewayStoreFake) GetRoute(context.Context, string, string) (*domain.DevMachine, *domain.DevMachineService, error) {
	return f.machine, f.service, nil
}
func (f *gatewayStoreFake) ConsumeAccessTicket(_ context.Context, tokenHash, host string) (*domain.DevMachineAccessTicket, error) {
	if f.expectedHash != "" && tokenHash != f.expectedHash {
		return nil, nil
	}
	if f.expectedHost != "" && host != f.expectedHost {
		return nil, nil
	}
	if f.ticketUsed {
		return nil, nil
	}
	f.ticketUsed = true
	f.consumedHosts = append(f.consumedHosts, host)
	return f.ticket, nil
}
func (f *gatewayStoreFake) CreateAccessSession(_ context.Context, session *domain.DevMachineAccessSession) error {
	f.session = session
	return nil
}
func (f *gatewayStoreFake) GetAccessSession(context.Context, string, string) (*domain.DevMachineAccessSession, error) {
	return f.session, nil
}
func (f *gatewayStoreFake) CreateAccessLog(_ context.Context, accessLog *domain.DevMachineAccessLog) error {
	f.accessLogs = append(f.accessLogs, *accessLog)
	return nil
}
func (f *gatewayStoreFake) TouchMachineActivity(context.Context, uuid.UUID, time.Time) error {
	return nil
}

func TestGatewayParsesOnlyConfiguredHosts(t *testing.T) {
	gateway, err := NewGateway(&gatewayStoreFake{}, "machines.example.com", time.Hour)
	require.NoError(t, err)
	routingKey, service, ok := gateway.parseHost("0123456789abcdef0123-browser.machines.example.com")
	require.True(t, ok)
	require.Equal(t, "0123456789abcdef0123", routingKey)
	require.Equal(t, "browser", service)
	_, _, ok = gateway.parseHost("0123456789abcdef0123.machines.example.com.attacker.test")
	require.False(t, ok)
}

func TestGatewayStripsKuayleCredentialsBeforeProxy(t *testing.T) {
	var authorization, cookie, internalHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		cookie = request.Header.Get("Cookie")
		internalHeader = request.Header.Get("X-Kuayle-Internal")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	upstreamURL := strings.TrimPrefix(upstream.URL, "http://")
	host, rawPort, _ := net.SplitHostPort(upstreamURL)
	port, _ := strconv.Atoi(rawPort)
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide", InternalHost: host, InternalPort: port},
		session: &domain.DevMachineAccessSession{MachineID: machineID, ServiceID: serviceID, UserID: userID},
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "http://0123456789abcdef0123.machines.example.com/", nil)
	request.Host = "0123456789abcdef0123.machines.example.com"
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	request.Header.Set("Authorization", "Bearer user-token")
	request.Header.Set("X-Kuayle-Internal", "secret")
	recorder := httptest.NewRecorder()
	gateway.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Empty(t, authorization)
	require.Empty(t, cookie)
	require.Empty(t, internalHeader)
	require.Equal(t, "allowed", store.accessLogs[len(store.accessLogs)-1].Decision)
}

func TestGatewayLaunchTicketIsSingleUse(t *testing.T) {
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide"},
		ticket:  &domain.DevMachineAccessTicket{MachineID: machineID, ServiceID: serviceID, UserID: userID, WorkspaceID: uuid.New()},
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	launchURL := "https://0123456789abcdef0123.machines.example.com/?ticket=" + strings.Repeat("b", 64)
	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, launchURL, nil)
	request.Host = "0123456789abcdef0123.machines.example.com"
	gateway.ServeHTTP(first, request)
	require.Equal(t, http.StatusSeeOther, first.Code)
	require.Contains(t, first.Header().Get("Set-Cookie"), machineSessionCookie)
	require.Contains(t, first.Header().Get("Set-Cookie"), "SameSite=Lax")
	second := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, launchURL, nil)
	request.Host = "0123456789abcdef0123.machines.example.com"
	gateway.ServeHTTP(second, request)
	require.Equal(t, http.StatusUnauthorized, second.Code)
}

func TestGatewayTerminalWebSocketTicketRequiresFrontendOriginAndStripsTicket(t *testing.T) {
	var upstreamQuery, upstreamCookie, upstreamAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamQuery = request.URL.RawQuery
		upstreamCookie = request.Header.Get("Cookie")
		upstreamAuth = request.Header.Get("Authorization")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	host, rawPort, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(rawPort)
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	rawTicket := strings.Repeat("c", 64)
	frontendOrigin := "https://app.example.com"
	runtimeSession, cwd := "term-123", "/workspace/tasks/eng-1"
	store := &gatewayStoreFake{
		machine:      &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service:      &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "terminal", InternalHost: host, InternalPort: port},
		ticket:       &domain.DevMachineAccessTicket{MachineID: machineID, ServiceID: serviceID, UserID: userID, WorkspaceID: uuid.New()},
		expectedHash: terminalTicketHash(rawTicket, frontendOrigin, runtimeSession, cwd),
		expectedHost: "0123456789abcdef0123-terminal.machines.example.net",
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour, frontendOrigin)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "https://0123456789abcdef0123-terminal.machines.example.net/ws?ticket="+rawTicket+"&session="+runtimeSession+"&cwd="+url.QueryEscape(cwd), nil)
	request.Host = "0123456789abcdef0123-terminal.machines.example.net"
	request.Header.Set("Origin", frontendOrigin)
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Authorization", "Bearer should-not-forward")
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, "arg=term-123&arg=%2Fworkspace%2Ftasks%2Feng-1", upstreamQuery)
	require.Empty(t, upstreamCookie)
	require.Empty(t, upstreamAuth)
	require.True(t, store.ticketUsed)
	require.Equal(t, []string{"0123456789abcdef0123-terminal.machines.example.net"}, store.consumedHosts)
}

func TestGatewayTerminalWebSocketTicketRejectsWrongOriginWithoutConsuming(t *testing.T) {
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	rawTicket := strings.Repeat("d", 64)
	store := &gatewayStoreFake{
		machine:      &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service:      &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "terminal", InternalHost: "127.0.0.1", InternalPort: 1},
		ticket:       &domain.DevMachineAccessTicket{MachineID: machineID, ServiceID: serviceID, UserID: userID, WorkspaceID: uuid.New()},
		expectedHash: terminalTicketHash(rawTicket, "https://app.example.com", "term-123", "/workspace/tasks/eng-1"),
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour, "https://app.example.com")
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "https://0123456789abcdef0123-terminal.machines.example.net/ws?ticket="+rawTicket+"&session=term-123&cwd=%2Fworkspace%2Ftasks%2Feng-1", nil)
	request.Host = "0123456789abcdef0123-terminal.machines.example.net"
	request.Header.Set("Origin", "https://evil.example.com")
	request.Header.Set("Upgrade", "websocket")
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.False(t, store.ticketUsed)
}

func TestGatewayRejectsCrossOriginMutation(t *testing.T) {
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide", InternalHost: "127.0.0.1", InternalPort: 1},
		session: &domain.DevMachineAccessSession{MachineID: machineID, ServiceID: serviceID, UserID: userID},
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "https://0123456789abcdef0123.machines.example.net/action", nil)
	request.Host = "0123456789abcdef0123.machines.example.net"
	request.Header.Set("Origin", "https://other.machines.example.net")
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "invalid_origin", *store.accessLogs[len(store.accessLogs)-1].Reason)
}

func TestGatewayDemoModeBlocksNonSysAdminTicketExchange(t *testing.T) {
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide"},
		ticket:  &domain.DevMachineAccessTicket{MachineID: machineID, ServiceID: serviceID, UserID: userID, WorkspaceID: uuid.New()},
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	gateway.SetDemoRestriction(true, func(id uuid.UUID) bool { return false })

	request := httptest.NewRequest(http.MethodGet, "https://0123456789abcdef0123.machines.example.com/?ticket="+strings.Repeat("b", 64), nil)
	request.Host = "0123456789abcdef0123.machines.example.com"
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.True(t, store.ticketUsed, "ticket must be consumed before the demo check runs")
	require.Equal(t, "demo_user_forbidden", *store.accessLogs[len(store.accessLogs)-1].Reason)
}

func TestGatewayDemoModeAllowsSysAdminTicketExchange(t *testing.T) {
	machineID, serviceID, sysAdminID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide"},
		ticket:  &domain.DevMachineAccessTicket{MachineID: machineID, ServiceID: serviceID, UserID: sysAdminID, WorkspaceID: uuid.New()},
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	gateway.SetDemoRestriction(true, func(id uuid.UUID) bool { return id == sysAdminID })

	request := httptest.NewRequest(http.MethodGet, "https://0123456789abcdef0123.machines.example.com/?ticket="+strings.Repeat("b", 64), nil)
	request.Host = "0123456789abcdef0123.machines.example.com"
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusSeeOther, recorder.Code)
}

func TestGatewayDemoModeBlocksNonSysAdminExistingSession(t *testing.T) {
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide"},
		session: &domain.DevMachineAccessSession{MachineID: machineID, ServiceID: serviceID, UserID: userID},
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	gateway.SetDemoRestriction(true, func(id uuid.UUID) bool { return false })

	request := httptest.NewRequest(http.MethodGet, "https://0123456789abcdef0123.machines.example.com/some-path", nil)
	request.Host = "0123456789abcdef0123.machines.example.com"
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "demo_user_forbidden", *store.accessLogs[len(store.accessLogs)-1].Reason)
}

func TestGatewayWithoutDemoRestrictionAllowsEveryone(t *testing.T) {
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide"},
		ticket:  &domain.DevMachineAccessTicket{MachineID: machineID, ServiceID: serviceID, UserID: userID, WorkspaceID: uuid.New()},
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodGet, "https://0123456789abcdef0123.machines.example.com/?ticket="+strings.Repeat("b", 64), nil)
	request.Host = "0123456789abcdef0123.machines.example.com"
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusSeeOther, recorder.Code)
}

func TestGatewayOnlyForwardsHostOnlyUpstreamCookies(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Add("Set-Cookie", "access_token=attacker; Domain=example.com; Path=/api")
		writer.Header().Add("Set-Cookie", "project=value; Path=/")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	host, rawPort, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(rawPort)
	machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New()
	store := &gatewayStoreFake{
		machine: &domain.DevMachine{ID: machineID, WorkspaceID: uuid.New(), RoutingKey: "0123456789abcdef0123", Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour)},
		service: &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceType: "ide", InternalHost: host, InternalPort: port},
		session: &domain.DevMachineAccessSession{MachineID: machineID, ServiceID: serviceID, UserID: userID},
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "http://0123456789abcdef0123.machines.example.net/", nil)
	request.Host = "0123456789abcdef0123.machines.example.net"
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, []string{"project=value; Path=/"}, recorder.Header().Values("Set-Cookie"))
}
