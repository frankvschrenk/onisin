//! Native Tauri commands the oos webview invokes for the few operations a
//! browser context cannot do itself. NATS / JetStream-KV / chat & turn
//! persistence stay webview-direct (see mainview/rpc.ts); only genuinely
//! native calls land here: server-side LLM HTTP (no CORS, key off the fetch
//! path), the stable instance identity, opening a URL in the OS browser, and
//! the OAuth2 + PKCE login dance (loopback redirect + token exchange +
//! keychain-backed token storage), which a webview cannot perform.

use std::fs;
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use data_encoding::{BASE32_NOPAD, BASE64URL_NOPAD};
use rand::rngs::OsRng;
use rand::RngCore;
use sha2::{Digest, Sha256};
use tauri::{AppHandle, Manager};
use tauri_plugin_opener::OpenerExt;

use crate::auth;
use crate::error::CmdError;

/// Fetch model ids from an OpenAI-compatible /v1/models endpoint.
///
/// Server-side so the key never rides a browser fetch and CORS is moot; this
/// is why the call is native rather than a webview fetch.
#[tauri::command]
pub async fn list_models(base_url: String, api_key: String) -> Result<ModelList, String> {
    fetch_models(&base_url, &api_key)
        .await
        .map(|models| ModelList { models })
        .map_err(|e| e.to_string())
}

/// Response shape mirrors the seam's expectation ({ models: string[] }).
#[derive(serde::Serialize)]
pub struct ModelList {
    pub models: Vec<String>,
}

async fn fetch_models(base_url: &str, api_key: &str) -> Result<Vec<String>, CmdError> {
    let url = format!("{}/v1/models", base_url.trim_end_matches('/'));
    let key = if api_key.is_empty() { "sk-no-key" } else { api_key };
    let res = reqwest::Client::new().get(&url).bearer_auth(key).send().await?;
    if !res.status().is_success() {
        return Err(CmdError::Msg(format!(
            "models endpoint returned HTTP {}",
            res.status().as_u16()
        )));
    }
    let body: serde_json::Value = res.json().await?;
    // OpenAI shape is { data: [{ id }] }; tolerate a bare array too.
    let items = body
        .get("data")
        .and_then(|d| d.as_array())
        .or_else(|| body.as_array());
    let mut ids: Vec<String> = match items {
        Some(items) => items
            .iter()
            .filter_map(|m| m.get("id").and_then(|i| i.as_str()).map(String::from))
            .collect(),
        None => Vec::new(),
    };
    ids.sort();
    Ok(ids)
}

/// Return this instance's stable 52-char Base32 node id, minting it on first
/// run. Persisted as a plain file under the app-data dir so it survives
/// restarts; the webview treats it as an opaque identity string.
///
/// Why not a real Ed25519 keypair (as the old oos-node-id-ts burned into the
/// binary): the webview only consumes the public id string, so a persisted
/// 32-byte random identity reproduces the exact contract. Swapping in a
/// dalek keypair later is a drop-in change behind this command.
#[tauri::command]
pub fn node_id(app: tauri::AppHandle) -> Result<String, String> {
    node_id_impl(&app).map_err(|e| e.to_string())
}

fn node_id_impl(app: &tauri::AppHandle) -> Result<String, CmdError> {
    let dir = app
        .path()
        .app_data_dir()
        .map_err(|e| CmdError::Msg(e.to_string()))?;
    fs::create_dir_all(&dir)?;
    let path = dir.join("node-id");
    if let Ok(existing) = fs::read_to_string(&path) {
        let trimmed = existing.trim();
        if !trimmed.is_empty() {
            return Ok(trimmed.to_string());
        }
    }
    let mut bytes = [0u8; 32];
    rand::thread_rng().fill_bytes(&mut bytes);
    let id = BASE32_NOPAD.encode(&bytes);
    fs::write(&path, &id)?;
    Ok(id)
}

/// Open a URL in the user's default browser via the opener plugin.
#[tauri::command]
pub fn open_external_url(app: tauri::AppHandle, url: String) -> Result<(), String> {
    app.opener()
        .open_url(url, None::<&str>)
        .map_err(|e| e.to_string())
}

// ── Auth: OAuth2 authorization-code + PKCE login ─────────────────
//
// start_login runs the whole dance natively because a webview can neither
// open a loopback HTTP listener for the redirect nor keep the token off the
// JS heap. The webview passes the IdP coordinates it read from the oos-iam KV
// bucket; this command discovers the endpoints, drives the browser login,
// exchanges the code, stores the id_token in the OS keychain, and returns
// only the decoded claims.

/// Auth status handed to the webview — decoded claims, never the raw token.
#[derive(Default, serde::Serialize)]
pub struct AuthStatus {
    pub authenticated: bool,
    /// Raw id_token, surfaced so the webview can decode display claims as
    /// the Bun model did; the keychain remains the at-rest store. Empty
    /// when not authenticated.
    pub token: String,
    pub email: String,
    pub username: String,
    pub role: String,
    pub groups: Vec<String>,
}

