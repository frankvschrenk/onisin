#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

// Thin shim: the real entry point lives in the lib so the mobile entry-point
// macro can reuse it.
fn main() {
    ooso_lib::run();
}
