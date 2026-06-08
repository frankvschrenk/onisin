//! OAuth2 authorization-code + PKCE endpoints (HTTP).
//!
//! Preserves the wire contract the oos PKCE client already speaks:
//!
//!   GET  /.well-known/openid-configuration
//!   GET  /jwks
//!   GET  /auth   — login form
//!   POST /auth   — verify credentials, issue code, redirect
//!   POST /token  — exchange code + verifier for an RS256 JWT
//!
//! Authorization codes live in memory: they last at most code_ttl_sec,
//! so a restart merely forces an in-flight login to start over — not
//! worth a table.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{SystemTime, UNIX_EPOCH};

use axum::extract::{Form, Query, State};
use axum::http::StatusCode;
use axum::response::{Html, IntoResponse, Response};
use axum::routing::{get, post};
use axum::{Json, Router};
use rand::rngs::OsRng;
use rand::RngCore;
use serde::Deserialize;
use serde_json::json;
use sqlx::PgPool;

use crate::jwt::{sign_token, TokenSubject};
use crate::keys::SigningKey;
use crate::pkce::verify_s256;
use crate::store;

/// Shared server state. Held in an Arc so every handler task sees the
/// same signing key, pool, and authorization-code map.
struct AppState {
    pool: PgPool,
    key: SigningKey,
    issuer: String,
    token_ttl_sec: u64,
    code_ttl_sec: u64,
    codes: Mutex<HashMap<String, PendingCode>>,
}

/// One outstanding authorization code between /auth and /token.
struct PendingCode {
    sub: TokenSubject,
    client_id: String,
    code_challenge: String,
    issued_at: u64,
}

/// Binds host:port and serves the OAuth2 endpoints until the process
/// exits. Takes ownership of the pool and signing key.
pub async fn serve(
    host: String,
    port: u16,
    pool: PgPool,
    key: SigningKey,
    issuer: String,
    token_ttl_sec: u64,
    code_ttl_sec: u64,
) -> anyhow::Result<()> {
    let state = Arc::new(AppState {
        pool,
        key,
        issuer,
        token_ttl_sec,
        code_ttl_sec,
        codes: Mutex::new(HashMap::new()),
    });
    let app = Router::new()
        .route("/.well-known/openid-configuration", get(discovery))
        .route("/jwks", get(jwks))
        .route("/auth", get(auth_form).post(auth_submit))
        .route("/token", post(token))
        .with_state(state);

    let addr = format!("{host}:{port}");
    let listener = tokio::net::TcpListener::bind(&addr).await?;
    println!("[oosiam] http listening on {addr}");
    axum::serve(listener, app).await?;
    Ok(())
}

// ── Discovery + JWKS ─────────────────────────────────

/// OIDC metadata document; jwks_uri makes the key set discoverable.
async fn discovery(State(st): State<Arc<AppState>>) -> Json<serde_json::Value> {
    let iss = &st.issuer;
    Json(json!({
        "issuer": iss,
        "authorization_endpoint": format!("{iss}/auth"),
        "token_endpoint": format!("{iss}/token"),
        "jwks_uri": format!("{iss}/jwks"),
        "response_types_supported": ["code"],
        "subject_types_supported": ["public"],
        "id_token_signing_alg_values_supported": ["RS256"],
        "scopes_supported": ["openid", "profile", "email"],
        "code_challenge_methods_supported": ["S256"],
    }))
}

async fn jwks(State(st): State<Arc<AppState>>) -> Json<serde_json::Value> {
    let jwk = serde_json::to_value(&st.key.public_jwk).unwrap_or(serde_json::Value::Null);
    Json(json!({ "keys": [jwk] }))
}

// ── /auth ────────────────────────────────────────

#[derive(Deserialize)]
struct AuthQuery {
    client_id: Option<String>,
    redirect_uri: Option<String>,
    code_challenge: Option<String>,
    state: Option<String>,
}

