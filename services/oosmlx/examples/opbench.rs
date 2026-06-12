//! Op micro-bench through the vendored mlx-rs build, f32 vs bf16.
//!
//! Exists to split "our Metal build is slow on bf16" from "our model graph
//! composes badly on bf16": the same shapes measured through PyPI mlx 0.30.6
//! are fast in bf16, so if these numbers come out slow the problem is the
//! vendored mlx-sys build, not the model code.
//!
//!   cargo run -p oosmlx --release --features mlx --example opbench

use mlx_rs::{fast, ops, Array, Dtype};
use std::time::Instant;

fn bench(label: &str, mut f: impl FnMut() -> Array, iters: u32) {
    for _ in 0..30 {
        f().eval().unwrap();
    }
    let t0 = Instant::now();
    for _ in 0..iters {
        f().eval().unwrap();
    }
    let ms = t0.elapsed().as_secs_f64() / iters as f64 * 1e3;
    println!("{label:<34} {ms:7.3} ms");
}

fn main() {
    for dt in [Dtype::Float32, Dtype::Bfloat16] {
        let name = format!("{dt:?}");

        // qmv: tied head [262144, 2816] affine 4bit/g64, x [1, 2816]
        let wf = mlx_rs::random::normal::<f32>(&[262144, 2816], None, None, None).unwrap();
        let (w, s, b) = ops::quantize(&wf, 64, 4, None).unwrap();
        let s = s.as_dtype(dt).unwrap();
        let b = b.as_dtype(dt).unwrap();
        let x = mlx_rs::random::normal::<f32>(&[1, 2816], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        for a in [&w, &s, &b, &x] {
            a.eval().unwrap();
        }
        bench(
            &format!("qmv tied-head {name}"),
            || ops::quantized_matmul(&x, &w, &s, Some(&b), true, 64, 4, None).unwrap(),
            200,
        );

        // sdpa decode, sliding geometry: q [1,16,1,256], K/V [1,8,1024,256]
        let q = mlx_rs::random::normal::<f32>(&[1, 16, 1, 256], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        let k = mlx_rs::random::normal::<f32>(&[1, 8, 1024, 256], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        let v = mlx_rs::random::normal::<f32>(&[1, 8, 1024, 256], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        for a in [&q, &k, &v] {
            a.eval().unwrap();
        }
        bench(
            &format!("sdpa sliding {name}"),
            || fast::scaled_dot_product_attention(&q, &k, &v, 1.0, None, None).unwrap(),
            300,
        );

        // sdpa decode, full geometry: q [1,16,1,512], K/V [1,2,4096,512]
        let qf = mlx_rs::random::normal::<f32>(&[1, 16, 1, 512], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        let kf = mlx_rs::random::normal::<f32>(&[1, 2, 4096, 512], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        let vf = mlx_rs::random::normal::<f32>(&[1, 2, 4096, 512], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        for a in [&qf, &kf, &vf] {
            a.eval().unwrap();
        }
        bench(
            &format!("sdpa full {name}"),
            || fast::scaled_dot_product_attention(&qf, &kf, &vf, 1.0, None, None).unwrap(),
            300,
        );

        // gather_qmm expert geometry: [128, 704, 2816] 4bit/g64, top-8
        let ef = mlx_rs::random::normal::<f32>(&[128, 704, 2816], None, None, None).unwrap();
        let (ew, es, eb) = ops::quantize(&ef, 64, 4, None).unwrap();
        let es = es.as_dtype(dt).unwrap();
        let eb = eb.as_dtype(dt).unwrap();
        let ex = mlx_rs::random::normal::<f32>(&[1, 1, 1, 2816], None, None, None)
            .unwrap()
            .as_dtype(dt)
            .unwrap();
        let idx = Array::from_slice(&[0u32, 5, 17, 33, 64, 90, 101, 120], &[1, 8]);
        for a in [&ew, &es, &eb, &ex, &idx] {
            a.eval().unwrap();
        }
        bench(
            &format!("gather_qmm {name}"),
            || {
                ops::gather_qmm(
                    &ex,
                    &ew,
                    &es,
                    Some(&eb),
                    None,
                    Some(&idx),
                    true,
                    64,
                    4,
                    false,
                    None,
                )
                .unwrap()
            },
            300,
        );
    }
}
