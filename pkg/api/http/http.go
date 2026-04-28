// Copyright (C) 2025 wangyusong
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package http

import (
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"github.com/glidea/zenfeed/pkg/api"
	"github.com/glidea/zenfeed/pkg/component"
	"github.com/glidea/zenfeed/pkg/config"
	telemetry "github.com/glidea/zenfeed/pkg/telemetry"
	"github.com/glidea/zenfeed/pkg/telemetry/log"
	telemetrymodel "github.com/glidea/zenfeed/pkg/telemetry/model"
	"github.com/glidea/zenfeed/pkg/util/jsonrpc"
)

// --- Interface code block ---
type Server interface {
	component.Component
	config.Watcher
}

type Config struct {
	Address        string
	AuthToken      string
	DisableCORS    bool
	AllowedOrigins []string
}

func (c *Config) Validate() error {
	if c.Address == "" {
		c.Address = ":1300"
	}
	if _, _, err := net.SplitHostPort(c.Address); err != nil {
		return errors.Wrap(err, "invalid address")
	}
	if len(c.AllowedOrigins) == 0 {
		c.AllowedOrigins = []string{
			"http://localhost:1400",
			"http://127.0.0.1:1400",
		}
	}

	return nil
}

func (c *Config) From(app *config.App) *Config {
	c.Address = app.API.HTTP.Address
	c.AuthToken = app.API.HTTP.AuthToken
	c.DisableCORS = app.API.HTTP.DisableCORS
	c.AllowedOrigins = app.API.HTTP.AllowedOrigins

	return c
}

type Dependencies struct {
	API api.API
}

// --- Factory code block ---
type Factory component.Factory[Server, config.App, Dependencies]

func NewFactory(mockOn ...component.MockOption) Factory {
	if len(mockOn) > 0 {
		return component.FactoryFunc[Server, config.App, Dependencies](
			func(instance string, config *config.App, dependencies Dependencies) (Server, error) {
				m := &mockServer{}
				component.MockOptions(mockOn).Apply(&m.Mock)

				return m, nil
			},
		)
	}

	return component.FactoryFunc[Server, config.App, Dependencies](new)
}

func new(instance string, app *config.App, dependencies Dependencies) (Server, error) {
	config := &Config{}
	config.From(app)
	if err := config.Validate(); err != nil {
		return nil, errors.Wrap(err, "validate config")
	}

	router := http.NewServeMux()
	impl := &server{
		Base: component.New(&component.BaseConfig[Config, Dependencies]{
			Name:         "HTTPServer",
			Instance:     instance,
			Config:       config,
			Dependencies: dependencies,
		}),
	}
	api := dependencies.API
	router.Handle("/write", impl.wrap(jsonrpc.API(api.Write), false))
	router.Handle("/query_config", impl.wrap(jsonrpc.API(api.QueryAppConfig), true))
	router.Handle("/apply_config", impl.wrap(jsonrpc.API(api.ApplyAppConfig), true))
	router.Handle("/query_config_schema", impl.wrap(jsonrpc.API(api.QueryAppConfigSchema), false))
	router.Handle("/query_rsshub_categories", impl.wrap(jsonrpc.API(api.QueryRSSHubCategories), false))
	router.Handle("/query_rsshub_websites", impl.wrap(jsonrpc.API(api.QueryRSSHubWebsites), false))
	router.Handle("/query_rsshub_routes", impl.wrap(jsonrpc.API(api.QueryRSSHubRoutes), false))
	router.Handle("/query", impl.wrap(jsonrpc.API(api.Query), false))
	httpServer := &http.Server{Addr: config.Address, Handler: router}

	impl.http = httpServer

	return impl, nil
}

// --- Implementation code block ---
type server struct {
	*component.Base[Config, Dependencies]
	http *http.Server
}

func (s *server) wrap(next http.Handler, requireAuth bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowCORS(w, r) {
			http.Error(w, "CORS origin is not allowed", http.StatusForbidden)

			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)

			return
		}
		if requireAuth && !s.isAuthorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *server) allowCORS(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if s.Config().DisableCORS {
		return s.isSameOrigin(origin, r)
	}
	if !s.isAllowedOrigin(origin) {
		return false
	}

	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set(
		"Access-Control-Allow-Headers",
		"Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Zenfeed-Auth-Token",
	)

	return true
}

func (s *server) isSameOrigin(origin string, r *http.Request) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}

	expectedScheme := r.Header.Get("X-Forwarded-Proto")
	if expectedScheme == "" {
		expectedScheme = "http"
		if r.TLS != nil {
			expectedScheme = "https"
		}
	}

	return strings.EqualFold(u.Scheme, expectedScheme)
}

func (s *server) isAllowedOrigin(origin string) bool {
	for _, allowedOrigin := range s.Config().AllowedOrigins {
		if origin == strings.TrimSpace(allowedOrigin) {
			return true
		}
	}

	return false
}

func (s *server) isAuthorized(r *http.Request) bool {
	token := strings.TrimSpace(s.Config().AuthToken)
	if token == "" {
		return true
	}

	if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			givenToken := strings.TrimSpace(authHeader[len("Bearer "):])
			if subtle.ConstantTimeCompare([]byte(givenToken), []byte(token)) == 1 {
				return true
			}
		}
	}

	givenToken := strings.TrimSpace(r.Header.Get("X-Zenfeed-Auth-Token"))

	return subtle.ConstantTimeCompare([]byte(givenToken), []byte(token)) == 1
}

func (s *server) Run() (err error) {
	ctx := telemetry.StartWith(s.Context(), append(s.TelemetryLabels(), telemetrymodel.KeyOperation, "Run")...)
	defer func() { telemetry.End(ctx, err) }()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- s.http.ListenAndServe()
	}()

	s.MarkReady()
	select {
	case <-ctx.Done():
		log.Info(ctx, "shutting down")

		return s.http.Shutdown(ctx)
	case err := <-serverErr:
		return errors.Wrap(err, "listen and serve")
	}
}

func (s *server) Reload(app *config.App) error {
	newConfig := &Config{}
	newConfig.From(app)
	if err := newConfig.Validate(); err != nil {
		return errors.Wrap(err, "validate config")
	}
	if s.Config().Address != newConfig.Address {
		return errors.New("address cannot be reloaded")
	}

	s.SetConfig(newConfig)

	return nil
}

type mockServer struct {
	component.Mock
}

func (m *mockServer) Reload(app *config.App) error {
	return m.Called(app).Error(0)
}
