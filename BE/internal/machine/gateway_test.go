package machine

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

type gatewayStoreFake struct {
	machine          *domain.DevMachine
	service          *domain.DevMachineService
	session          *domain.DevMachineAccessSession
	ticket           *domain.DevMachineAccessTicket
	expectedHash     string
	expectedHost     string
	consumedHosts    []string
	ticketUsed       bool
	createSessionErr error
	accessLogs       []domain.DevMachineAccessLog
	routeLookups     int
}

type blockingGatewayStore struct {
	*gatewayStoreFake
	started chan struct{}
	release chan struct{}
	lookups atomic.Int64
}

func (f *blockingGatewayStore) GetRoute(context.Context, string, string) (*domain.DevMachine, *domain.DevMachineService, error) {
	f.lookups.Add(1)
	f.started <- struct{}{}
	<-f.release
	return nil, nil, nil
}

func (f *gatewayStoreFake) GetRoute(context.Context, string, string) (*domain.DevMachine, *domain.DevMachineService, error) {
	f.routeLookups++
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
	if f.ticket == nil {
		return nil, nil
	}
	f.ticketUsed = true
	f.consumedHosts = append(f.consumedHosts, host)
	ticket := *f.ticket
	ticket.Status = domain.DevMachineAccessTicketStatusUsed
	usedAt := time.Now().UTC()
	ticket.UsedAt = &usedAt
	return &ticket, nil
}
func (f *gatewayStoreFake) CreateAccessSession(_ context.Context, session *domain.DevMachineAccessSession) error {
	if f.createSessionErr != nil {
		return f.createSessionErr
	}
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

func gatewayMachine(workspaceID, machineID, userID uuid.UUID, routingKey string) *domain.DevMachine {
	return &domain.DevMachine{
		ID: machineID, WorkspaceID: workspaceID, CreatedByUserID: &userID, RoutingKey: routingKey,
		Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning, ExpiresAt: time.Now().Add(time.Hour),
	}
}

func gatewayService(serviceID, machineID uuid.UUID, serviceType string) *domain.DevMachineService {
	return &domain.DevMachineService{ID: serviceID, MachineID: machineID, ServiceKey: serviceType, ServiceType: serviceType, Status: "running"}
}

func gatewayTicket(workspaceID, machineID, serviceID, userID uuid.UUID, host string) *domain.DevMachineAccessTicket {
	return &domain.DevMachineAccessTicket{
		WorkspaceID: workspaceID, MachineID: machineID, ServiceID: serviceID, UserID: userID,
		Status: domain.DevMachineAccessTicketStatusActive, BoundHost: host, ExpiresAt: time.Now().Add(time.Minute),
	}
}

func gatewaySession(workspaceID, machineID, serviceID, userID uuid.UUID, host string) *domain.DevMachineAccessSession {
	return &domain.DevMachineAccessSession{
		WorkspaceID: workspaceID, MachineID: machineID, ServiceID: serviceID, UserID: userID,
		BoundHost: host, ExpiresAt: time.Now().Add(time.Hour),
	}
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

func TestGatewayCachesAndSamplesUnknownRoutes(t *testing.T) {
	store := &gatewayStoreFake{}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	host := "0123456789abcdef0123.machines.example.com"

	for range 50 {
		request := httptest.NewRequest(http.MethodGet, "https://"+host+"/", nil)
		request.Host = host
		recorder := httptest.NewRecorder()
		gateway.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNotFound, recorder.Code)
	}

	require.Equal(t, 1, store.routeLookups)
	require.Len(t, store.accessLogs, 1)
	require.Equal(t, "route_not_found", *store.accessLogs[0].Reason)
}

func TestGatewayRateLimitsWildcardRouteLookupsBeforeDatabase(t *testing.T) {
	store := &gatewayStoreFake{}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	source := "192.0.2.1"
	future := time.Now().Add(time.Hour)
	for range gatewayRoutesPerSource {
		require.True(t, gateway.abuseGuard.allowRouteLookup(source, future))
	}
	host := "0123456789abcdef0123.machines.example.com"
	request := httptest.NewRequest(http.MethodGet, "https://"+host+"/", nil)
	request.Host = host
	request.RemoteAddr = source + ":1234"
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "1", recorder.Header().Get("Retry-After"))
	require.Zero(t, store.routeLookups)
}