/// Runs the OAuth2 + PKCE login against the given issuer and returns the
/// authenticated user's claims. The id_token is stored in the keychain.
#[tauri::command]
pub async fn start_login(
    app: AppHandle,
    issuer: String,
    client_id: String,
    redirect_uri: String,
) -> Result<AuthStatus, String> {
    start_login_impl(&app, &issuer, &client_id, &redirect_uri)
        .await
        .map_err(|e| e.to_string())
}

async fn start_login_impl(
    app: &AppHandle,
    issuer: &str,
    client_id: &str,
    redirect_uri: &str,
) -> Result<AuthStatus, CmdError> {
    // 1. Discover the authorization + token endpoints.
    let disc: serde_json::Value = reqwest::Client::new()
        .get(format!("{}/.well-known/openid-configuration", issuer.trim_end_matches('/')))
        .send()
        .await?
        .json()
        .await?;
    let auth_ep = disc
        .get("authorization_endpoint")
        .and_then(|v| v.as_str())
        .ok_or_else(|| CmdError::Msg("discovery missing authorization_endpoint".into()))?
        .to_string();
    let token_ep = disc
        .get("token_endpoint")
        .and_then(|v| v.as_str())
        .ok_or_else(|| CmdError::Msg("discovery missing token_endpoint".into()))?
        .to_string();

    // 2. PKCE verifier/challenge (S256) + anti-forgery state.
    let verifier = rand_b64url(32);
    let challenge = BASE64URL_NOPAD.encode(&Sha256::digest(verifier.as_bytes()));
    let state = rand_b64url(16);

    // 3. Bind the loopback listener BEFORE opening the browser so the
    //    redirect can never beat us to the port.
    let port = parse_port(redirect_uri)
        .ok_or_else(|| CmdError::Msg("redirect_uri has no port".into()))?;
    let listener = TcpListener::bind(("127.0.0.1", port))?;

    // 4. Hand the user off to the IdP in their default browser.
    let auth_url = format!(
        "{auth_ep}?response_type=code&client_id={cid}&redirect_uri={ruri}\
         &code_challenge={ch}&code_challenge_method=S256&state={st}&scope={scope}",
        cid = urlencode(client_id),
        ruri = urlencode(redirect_uri),
        ch = challenge,
        st = urlencode(&state),
        scope = urlencode("openid profile email"),
    );
    app.opener()
        .open_url(auth_url, None::<&str>)
        .map_err(|e| CmdError::Msg(format!("could not open browser: {e}")))?;

    // 5. Wait for the redirect on a blocking thread (a login can take
    //    minutes), validating state and returning the code.
    let want_state = state.clone();
    let code = match tauri::async_runtime::spawn_blocking(move || {
        wait_for_code(listener, &want_state, Duration::from_secs(300))
    })
    .await
    {
        Ok(inner) => inner?,
        Err(e) => return Err(CmdError::Msg(format!("callback task failed: {e:?}"))),
    };

    // 6. Exchange the code + verifier for tokens.
    let token_resp: serde_json::Value = reqwest::Client::new()
        .post(&token_ep)
        .form(&[
            ("grant_type", "authorization_code"),
            ("code", code.as_str()),
            ("client_id", client_id),
            ("redirect_uri", redirect_uri),
            ("code_verifier", verifier.as_str()),
        ])
        .send()
        .await?
        .json()
        .await?;
    let id_token = token_resp
        .get("id_token")
        .and_then(|v| v.as_str())
        .or_else(|| token_resp.get("access_token").and_then(|v| v.as_str()))
        .ok_or_else(|| CmdError::Msg("token response missing id_token".into()))?
        .to_string();

    // 7. Persist in the keychain, return decoded claims only.
    auth::store_token(&id_token).map_err(|e| CmdError::Msg(format!("keychain store failed: {e}")))?;
    let claims = auth::decode_claims(&id_token)
        .ok_or_else(|| CmdError::Msg("issued token did not decode".into()))?;
    Ok(AuthStatus {
        authenticated: true,
        token: id_token,
        email: claims.email,
        username: claims.username,
        role: claims.role,
        groups: claims.groups,
    })
}

/// Returns the current auth status from the stored token (decoded, expiry
/// checked). authenticated=false when there is no token or it has expired.
#[tauri::command]
pub fn get_auth_status() -> AuthStatus {
    let Some(token) = auth::load_token() else { return AuthStatus::default() };
    let Some(claims) = auth::decode_claims(&token) else { return AuthStatus::default() };
    if claims.exp != 0 && claims.exp <= now_secs() {
        return AuthStatus::default();
    }
    AuthStatus {
        authenticated: true,
        token,
        email: claims.email,
        username: claims.username,
        role: claims.role,
        groups: claims.groups,
    }
}

