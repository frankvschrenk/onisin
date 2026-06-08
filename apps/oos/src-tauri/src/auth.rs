//! OIDC token handling for the oos shell: decode claims, resolve the
//! OOS role, and store/read/clear the id_token in the OS keychain.
//!
//! The raw token never reaches the webview — it is exchanged and stored
//! natively (keychain), and only the decoded claims (email / role /
//! groups) are handed up. Decoding does NOT verify the RS256 signature:
//! the keychain is the trust boundary for a stored token, exactly as the
//! Bun decodeToken path did. Verifying against the issuer JWKS is a
//! separate hardening step, not done here.
//!
//! Role resolution mirrors the Bun resolveRole / Go helper priority
//! table: admin > manager > user. Each group string is split on "-" and
//! the highest-ranked keyword found wins.

use data_encoding::BASE64URL_NOPAD;
use keyring::Entry;
use serde::Serialize;

/// Keychain coordinates. One entry holds the current session's id_token.
const KEYCHAIN_SERVICE: &str = "com.onisin.oos";
const KEYCHAIN_ACCOUNT: &str = "oidc-token";

/// OOS-relevant claims decoded from an id_token.
#[derive(Clone, Default, Serialize)]
pub struct OosClaims {
    pub email: String,
    pub username: String,
    pub groups: Vec<String>,
    /// Resolved OOS role: "admin" | "manager" | "user" | "".
    pub role: String,
    /// Token expiry (unix seconds), 0 when absent.
    pub exp: i64,
}

/// Highest-priority OOS role from IAM group strings, splitting each on
/// "-". Returns "" when no group matches a known role keyword.
pub fn resolve_role(groups: &[String]) -> String {
    let mut best: &'static str = "";
    let mut best_prio = 0u8;
    for group in groups {
        for part in group.split('-') {
            let (keyword, prio): (&'static str, u8) = match part {
                "admin" => ("admin", 3),
                "manager" => ("manager", 2),
                "user" => ("user", 1),
                _ => ("", 0),
            };
            if prio > best_prio {
                best = keyword;
                best_prio = prio;
            }
        }
    }
    best.to_string()
}

/// Decodes a JWT payload (NO signature verification) into OOS claims.
/// Returns None when the token is missing or malformed.
pub fn decode_claims(token: &str) -> Option<OosClaims> {
    if token.is_empty() {
        return None;
    }
    let parts: Vec<&str> = token.split('.').collect();
    if parts.len() != 3 {
        return None;
    }
    // JWT segments are base64url WITHOUT padding; tolerate stray padding.
    let payload_b64 = parts[1].trim_end_matches('=');
    let bytes = BASE64URL_NOPAD.decode(payload_b64.as_bytes()).ok()?;
    let payload: serde_json::Value = serde_json::from_slice(&bytes).ok()?;
    Some(extract_claims(&payload))
}

/// Maps a raw JWT payload to OosClaims, deriving username and groups the
/// way the Bun extractClaims did (preferred_username → email local-part;
/// `groups` claim → synthetic group from a role-named username).
fn extract_claims(payload: &serde_json::Value) -> OosClaims {
    let email = payload.get("email").and_then(|v| v.as_str()).unwrap_or_default().to_string();
    let pref = payload.get("preferred_username").and_then(|v| v.as_str()).unwrap_or_default().to_string();

    let mut username = pref;
    if username.is_empty() && !email.is_empty() {
        username = match email.find('@') {
            Some(at) if at > 0 => email[..at].to_string(),
            _ => email.clone(),
        };
    }

    let mut groups: Vec<String> = payload
        .get("groups")
        .and_then(|v| v.as_array())
        .map(|arr| {
            arr.iter()
                .filter_map(|g| g.as_str())
                .filter(|s| !s.is_empty())
                .map(String::from)
                .collect()
        })
        .unwrap_or_default();

    // Last resort: a role-named username becomes a synthetic group, so a
    // minimal IdP that issues no groups still resolves a role.
    if groups.is_empty() {
        match username.to_lowercase().as_str() {
            "admin" => groups = vec!["oos-admin".to_string()],
            "manager" => groups = vec!["oos-manager".to_string()],
            "user" => groups = vec!["oos-user".to_string()],
            _ => {}
        }
    }

    let exp = payload.get("exp").and_then(|v| v.as_i64()).unwrap_or(0);
    let role = resolve_role(&groups);
    OosClaims { email, username, groups, role, exp }
}

// ── Keychain-backed token storage ──────────────────────────

fn entry() -> Result<Entry, keyring::Error> {
    Entry::new(KEYCHAIN_SERVICE, KEYCHAIN_ACCOUNT)
}

/// Stores the id_token in the OS keychain, replacing any previous one.
pub fn store_token(token: &str) -> Result<(), keyring::Error> {
    entry()?.set_password(token)
}

/// Reads the stored id_token, or None when there is none (or the
/// keychain is unreachable — treated as "not logged in").
pub fn load_token() -> Option<String> {
    entry().ok()?.get_password().ok()
}

/// Clears the stored token (logout). Idempotent: a missing entry is a
/// success, not an error.
pub fn clear_token() -> Result<(), keyring::Error> {
    match entry()?.delete_credential() {
        Ok(()) | Err(keyring::Error::NoEntry) => Ok(()),
        Err(e) => Err(e),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use data_encoding::BASE64URL_NOPAD as B64;

    fn make_token(payload: &serde_json::Value) -> String {
        let h = B64.encode(br#"{"alg":"RS256","typ":"JWT"}"#);
        let p = B64.encode(serde_json::to_vec(payload).unwrap().as_slice());
        format!("{h}.{p}.sig")
    }

    #[test]
    fn resolves_priority() {
        assert_eq!(resolve_role(&["oos-admin".into()]), "admin");
        assert_eq!(resolve_role(&["oos-user".into(), "oos-admin".into()]), "admin");
        assert_eq!(resolve_role(&["oos-user".into()]), "user");
        assert_eq!(resolve_role(&["sales".into()]), "");
    }

    #[test]
    fn decodes_groups_and_role() {
        let tok = make_token(&serde_json::json!({
            "email": "pkce_probe@oos.local",
            "preferred_username": "Probe",
            "groups": ["oos-probe", "oos-admin"],
            "exp": 1780621465i64
        }));
        let c = decode_claims(&tok).expect("decodes");
        assert_eq!(c.email, "pkce_probe@oos.local");
        assert_eq!(c.username, "Probe");
        assert_eq!(c.role, "admin");
        assert_eq!(c.exp, 1780621465);
    }

    #[test]
    fn synthetic_group_from_username() {
        let tok = make_token(&serde_json::json!({ "email": "admin@oos.local" }));
        let c = decode_claims(&tok).expect("decodes");
        assert_eq!(c.username, "admin");
        assert_eq!(c.role, "admin");
    }

    #[test]
    fn rejects_malformed() {
        assert!(decode_claims("").is_none());
        assert!(decode_claims("only.two").is_none());
    }
}
