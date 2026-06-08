//! Server configuration sourced from environment variables.
//!
//! oosiam runs two faces from one process: an HTTP listener for the
//! OAuth2 redirect dance (which cannot travel over NATS) and a NATS
//! client for management commands issued by oosd. Config therefore
//! covers the HTTP bind, NATS, Postgres, the signing-key location, and
//! token lifetimes.
//!
//! Unlike the other services oosiam reads NO config from KV — it is the
//! producer of the oos-iam bucket, not a consumer — so every value comes
//! from an OOSIAM_* env var or its default. The HTTP port defaults to
//! 5556 so the existing oos PKCE flow points at oosiam unchanged; the
//! Postgres DSN defaults to the shared demo DB so a fresh checkout runs
//! without env vars. Provenance rides along in `entries` for env.show.

use oos_svc::env::{resolve_env, EnvEntry};

/// Resolved server configuration, frozen at startup.
pub struct Config {
    pub host: String,
    pub port: u16,
    pub nats_url: String,
    pub pg_url: String,
    /// Filesystem path of the RS256 private key (PKCS#8 PEM). Generated
    /// on first start if absent.
    pub key_path: String,
    /// Access/ID token lifetime in seconds.
    pub token_ttl_sec: u64,
    /// Authorization code lifetime in seconds.
    pub code_ttl_sec: u64,
    /// Default OAuth2 client_id published to KV for desktop clients.
    pub client_id: String,
    /// Default loopback redirect URI published to KV for desktop clients.
    pub redirect_uri: String,
    /// Resolved entries for oos.cmd.oosiam.env.show, in declaration order.
    pub entries: Vec<EnvEntry>,
}

impl Config {
    /// Reads OOSIAM_* env vars and returns the resolved config.
    pub fn load() -> Self {
        let host = resolve_env("host", &["OOSIAM_HOST"], "localhost", false);
        let port = resolve_env("port", &["OOSIAM_PORT"], "5556", false);
        let nats_url = resolve_env("natsUrl", &["OOSIAM_NATS_URL"], "nats://localhost:4222", false);
        let pg_url = resolve_env(
            "pgUrl",
            &["OOSIAM_PG_URL"],
            "postgres://postgres:demo@localhost:5432/onisin",
            true,
        );
        // Default key path sits beside the app so a single-user install
        // keeps its signing key stable across restarts without setup. A
        // real deployment overrides this to a path outside the repo.
        let key_path = resolve_env("keyPath", &["OOSIAM_KEY_PATH"], ".oosiam/signing-key.pem", false);
        let token_ttl = resolve_env("tokenTtlSec", &["OOSIAM_TOKEN_TTL_SEC"], "28800", false);
        let code_ttl = resolve_env("codeTtlSec", &["OOSIAM_CODE_TTL_SEC"], "300", false);
        let client_id = resolve_env("clientId", &["OOSIAM_CLIENT_ID"], "oos-desktop", false);
        let redirect_uri = resolve_env(
            "redirectUri",
            &["OOSIAM_REDIRECT_URI"],
            "http://localhost:5557/callback",
            false,
        );

        // Parse the numeric values, falling back to the documented
        // defaults if an operator set a non-numeric override.
        let port_num = port.value.parse::<u16>().unwrap_or(5556);
        let token_ttl_num = token_ttl.value.parse::<u64>().unwrap_or(28800);
        let code_ttl_num = code_ttl.value.parse::<u64>().unwrap_or(300);

        let entries = vec![
            host.clone(),
            port.clone(),
            nats_url.clone(),
            pg_url.clone(),
            key_path.clone(),
            token_ttl.clone(),
            code_ttl.clone(),
            client_id.clone(),
            redirect_uri.clone(),
        ];

        Config {
            host: host.value,
            port: port_num,
            nats_url: nats_url.value,
            pg_url: pg_url.value,
            key_path: key_path.value,
            token_ttl_sec: token_ttl_num,
            code_ttl_sec: code_ttl_num,
            client_id: client_id.value,
            redirect_uri: redirect_uri.value,
            entries,
        }
    }
}