/// Clears the stored token (logout). Idempotent.
#[tauri::command]
pub fn logout() -> Result<(), String> {
    auth::clear_token().map_err(|e| e.to_string())
}

// ── Auth helpers ───────────────────────────────────

/// One-shot loopback HTTP handler: waits for GET /callback?code=&state=,
/// validates state, replies with a self-closing page, returns the code.
/// Polls with a deadline so an abandoned login frees the port.
fn wait_for_code(listener: TcpListener, expected_state: &str, timeout: Duration) -> Result<String, CmdError> {
    listener
        .set_nonblocking(true)
        .map_err(|e| CmdError::Msg(format!("listener config failed: {e}")))?;
    let deadline = Instant::now() + timeout;
    loop {
        match listener.accept() {
            Ok((mut stream, _)) => {
                let target = read_request_target(&mut stream);
                if !target.starts_with("/callback") {
                    respond(&mut stream, 404, "not found");
                    continue;
                }
                let (mut code, mut state) = (None, None);
                if let Some(query) = target.split('?').nth(1) {
                    for pair in query.split('&') {
                        let mut it = pair.splitn(2, '=');
                        match (it.next(), it.next()) {
                            (Some("code"), Some(v)) => code = Some(urldecode(v)),
                            (Some("state"), Some(v)) => state = Some(urldecode(v)),
                            _ => {}
                        }
                    }
                }
                match (code, state) {
                    (Some(c), Some(s)) if s == expected_state => {
                        respond(
                            &mut stream,
                            200,
                            "<!doctype html><meta charset=\"utf-8\"><script>window.close()</script>\
                             <p>Login successful — you can close this window.</p>",
                        );
                        return Ok(c);
                    }
                    (Some(_), Some(_)) => {
                        respond(&mut stream, 400, "state mismatch");
                        return Err(CmdError::Msg("callback state mismatch".into()));
                    }
                    _ => {
                        respond(&mut stream, 400, "missing code");
                        return Err(CmdError::Msg("callback missing code".into()));
                    }
                }
            }
            Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock => {
                if Instant::now() > deadline {
                    return Err(CmdError::Msg("login timeout".into()));
                }
                std::thread::sleep(Duration::from_millis(150));
            }
            Err(e) => return Err(CmdError::Msg(format!("accept failed: {e}"))),
        }
    }
}

/// Reads the request line and returns its target (e.g. "/callback?code=..").
fn read_request_target(stream: &mut TcpStream) -> String {
    let _ = stream.set_nonblocking(false);
    let _ = stream.set_read_timeout(Some(Duration::from_secs(5)));
    let mut buf = [0u8; 4096];
    let n = stream.read(&mut buf).unwrap_or(0);
    let req = String::from_utf8_lossy(&buf[..n]);
    req.lines()
        .next()
        .and_then(|line| line.split(' ').nth(1))
        .unwrap_or("")
        .to_string()
}

/// Writes a minimal HTTP/1.1 response and closes.
fn respond(stream: &mut TcpStream, status: u16, body: &str) {
    let reason = match status {
        200 => "OK",
        404 => "Not Found",
        _ => "Bad Request",
    };
    let resp = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    );
    let _ = stream.write_all(resp.as_bytes());
    let _ = stream.flush();
}

/// n random bytes, base64url (no padding) — PKCE verifier / state.
fn rand_b64url(n: usize) -> String {
    let mut bytes = vec![0u8; n];
    OsRng.fill_bytes(&mut bytes);
    BASE64URL_NOPAD.encode(&bytes)
}

/// Extracts the port from a loopback redirect URI (http://host:PORT/path).
fn parse_port(redirect_uri: &str) -> Option<u16> {
    let after = redirect_uri.split("://").nth(1)?;
    let authority = after.split('/').next()?;
    authority.rsplit(':').next()?.parse::<u16>().ok()
}

/// Percent-encodes a query value to RFC 3986 unreserved characters.
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

/// Percent-decodes a query value (and '+' as space).
fn urldecode(s: &str) -> String {
    let bytes = s.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'%' if i + 2 < bytes.len() => match (hex_val(bytes[i + 1]), hex_val(bytes[i + 2])) {
                (Some(h), Some(l)) => {
                    out.push(h * 16 + l);
                    i += 3;
                }
                _ => {
                    out.push(b'%');
                    i += 1;
                }
            },
            b'+' => {
                out.push(b' ');
                i += 1;
            }
            c => {
                out.push(c);
                i += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

fn hex_val(b: u8) -> Option<u8> {
    match b {
        b'0'..=b'9' => Some(b - b'0'),
        b'a'..=b'f' => Some(b - b'a' + 10),
        b'A'..=b'F' => Some(b - b'A' + 10),
        _ => None,
    }
}

fn now_secs() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}
