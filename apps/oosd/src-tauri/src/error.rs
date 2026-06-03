//! Command error type. thiserror internally (with #[from] for the libraries
//! the commands touch); commands map it to a flat String at the invoke
//! boundary, because Tauri requires a Serialize error and a plain message is
//! what the webview surfaces to the user.

use thiserror::Error;

#[derive(Debug, Error)]
pub enum CmdError {
    #[error("http request failed: {0}")]
    Http(#[from] reqwest::Error),
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),
    #[error("{0}")]
    Msg(String),
}