async fn auth_form(Query(q): Query<AuthQuery>) -> Response {
    render_form(&FormParams {
        client_id: q.client_id.unwrap_or_default(),
        redirect_uri: q.redirect_uri.unwrap_or_default(),
        code_challenge: q.code_challenge.unwrap_or_default(),
        state: q.state.unwrap_or_default(),
        error: None,
    })
}

#[derive(Deserialize)]
struct AuthForm {
    #[serde(default)]
    email: String,
    #[serde(default)]
    password: String,
    #[serde(default)]
    client_id: String,
    #[serde(default)]
    redirect_uri: String,
    #[serde(default)]
    code_challenge: String,
    #[serde(default)]
    state: String,
}

async fn auth_submit(State(st): State<Arc<AppState>>, Form(f): Form<AuthForm>) -> Response {
    let base = FormParams {
        client_id: f.client_id.clone(),
        redirect_uri: f.redirect_uri.clone(),
        code_challenge: f.code_challenge.clone(),
        state: f.state.clone(),
        error: None,
    };

    let user = match store::verify_login(&st.pool, &f.email, &f.password).await {
        Ok(Some(u)) => u,
        Ok(None) => {
            let who = if f.email.is_empty() { "(blank)" } else { f.email.as_str() };
            eprintln!("[oosiam] failed login for {who}");
            return render_form(&FormParams { error: Some("Invalid email or password.".into()), ..base });
        }
        Err(e) => {
            eprintln!("[oosiam] login lookup failed: {e}");
            return render_form(&FormParams { error: Some("Invalid email or password.".into()), ..base });
        }
    };

    let code = new_code();
    {
        let mut codes = st.codes.lock().unwrap();
        codes.insert(
            code.clone(),
            PendingCode {
                sub: TokenSubject { email: user.email, username: user.username, groups: user.groups },
                client_id: f.client_id.clone(),
                code_challenge: f.code_challenge.clone(),
                issued_at: now_sec(),
            },
        );
    }

    let mut target = format!("{}?code={}", f.redirect_uri, urlencode(&code));
    if !f.state.is_empty() {
        target.push_str("&state=");
        target.push_str(&urlencode(&f.state));
    }
    Response::builder()
        .status(StatusCode::FOUND)
        .header("Location", target)
        .body(axum::body::Body::empty())
        .expect("valid redirect response")
}

// ── /token ──────────────────────────────────────

#[derive(Deserialize)]
struct TokenForm {
    #[serde(default)]
    code: String,
    #[serde(default)]
    code_verifier: String,
}

async fn token(State(st): State<Arc<AppState>>, Form(f): Form<TokenForm>) -> Response {
    if f.code.is_empty() || f.code_verifier.is_empty() {
        return err_json(StatusCode::BAD_REQUEST, "invalid_request", None);
    }
    // single-use: remove regardless of what happens next.
    let pc = { st.codes.lock().unwrap().remove(&f.code) };
    let Some(pc) = pc else {
        return err_json(StatusCode::BAD_REQUEST, "invalid_grant", Some("unknown code"));
    };
    if now_sec().saturating_sub(pc.issued_at) > st.code_ttl_sec {
        return err_json(StatusCode::BAD_REQUEST, "invalid_grant", Some("code expired"));
    }
    if !verify_s256(&f.code_verifier, &pc.code_challenge) {
        return err_json(StatusCode::BAD_REQUEST, "invalid_grant", Some("PKCE failed"));
    }
    match sign_token(&st.key, &st.issuer, &pc.client_id, &pc.sub, st.token_ttl_sec) {
        Ok(tok) => {
            println!("[oosiam] issued token for {}", pc.sub.email);
            Json(json!({
                "access_token": tok,
                "id_token": tok,
                "token_type": "Bearer",
                "expires_in": st.token_ttl_sec,
            }))
            .into_response()
        }
        Err(e) => {
            eprintln!("[oosiam] token signing failed: {e}");
            err_json(StatusCode::INTERNAL_SERVER_ERROR, "server_error", None)
        }
    }
}

