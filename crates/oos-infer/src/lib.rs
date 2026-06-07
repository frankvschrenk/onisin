//! oos-infer — backend-agnostic core for the onisin inference engines.
//!
//! Why this crate exists: oosmlx (Apple/MLX) and ooscuda (NVIDIA) should differ
//! only in their forward pass. Everything else — the OpenAI-compatible HTTP
//! API, resolving a model from a local directory or a Hugging Face repo, the
//! Engine trait and the request plumbing — lives here once, so both engines
//! expose the same surface and a customer can swap hardware without changing
//! how they call onisin.

pub mod engine;
pub mod openai;
pub mod registry;
pub mod server;

pub use engine::{Engine, GenParams, Generation};
pub use registry::{resolve, ModelFiles, ModelRef};
