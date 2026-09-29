package transit

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Address, DSN, PublicBaseURL, HomeURL, PriceURL, MonitorURL, PricingFile string
	CacheTTL                                                                time.Duration
}

func LoadConfig() (Config, error) {
	var c Config
	c.CacheTTL = 60 * time.Second
	c.PublicBaseURL = os.Getenv("PUBLIC_BASE_URL")
	c.HomeURL = os.Getenv("STATION_HOME_URL")
	c.PriceURL = os.Getenv("STATION_PRICE_URL")
	c.MonitorURL = os.Getenv("STATION_MONITOR_URL")
	for name, value := range map[string]string{"PUBLIC_BASE_URL": c.PublicBaseURL, "STATION_HOME_URL": c.HomeURL, "STATION_PRICE_URL": c.PriceURL, "STATION_MONITOR_URL": c.MonitorURL} {
		if value == "" && name != "PUBLIC_BASE_URL" {
			continue
		}
		u, err := url.Parse(value)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || (name == "PUBLIC_BASE_URL" && (u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/"))) {
			return c, fmt.Errorf("invalid %s", name)
		}
	}
	c.PublicBaseURL = strings.TrimRight(c.PublicBaseURL, "/")
	port := env("SERVER_PORT", "8080")
	if err := validPort(port); err != nil {
		return c, fmt.Errorf("invalid SERVER_PORT")
	}
	c.Address = net.JoinHostPort(env("SERVER_HOST", "0.0.0.0"), port)
	dbPort := env("DATABASE_PORT", "5432")
	if err := validPort(dbPort); err != nil {
		return c, fmt.Errorf("invalid DATABASE_PORT")
	}
	ssl := env("DATABASE_SSLMODE", "disable")
	switch ssl {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return c, fmt.Errorf("invalid DATABASE_SSLMODE")
	}
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(env("DATABASE_HOST", "localhost"), dbPort), Path: "/" + env("DATABASE_DBNAME", "sub2api"), User: url.UserPassword(env("DATABASE_USER", "transit_reader"), os.Getenv("DATABASE_PASSWORD"))}
	q := u.Query()
	q.Set("sslmode", ssl)
	q.Set("connect_timeout", "5")
	q.Set("default_transaction_read_only", "on")
	q.Set("statement_timeout", "15000")
	q.Set("search_path", "public")
	q.Set("application_name", "ai-transit-readonly")
	if cert := os.Getenv("DATABASE_SSLROOTCERT"); cert != "" {
		q.Set("sslrootcert", cert)
	}
	u.RawQuery = q.Encode()
	c.DSN = u.String()
	c.PricingFile = os.Getenv("PRICING_FILE")
	return c, nil
}
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func validPort(s string) error {
	n, e := strconv.Atoi(s)
	if e != nil || n < 1 || n > 65535 {
		return fmt.Errorf("invalid port")
	}
	return nil
}
