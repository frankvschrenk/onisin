//! Model resolution: a developer points at either a local directory or a
//! Hugging Face repo, and we make the needed files available on disk. This is
//! the bit that frees customers from a bundled model zoo — bring your own from
//! HF, or keep weights local.

use std::path::{Path, PathBuf};

use anyhow::{Context, Result};

/// Where a model comes from.
#[derive(Debug, Clone)]
pub enum ModelRef {
    /// A directory on disk that already holds the model files.
    Local(PathBuf),
    /// A Hugging Face repo, optionally pinned to a revision via `repo@rev`.
    Hf { repo: String, revision: String },
}

impl ModelRef {
    /// An existing path is local; otherwise the string is an HF repo id, with
    /// an optional `@revision` suffix.
    pub fn parse(s: &str) -> Self {
        if Path::new(s).exists() {
            return ModelRef::Local(PathBuf::from(s));
        }
        match s.split_once('@') {
            Some((repo, revision)) => ModelRef::Hf {
                repo: repo.to_string(),
                revision: revision.to_string(),
            },
            None => ModelRef::Hf {
                repo: s.to_string(),
                revision: "main".to_string(),
            },
        }
    }
}

/// Resolved on-disk files every backend needs. Weight files are fetched by the
/// backend itself (it knows which format/shards it wants); here we guarantee
/// the tokenizer and config, which are common to all of them.
#[derive(Debug, Clone)]
pub struct ModelFiles {
    pub dir: PathBuf,
    pub tokenizer_json: PathBuf,
    pub config_json: PathBuf,
}

/// Ensure the model is present locally, downloading from HF into the local
/// hf-hub cache when needed.
pub fn resolve(model: &ModelRef) -> Result<ModelFiles> {
    match model {
        ModelRef::Local(dir) => Ok(ModelFiles {
            tokenizer_json: dir.join("tokenizer.json"),
            config_json: dir.join("config.json"),
            dir: dir.clone(),
        }),
        ModelRef::Hf { repo, revision } => {
            use hf_hub::api::sync::ApiBuilder;
            use hf_hub::{Repo, RepoType};

            let api = ApiBuilder::new().build().context("init hf-hub api")?;
            let handle = api.repo(Repo::with_revision(
                repo.clone(),
                RepoType::Model,
                revision.clone(),
            ));
            let tokenizer_json = handle.get("tokenizer.json").context("fetch tokenizer.json")?;
            let config_json = handle.get("config.json").context("fetch config.json")?;
            let dir = tokenizer_json
                .parent()
                .map(Path::to_path_buf)
                .unwrap_or_default();
            Ok(ModelFiles {
                dir,
                tokenizer_json,
                config_json,
            })
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_hf_and_local_refs() {
        match ModelRef::parse("mlx-community/gemma-4-12B-it-qat-4bit") {
            ModelRef::Hf { repo, revision } => {
                assert_eq!(repo, "mlx-community/gemma-4-12B-it-qat-4bit");
                assert_eq!(revision, "main");
            }
            other => panic!("expected HF ref, got {other:?}"),
        }
        match ModelRef::parse("org/model@v2") {
            ModelRef::Hf { repo, revision } => {
                assert_eq!(repo, "org/model");
                assert_eq!(revision, "v2");
            }
            other => panic!("expected HF ref, got {other:?}"),
        }
        // The current directory exists, so it resolves as local.
        assert!(matches!(ModelRef::parse("."), ModelRef::Local(_)));
    }
}
