package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	apiauth "github.com/vein05/pali/internal/auth"
	"github.com/vein05/pali/internal/config"
)

func main() {
	var (
		tenant     string
		secret     string
		issuer     string
		configPath string
		ttlRaw     string
	)

	flag.StringVar(&tenant, "tenant", "", "Tenant ID to embed in token claims (required)")
	flag.StringVar(&secret, "secret", "", "JWT secret (fallback: PALI_JWT_SECRET or config auth.jwt_secret)")
	flag.StringVar(&issuer, "issuer", "", "JWT issuer (fallback: config auth.issuer, then 'pali')")
	flag.StringVar(&ttlRaw, "ttl", "24h", "Token lifetime, e.g. 15m, 1h, 24h")
	flag.StringVar(&configPath, "config", "pali.yaml", "Optional config file for fallback secret/issuer")
	flag.Parse()

	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		exitf("missing required flag: -tenant")
	}

	ttl, err := time.ParseDuration(ttlRaw)
	if err != nil {
		exitf("invalid -ttl value %q: %v", ttlRaw, err)
	}

	secret = strings.TrimSpace(secret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("PALI_JWT_SECRET"))
	}

	cfg, cfgErr := config.Load(configPath)
	if cfgErr == nil {
		if secret == "" {
			secret = strings.TrimSpace(cfg.Auth.JWTSecret)
		}
		if strings.TrimSpace(issuer) == "" {
			issuer = strings.TrimSpace(cfg.Auth.Issuer)
		}
	} else if !errors.Is(cfgErr, os.ErrNotExist) && secret == "" {
		exitf("failed loading config %q: %v", configPath, cfgErr)
	}

	if strings.TrimSpace(issuer) == "" {
		issuer = "pali"
	}

	token, err := apiauth.GenerateTenantToken(secret, issuer, tenant, ttl)
	if err != nil {
		exitf("generate token: %v", err)
	}

	fmt.Println(token)
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
