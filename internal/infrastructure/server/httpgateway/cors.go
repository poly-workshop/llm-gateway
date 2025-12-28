package httpgateway

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/poly-workshop/llm-gateway/internal/infrastructure/config"
)

func normalizeCORSConfig(in config.CORSConfig) config.CORSConfig {
	out := in
	if len(out.AllowOrigins) == 0 {
		out.AllowOrigins = []string{"*"}
	}
	if len(out.AllowMethods) == 0 {
		out.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(out.AllowHeaders) == 0 {
		out.AllowHeaders = []string{"Authorization", "Content-Type", "X-Request-Id", "X-Usage-Callback"}
	}
	if out.MaxAge <= 0 {
		out.MaxAge = 10 * time.Minute
	}
	return out
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	cfg := s.cors
	if !cfg.Enabled {
		return next
	}
	cfg = normalizeCORSConfig(cfg)

	allowAnyOrigin := containsString(cfg.AllowOrigins, "*")
	allowHeadersAny := containsString(cfg.AllowHeaders, "*")
	allowMethodsAny := containsString(cfg.AllowMethods, "*")

	allowMethods := strings.Join(uniqueUpper(cfg.AllowMethods), ", ")
	allowHeaders := strings.Join(uniqueHeaderCase(cfg.AllowHeaders), ", ")
	exposeHeaders := strings.Join(uniqueHeaderCase(cfg.ExposeHeaders), ", ")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			next.ServeHTTP(w, r)
			return
		}
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		if !allowAnyOrigin && !containsString(cfg.AllowOrigins, origin) {
			// Disallowed origin: fail preflight; for non-preflight, omit CORS headers.
			if isPreflight(r) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Allowed origin.
		if allowAnyOrigin && !cfg.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			appendVary(w.Header(), "Origin")
		}
		if cfg.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if exposeHeaders != "" {
			w.Header().Set("Access-Control-Expose-Headers", exposeHeaders)
		}

		if isPreflight(r) {
			// Methods
			if allowMethodsAny {
				if m := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")); m != "" {
					w.Header().Set("Access-Control-Allow-Methods", strings.ToUpper(m))
				} else {
					w.Header().Set("Access-Control-Allow-Methods", allowMethods)
				}
			} else {
				w.Header().Set("Access-Control-Allow-Methods", allowMethods)
			}

			// Headers
			if allowHeadersAny {
				if h := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers")); h != "" {
					w.Header().Set("Access-Control-Allow-Headers", h)
				} else {
					w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
				}
			} else {
				w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
			}

			w.Header().Set("Access-Control-Max-Age", strconv.FormatInt(int64(cfg.MaxAge/time.Second), 10))
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isPreflight(r *http.Request) bool {
	if r == nil {
		return false
	}
	if strings.ToUpper(r.Method) != http.MethodOptions {
		return false
	}
	return strings.TrimSpace(r.Header.Get("Access-Control-Request-Method")) != ""
}

func appendVary(h http.Header, value string) {
	cur := h.Values("Vary")
	for _, v := range cur {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	if len(cur) == 0 {
		h.Set("Vary", value)
		return
	}
	h.Add("Vary", value)
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func uniqueUpper(in []string) []string {
	set := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := set[s]; ok {
			continue
		}
		set[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func uniqueHeaderCase(in []string) []string {
	set := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := set[key]; ok {
			continue
		}
		set[key] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