func TestGatewayAbuseGuardBoundsAttackerControlledState(t *testing.T) {
	guard := newGatewayAbuseGuard()
	now := time.Now().UTC()
	for index := 0; index < 20_000; index++ {
		source := fmt.Sprintf("198.51.%d.%d", (index/256)%256, index%256)
		guard.allowRouteLookup(source, now.Add(time.Duration(index)*gatewayRouteWindow))
		guard.allowUnknownAudit(source, now)
		guard.rememberNegativeRoute(fmt.Sprintf("%020d:ide", index), now)
	}

	require.LessOrEqual(t, len(guard.routeSources), gatewayMaxTrackedSources)
	require.LessOrEqual(t, len(guard.auditAfter), gatewayMaxTrackedSources)
	require.LessOrEqual(t, len(guard.negativeUntil), gatewayMaxNegativeRoutes)
}

func TestGatewayAbuseGuardAppliesGlobalRouteLimit(t *testing.T) {
	guard := newGatewayAbuseGuard()
	now := time.Now().UTC()
	for index := range gatewayRoutesGlobal {
		require.True(t, guard.allowRouteLookup(fmt.Sprintf("source-%d", index), now))
	}
	require.False(t, guard.allowRouteLookup("one-source-too-many", now))
}

func TestGatewayCapsConcurrentRouteQueries(t *testing.T) {
	store := &blockingGatewayStore{
		gatewayStoreFake: &gatewayStoreFake{},
		started:          make(chan struct{}, 32),
		release:          make(chan struct{}),
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	host := "0123456789abcdef0123.machines.example.com"
	var requests sync.WaitGroup
	for range cap(gateway.routeSlots) {
		requests.Add(1)
		go func() {
			defer requests.Done()
			request := httptest.NewRequest(http.MethodGet, "https://"+host+"/", nil)
			request.Host = host
			gateway.ServeHTTP(httptest.NewRecorder(), request)
		}()
	}
	for range cap(gateway.routeSlots) {
		<-store.started
	}
	request := httptest.NewRequest(http.MethodGet, "https://"+host+"/", nil)
	request.Host = host
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, int64(cap(gateway.routeSlots)), store.lookups.Load())
	close(store.release)
	requests.Wait()
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
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	service := gatewayService(serviceID, machineID, "ide")
	service.InternalHost = host
	service.InternalPort = port
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service: service,
		session: gatewaySession(workspaceID, machineID, serviceID, userID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "http://"+machineHost+"/", nil)
	request.Host = machineHost
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
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service: gatewayService(serviceID, machineID, "ide"),
		ticket:  gatewayTicket(workspaceID, machineID, serviceID, userID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	launchURL := "https://" + machineHost + "/?ticket=" + strings.Repeat("b", 64)
	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, launchURL, nil)
	request.Host = machineHost
	gateway.ServeHTTP(first, request)
	require.Equal(t, http.StatusSeeOther, first.Code)
	require.Contains(t, first.Header().Get("Set-Cookie"), machineSessionCookie)
	require.Contains(t, first.Header().Get("Set-Cookie"), "SameSite=Lax")
	require.NotNil(t, store.session)
	require.Equal(t, workspaceID, store.session.WorkspaceID)
	require.Equal(t, machineID, store.session.MachineID)
	require.Equal(t, serviceID, store.session.ServiceID)
	require.Equal(t, userID, store.session.UserID)
	second := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, launchURL, nil)
	request.Host = machineHost
	gateway.ServeHTTP(second, request)
	require.Equal(t, http.StatusUnauthorized, second.Code)
}