// ── Login form ────────────────────────────────────

struct FormParams {
    client_id: String,
    redirect_uri: String,
    code_challenge: String,
    state: String,
    error: Option<String>,
}

const FORM_HEAD: &str = r#"<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Onisin — Sign in</title>
<style>
  body{font-family:system-ui,sans-serif;max-width:360px;margin:4em auto;color:#1a1a1a;padding:0 1em}
  h1{font-size:1.25em;margin-bottom:1em}
  label{display:block;font-size:.85em;margin:.8em 0 .25em;color:#555}
  input{width:100%;box-sizing:border-box;padding:.55em .7em;font-size:1em;border:1px solid #ccc;border-radius:6px}
  button{margin-top:1.4em;width:100%;padding:.6em;font-size:1em;border:0;border-radius:6px;background:#1a1a1a;color:#fff;cursor:pointer}
  .err{background:#fdecea;border:1px solid #f5c2c0;color:#9a1c14;padding:.5em .7em;border-radius:6px;font-size:.85em}
</style></head><body>
<h1>Sign in to Onisin</h1>
"#;

fn render_form(p: &FormParams) -> Response {
    if p.client_id.is_empty() || p.redirect_uri.is_empty() || p.code_challenge.is_empty() {
        return (StatusCode::BAD_REQUEST, "missing required OAuth parameters").into_response();
    }
    let error_html = match &p.error {
        Some(e) => format!("<p class=\"err\">{}</p>", esc(e)),
        None => String::new(),
    };
    let body = format!(
        "{head}{error}<form method=\"post\" action=\"/auth\">\n\
         <input type=\"hidden\" name=\"client_id\" value=\"{cid}\">\n\
         <input type=\"hidden\" name=\"redirect_uri\" value=\"{ruri}\">\n\
         <input type=\"hidden\" name=\"code_challenge\" value=\"{cc}\">\n\
         <input type=\"hidden\" name=\"state\" value=\"{state}\">\n\
         <label for=\"email\">Email</label>\n\
         <input id=\"email\" name=\"email\" type=\"email\" autocomplete=\"username\" autofocus required>\n\
         <label for=\"password\">Password</label>\n\
         <input id=\"password\" name=\"password\" type=\"password\" autocomplete=\"current-password\" required>\n\
         <button type=\"submit\">Sign in</button>\n\
         </form></body></html>",
        head = FORM_HEAD,
        error = error_html,
        cid = esc(&p.client_id),
        ruri = esc(&p.redirect_uri),
        cc = esc(&p.code_challenge),
        state = esc(&p.state),
    );
    let status = if p.error.is_some() { StatusCode::UNAUTHORIZED } else { StatusCode::OK };
    (status, Html(body)).into_response()
}

// ── Small helpers ─────────────────────────────────

fn err_json(status: StatusCode, error: &str, desc: Option<&str>) -> Response {
    let body = match desc {
        Some(d) => json!({ "error": error, "error_description": d }),
        None => json!({ "error": error }),
    };
    (status, Json(body)).into_response()
}

fn now_sec() -> u64 {
    SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_secs()).unwrap_or(0)
}

/// 32 hex characters from the OS CSPRNG — the single-use authorization
/// code (matches the Bun version's dash-stripped UUID length).
fn new_code() -> String {
    let mut bytes = [0u8; 16];
    OsRng.fill_bytes(&mut bytes);
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

/// Minimal percent-encoding for a URL query value. The code is hex
/// (already safe); state is opaque client data, so encode anything that
/// is not an RFC 3986 unreserved character.
fn urlencode(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => out.push(b as char),
            _ => out.push_str(&format!("%{b:02X}")),
        }
    }
    out
}

/// Escapes the four HTML attribute-significant characters.
fn esc(s: &str) -> String {
    s.replace('&', "&amp;").replace('<', "&lt;").replace('>', "&gt;").replace('\"', "&quot;")
}
