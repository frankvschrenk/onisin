//! Error type shared by the native commands.
//
// CmdError keeps the command bodies terse: reqwest failures convert with `?`,
// and Msg carries the hand-built messages. Commands map it to String at the
// boundary because Tauri serialises command errors as strings to the webview.

use thiserror::Error;

#[derive(Debug, Error)]
pub enum CmdError {
    #[error("{0}")]
    Msg(String),
    #[error(transparent)]
    Reqwest(#[from] reqwest::Error),
    #[error(transparent)]
    Io(#[from] std::io::Error),
}