func TestGatewayRejectsMalformedTicketTuples(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	for _, test := range []struct {
		name   string
		mutate func(*domain.DevMachineAccessTicket)
	}{
		{name: "cross-workspace", mutate: func(ticket *domain.DevMachineAccessTicket) { ticket.WorkspaceID = uuid.New() }},
		{name: "noncreator", mutate: func(ticket *domain.DevMachineAccessTicket) { ticket.UserID = uuid.New() }},
		{name: "wrong-service", mutate: func(ticket *domain.DevMachineAccessTicket) { ticket.ServiceID = uuid.New() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			ticket := gatewayTicket(workspaceID, machineID, serviceID, userID, machineHost)
			test.mutate(ticket)
			store := &gatewayStoreFake{
				machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
				service: gatewayService(serviceID, machineID, "ide"),
				ticket:  ticket,
			}
			gateway, err := NewGateway(store, "machines.example.com", time.Hour)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/?ticket="+strings.Repeat("b", 64), nil)
			request.Host = machineHost
			recorder := httptest.NewRecorder()

			gateway.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
			require.True(t, store.ticketUsed, "ticket should be atomically consumed before tuple validation rejects it")
			require.Nil(t, store.session)
		})
	}
}

func TestGatewayTicketExchangeMapsSessionNoRowsToUnauthorized(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	store := &gatewayStoreFake{
		machine:          gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service:          gatewayService(serviceID, machineID, "ide"),
		ticket:           gatewayTicket(workspaceID, machineID, serviceID, userID, machineHost),
		createSessionErr: sql.ErrNoRows,
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/?ticket="+strings.Repeat("b", 64), nil)
	request.Host = machineHost
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.True(t, store.ticketUsed)
	require.Equal(t, "invalid_ticket", *store.accessLogs[len(store.accessLogs)-1].Reason)
}

func TestGatewayRejectsMalformedSessionTuples(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	for _, test := range []struct {
		name   string
		mutate func(*domain.DevMachineAccessSession)
	}{
		{name: "cross-workspace", mutate: func(session *domain.DevMachineAccessSession) { session.WorkspaceID = uuid.New() }},
		{name: "noncreator", mutate: func(session *domain.DevMachineAccessSession) { session.UserID = uuid.New() }},
		{name: "wrong-service", mutate: func(session *domain.DevMachineAccessSession) { session.ServiceID = uuid.New() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := gatewaySession(workspaceID, machineID, serviceID, userID, machineHost)
			test.mutate(session)
			service := gatewayService(serviceID, machineID, "ide")
			service.InternalHost = "127.0.0.1"
			service.InternalPort = 1
			store := &gatewayStoreFake{
				machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
				service: service,
				session: session,
			}
			gateway, err := NewGateway(store, "machines.example.com", time.Hour)
			require.NoError(t, err)
			request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/", nil)
			request.Host = machineHost
			request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
			recorder := httptest.NewRecorder()

			gateway.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusUnauthorized, recorder.Code)
		})
	}
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
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	rawTicket := strings.Repeat("c", 64)
	frontendOrigin := "https://app.example.com"
	runtimeSession, cwd := "term-123", "/workspace/tasks/eng-1"
	machineHost := "0123456789abcdef0123-terminal.machines.example.net"
	service := gatewayService(serviceID, machineID, "terminal")
	service.InternalHost = host
	service.InternalPort = port
	store := &gatewayStoreFake{
		machine:      gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service:      service,
		ticket:       gatewayTicket(workspaceID, machineID, serviceID, userID, machineHost),
		expectedHash: terminalTicketHash(rawTicket, frontendOrigin, runtimeSession, cwd),
		expectedHost: machineHost,
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour, frontendOrigin)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/ws?ticket="+rawTicket+"&session="+runtimeSession+"&cwd="+url.QueryEscape(cwd), nil)
	request.Host = machineHost
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
	require.Equal(t, []string{machineHost}, store.consumedHosts)
}

func TestGatewayTerminalWebSocketTicketRejectsWrongOriginWithoutConsuming(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	rawTicket := strings.Repeat("d", 64)
	machineHost := "0123456789abcdef0123-terminal.machines.example.net"
	service := gatewayService(serviceID, machineID, "terminal")
	service.InternalHost = "127.0.0.1"
	service.InternalPort = 1
	store := &gatewayStoreFake{
		machine:      gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service:      service,
		ticket:       gatewayTicket(workspaceID, machineID, serviceID, userID, machineHost),
		expectedHash: terminalTicketHash(rawTicket, "https://app.example.com", "term-123", "/workspace/tasks/eng-1"),
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour, "https://app.example.com")
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/ws?ticket="+rawTicket+"&session=term-123&cwd=%2Fworkspace%2Ftasks%2Feng-1", nil)
	request.Host = machineHost
	request.Header.Set("Origin", "https://evil.example.com")
	request.Header.Set("Upgrade", "websocket")
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.False(t, store.ticketUsed)
}

func TestGatewayRejectsCrossOriginMutation(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.net"
	service := gatewayService(serviceID, machineID, "ide")
	service.InternalHost = "127.0.0.1"
	service.InternalPort = 1
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service: service,
		session: gatewaySession(workspaceID, machineID, serviceID, userID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "https://"+machineHost+"/action", nil)
	request.Host = machineHost
	request.Header.Set("Origin", "https://other.machines.example.net")
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "invalid_origin", *store.accessLogs[len(store.accessLogs)-1].Reason)
}

func TestGatewayDemoModeBlocksNonSysAdminTicketExchange(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service: gatewayService(serviceID, machineID, "ide"),
		ticket:  gatewayTicket(workspaceID, machineID, serviceID, userID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	gateway.SetDemoRestriction(true, func(id uuid.UUID) bool { return false })

	request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/?ticket="+strings.Repeat("b", 64), nil)
	request.Host = machineHost
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.True(t, store.ticketUsed, "ticket must be consumed before the demo check runs")
	require.Equal(t, "demo_user_forbidden", *store.accessLogs[len(store.accessLogs)-1].Reason)
}

func TestGatewayDemoModeAllowsSysAdminTicketExchange(t *testing.T) {
	workspaceID, machineID, serviceID, sysAdminID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, sysAdminID, "0123456789abcdef0123"),
		service: gatewayService(serviceID, machineID, "ide"),
		ticket:  gatewayTicket(workspaceID, machineID, serviceID, sysAdminID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	gateway.SetDemoRestriction(true, func(id uuid.UUID) bool { return id == sysAdminID })

	request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/?ticket="+strings.Repeat("b", 64), nil)
	request.Host = machineHost
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusSeeOther, recorder.Code)
}

func TestGatewayDemoModeBlocksNonSysAdminExistingSession(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service: gatewayService(serviceID, machineID, "ide"),
		session: gatewaySession(workspaceID, machineID, serviceID, userID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)
	gateway.SetDemoRestriction(true, func(id uuid.UUID) bool { return false })

	request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/some-path", nil)
	request.Host = machineHost
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Equal(t, "demo_user_forbidden", *store.accessLogs[len(store.accessLogs)-1].Reason)
}

func TestGatewayWithoutDemoRestrictionAllowsEveryone(t *testing.T) {
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.com"
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service: gatewayService(serviceID, machineID, "ide"),
		ticket:  gatewayTicket(workspaceID, machineID, serviceID, userID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.com", time.Hour)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodGet, "https://"+machineHost+"/?ticket="+strings.Repeat("b", 64), nil)
	request.Host = machineHost
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
	workspaceID, machineID, serviceID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	machineHost := "0123456789abcdef0123.machines.example.net"
	service := gatewayService(serviceID, machineID, "ide")
	service.InternalHost = host
	service.InternalPort = port
	store := &gatewayStoreFake{
		machine: gatewayMachine(workspaceID, machineID, userID, "0123456789abcdef0123"),
		service: service,
		session: gatewaySession(workspaceID, machineID, serviceID, userID, machineHost),
	}
	gateway, err := NewGateway(store, "machines.example.net", time.Hour)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "http://"+machineHost+"/", nil)
	request.Host = machineHost
	request.AddCookie(&http.Cookie{Name: machineSessionCookie, Value: strings.Repeat("a", 64)})
	recorder := httptest.NewRecorder()

	gateway.ServeHTTP(recorder, request)

	require.Equal(t, []string{"project=value; Path=/"}, recorder.Header().Values("Set-Cookie"))
}
