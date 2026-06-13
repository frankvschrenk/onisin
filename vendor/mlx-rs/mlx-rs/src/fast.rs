//! Fast implementations of commonly used multi-op functions.

use std::ffi::CStr;

use crate::error::Result;
use crate::utils::guard::Guarded;
use crate::utils::{IntoOption, VectorArray, SUCCESS};
use crate::{Array, Stream};
use mlx_internal_macros::{default_device, generate_macro};

/// Optimized implementation of `NN.RoPE`.
#[allow(clippy::too_many_arguments)]
#[generate_macro(customize(root = "$crate::fast"))]
#[default_device]
pub fn rope_device<'a>(
    #[named] array: impl AsRef<Array>,
    #[named] dimensions: i32,
    #[named] traditional: bool,
    #[optional] base: impl Into<Option<f32>>,
    #[named] scale: f32,
    #[named] offset: i32,
    #[optional] freqs: impl Into<Option<&'a Array>>,
    #[optional] stream: impl AsRef<Stream>,
) -> Result<Array> {
    let base = base.into();
    let base = mlx_sys::mlx_optional_float {
        value: base.unwrap_or(0.0),
        has_value: base.is_some(),
    };
    let freqs = freqs.into();
    Array::try_from_op(|res| unsafe {
        mlx_sys::mlx_fast_rope(
            res,
            array.as_ref().as_ptr(),
            dimensions,
            traditional,
            base,
            scale,
            offset,
            freqs
                .map(|a| a.as_ptr())
                .unwrap_or(mlx_sys::mlx_array_new()),
            stream.as_ref().as_ptr(),
        )
    })
}

/// Optimized implementation of `NN.RoPE` with dynamic (array) offset.
///
/// This variant allows specifying the offset as an array, enabling different
/// offsets for different positions in the input.
///
/// # Params
///
/// - `array`: Input array
/// - `dimensions`: The feature dimensions to apply rope to
/// - `traditional`: If true, uses the traditional rope implementation
/// - `base`: The base used to compute angular frequency for each dimension
/// - `scale`: The scale to apply to the positions
/// - `offset`: An array of position offsets
/// - `freqs`: Optional precomputed frequencies
/// - `stream`: Stream to evaluate on
#[allow(clippy::too_many_arguments)]
#[generate_macro(customize(root = "$crate::fast"))]
#[default_device]
pub fn rope_dynamic_device<'a>(
    #[named] array: impl AsRef<Array>,
    #[named] dimensions: i32,
    #[named] traditional: bool,
    #[optional] base: impl Into<Option<f32>>,
    #[named] scale: f32,
    #[named] offset: impl AsRef<Array>,
    #[optional] freqs: impl Into<Option<&'a Array>>,
    #[optional] stream: impl AsRef<Stream>,
) -> Result<Array> {
    let base = base.into();
    let base = mlx_sys::mlx_optional_float {
        value: base.unwrap_or(0.0),
        has_value: base.is_some(),
    };
    let freqs = freqs.into();
    Array::try_from_op(|res| unsafe {
        mlx_sys::mlx_fast_rope_dynamic(
            res,
            array.as_ref().as_ptr(),
            dimensions,
            traditional,
            base,
            scale,
            offset.as_ref().as_ptr(),
            freqs
                .map(|a| a.as_ptr())
                .unwrap_or(mlx_sys::mlx_array_new()),
            stream.as_ref().as_ptr(),
        )
    })
}

const DEFAULT_MASK_MODE: &CStr = c"";
const CAUSAL_MASK_MODE: &CStr = c"causal";

/// Mask modes for scaled dot product attention.
#[derive(Debug)]
pub enum ScaledDotProductAttentionMask<'a> {
    /// A single mask array
    Array(&'a Array),

    /// Causal masking (no explicit mask array needed)
    Causal,
}

impl<'a> From<&'a Array> for ScaledDotProductAttentionMask<'a> {
    fn from(mask: &'a Array) -> Self {
        ScaledDotProductAttentionMask::Array(mask)
    }
}

impl<'a> IntoOption<ScaledDotProductAttentionMask<'a>> for &'a Array {
    fn into_option(self) -> Option<ScaledDotProductAttentionMask<'a>> {
        Some(ScaledDotProductAttentionMask::Array(self))
    }
}

impl ScaledDotProductAttentionMask<'_> {
    fn as_mode_and_mask(&self) -> (&'static CStr, mlx_sys::mlx_array) {
        match self {
            ScaledDotProductAttentionMask::Array(mask) => (DEFAULT_MASK_MODE, mask.as_ptr()),
            ScaledDotProductAttentionMask::Causal => {
                (CAUSAL_MASK_MODE, unsafe { mlx_sys::mlx_array_new() })
            }
        }
    }
}

/// A fast implementation of multi-head attention: `O = softmax(Q @ K.T, dim=-1) @ V`
///
/// Supports [Multi-Head Attention](https://arxiv.org/abs/1706.03762), [Grouped Query Attention](https://arxiv.org/abs/2305.13245), and [Multi-Query Attention](https://arxiv.org/abs/1911.02150).
///
/// This function will dispatch to an optimized Metal kernel when the query sequence length is 1. It handles other cases with regular MLX operations.
///
/// > Note: The softmax operation is performed in float32 precision regardless of input precision (float16 or float32).
///
/// > Note: For Grouped Query Attention and Multi-Query Attention, the input arrays for `key` and `value` should not be pre-tiled to match the `query` array.
#[generate_macro(customize(root = "$crate::fast"))]
#[default_device]
pub fn scaled_dot_product_attention_device<'a>(
    queries: impl AsRef<Array>,
    keys: impl AsRef<Array>,
    values: impl AsRef<Array>,
    scale: f32,
    #[optional] mask: impl IntoOption<ScaledDotProductAttentionMask<'a>>,
    #[optional] sinks: impl Into<Option<&'a Array>>,
    #[optional] stream: impl AsRef<Stream>,
) -> Result<Array> {
    let (mask_mode, mask_arr) = mask.into_option().map_or_else(
        || (DEFAULT_MASK_MODE, unsafe { mlx_sys::mlx_array_new() }),
        |m| m.as_mode_and_mask(),
    );

    Array::try_from_op(|res| unsafe {
        mlx_sys::mlx_fast_scaled_dot_product_attention(
            res,
            queries.as_ref().as_ptr(),
            keys.as_ref().as_ptr(),
            values.as_ref().as_ptr(),
            scale,
            mask_mode.as_ptr(),
            mask_arr,
            sinks
                .into()
                .map(|a| a.as_ptr())
                .unwrap_or(mlx_sys::mlx_array_new()),
            stream.as_ref().as_ptr(),
        )
    })
}

/// Root Mean Square normalization (RMS norm).
///
/// The normalization is with respect to the last axis of the input `x`.
///
/// # Params
///
/// - x: input array
/// - weight: A multiplicative weight to scale the result by. The `weight` should be one-dimensional with the same size as the last axis of `x`.
/// - eps: A small additive constant for numerical stability
/// - stream: stream or device to evaluate on
#[generate_macro(customize(root = "$crate::fast"))]
#[default_device]
pub fn rms_norm_device(
    x: impl AsRef<Array>,
    weight: impl AsRef<Array>,
    eps: f32,
    #[optional] stream: impl AsRef<Stream>,
) -> Result<Array> {
    Array::try_from_op(|res| unsafe {
        mlx_sys::mlx_fast_rms_norm(
            res,
            x.as_ref().as_ptr(),
            weight.as_ref().as_ptr(),
            eps,
            stream.as_ref().as_ptr(),
        )
    })
}

/// Layer normalization.
///
/// The normalization is with respect to the last axis of the input `x`.
///
/// # Params
///
/// - x: input array
/// - weight: A multiplicative weight to scale the result by. The `weight` should be one-dimensional
///   with the same size as the last axis of `x`.  If not given no scaling will occur.
/// - bias: An additive offset to be added to the result. The `bias` should be one-dimensional
///   with the same size as the last axis of `x`.  It not given no offset will occur.
/// - eps: A small additive constant for numerical stability
/// - stream: stream or device to evaluate on
#[generate_macro(customize(root = "$crate::fast"))]
#[default_device]
pub fn layer_norm_device<'a>(
    #[named] x: impl AsRef<Array>,
    #[optional] weight: impl Into<Option<&'a Array>>,
    #[optional] bias: impl Into<Option<&'a Array>>,
    #[named] eps: f32,
    #[optional] stream: impl AsRef<Stream>,
) -> Result<Array> {
    Array::try_from_op(|res| unsafe {
        mlx_sys::mlx_fast_layer_norm(
            res,
            x.as_ref().as_ptr(),
            weight
                .into()
                .map(|a| a.as_ptr())
                .unwrap_or(mlx_sys::mlx_array_new()),
            bias.into()
                .map(|a| a.as_ptr())
                .unwrap_or(mlx_sys::mlx_array_new()),
            eps,
            stream.as_ref().as_ptr(),
        )
    })
}

// ---------------------------------------------------------------------------
// Custom Metal kernels (`mx.fast.metal_kernel`)
// ---------------------------------------------------------------------------

/// Convert to a `CString`, surfacing an interior-NUL byte as a normal error
// (the FFI strings must be NUL-terminated, so we cannot pass such input).
fn cstr(s: &str) -> Result<std::ffi::CString> {
    std::ffi::CString::new(s).map_err(|_| last_error_or("string contains an interior NUL byte"))
}

/// Build an `Exception` from the last MLX error, falling back to `fallback`
// when the C side reported failure without setting an error string.
#[track_caller]
fn last_error_or(fallback: &str) -> crate::error::Exception {
    let what = crate::error::get_and_clear_last_mlx_error()
        .map(|e| e.what)
        .unwrap_or_else(|| fallback.to_string());
    crate::error::Exception {
        what,
        location: std::panic::Location::caller(),
    }
}

/// Turn a C status code into a `Result`, pulling the MLX error on failure.
// The config setters return a status rather than an out-param, so they cannot
// go through the `Guarded::try_from_op` path the array-returning ops use.
#[track_caller]
fn check(status: i32) -> Result<()> {
    if status == SUCCESS {
        Ok(())
    } else {
        Err(last_error_or("metal kernel FFI call failed"))
    }
}

/// RAII handle over `mlx_vector_string`, used to ferry the kernel's input and
// output names across the FFI. The C side copies the strings into `std::string`,
// so this only has to outlive the `mlx_fast_metal_kernel_new` call.
struct VectorString {
    c_vec: mlx_sys::mlx_vector_string,
}

impl VectorString {
    fn try_from_strs(values: &[&str]) -> Result<Self> {
        // The empty vector is allocated first so `Drop` frees it even if a later
        // append fails midway.
        let this = Self {
            c_vec: unsafe { mlx_sys::mlx_vector_string_new() },
        };
        for v in values {
            let c = cstr(v)?;
            check(unsafe { mlx_sys::mlx_vector_string_append_value(this.c_vec, c.as_ptr()) })?;
        }
        Ok(this)
    }

    fn as_ptr(&self) -> mlx_sys::mlx_vector_string {
        self.c_vec
    }
}

impl Drop for VectorString {
    fn drop(&mut self) {
        let status = unsafe { mlx_sys::mlx_vector_string_free(self.c_vec) };
        debug_assert_eq!(status, SUCCESS);
    }
}

/// RAII handle over `mlx_fast_metal_kernel_config`, freed on drop so an early
// return while populating it (a failing setter) cannot leak the C object.
struct MetalKernelConfigGuard(mlx_sys::mlx_fast_metal_kernel_config);

impl MetalKernelConfigGuard {
    fn new() -> Result<Self> {
        let c = unsafe { mlx_sys::mlx_fast_metal_kernel_config_new() };
        if c.ctx.is_null() {
            return Err(last_error_or("failed to allocate metal kernel config"));
        }
        Ok(Self(c))
    }
}

impl Drop for MetalKernelConfigGuard {
    fn drop(&mut self) {
        unsafe { mlx_sys::mlx_fast_metal_kernel_config_free(self.0) };
    }
}

/// A compile-time template argument for a [`MetalKernel`].
///
/// Mirrors the `template=[(name, value)]` argument of `mx.fast.metal_kernel`:
/// each entry specialises a Metal `template <...>` parameter on a dtype, an
/// integer, or a boolean.
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum MetalKernelTemplateArg {
    /// A `typename` / dtype template parameter.
    Dtype(crate::Dtype),
    /// An integer (`int`) template parameter.
    Int(i32),
    /// A boolean (`bool`) template parameter.
    Bool(bool),
}

impl From<crate::Dtype> for MetalKernelTemplateArg {
    fn from(value: crate::Dtype) -> Self {
        Self::Dtype(value)
    }
}

impl From<i32> for MetalKernelTemplateArg {
    fn from(value: i32) -> Self {
        Self::Int(value)
    }
}

impl From<bool> for MetalKernelTemplateArg {
    fn from(value: bool) -> Self {
        Self::Bool(value)
    }
}

/// Per-invocation parameters for [`MetalKernel::apply`].
///
/// Everything that varies per launch (output sizes, launch geometry, template
// specialisation) lives here, so the compiled [`MetalKernel`] itself stays
// immutable and reusable across forward passes.
#[derive(Debug)]
pub struct MetalKernelConfig<'a> {
    /// Shape of every output array, in the same order as the kernel's
    /// `output_names`. Must be the same length as `output_dtypes`.
    pub output_shapes: &'a [&'a [i32]],
    /// Dtype of every output array, parallel to `output_shapes`.
    pub output_dtypes: &'a [crate::Dtype],
    /// Total launch grid `(x, y, z)` counted in threads — this is the MLX/Metal
    /// convention (total threads), not CUDA's threadgroup count.
    pub grid: (i32, i32, i32),
    /// Threadgroup size `(x, y, z)`.
    pub thread_group: (i32, i32, i32),
    /// Compile-time template arguments as `(name, value)` pairs.
    pub template_args: &'a [(&'a str, MetalKernelTemplateArg)],
    /// If set, every output element is initialised to this value before launch,
    /// letting a kernel write only the indices it owns (scatter / accumulate).
    pub init_value: Option<f32>,
    /// Print the generated Metal source to stderr — debugging only.
    pub verbose: bool,
}

impl<'a> MetalKernelConfig<'a> {
    /// Minimal config: outputs plus launch geometry, no templates / init / verbose.
    pub fn new(
        output_shapes: &'a [&'a [i32]],
        output_dtypes: &'a [crate::Dtype],
        grid: (i32, i32, i32),
        thread_group: (i32, i32, i32),
    ) -> Self {
        Self {
            output_shapes,
            output_dtypes,
            grid,
            thread_group,
            template_args: &[],
            init_value: None,
            verbose: false,
        }
    }
}

/// A custom GPU kernel compiled from inline Metal source, mirroring
/// `mx.fast.metal_kernel`.
///
/// Construct once with [`MetalKernel::new`] and launch repeatedly via
/// [`MetalKernel::apply`]; MLX caches the compiled pipeline by name + source,
/// so holding one instance across forward passes avoids recompilation. `source`
/// is the kernel *body* — MLX synthesises the `[[kernel]]` signature from the
/// input / output names and the standard thread-position attributes.
pub struct MetalKernel {
    // Reused across launches and meant to live inside a model struct. The
    // underlying CustomKernelFunction is an immutable, reference-counted
    // compiled-kernel handle.
    c_kernel: mlx_sys::mlx_fast_metal_kernel,
}

// SAFETY: mirrors `Array`'s own `unsafe impl Send`. The handle wraps an
// immutable, reference-counted compiled-kernel function that we never mutate
// after `new` and never share concurrently — callers keep it behind the
// engine's `Mutex`, exactly like the resident model it lives in.
unsafe impl Send for MetalKernel {}

impl std::fmt::Debug for MetalKernel {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        // The handle is an opaque compiled-kernel pointer with nothing useful to
        // print; expose the type without dereferencing it.
        f.debug_struct("MetalKernel").finish_non_exhaustive()
    }
}

impl MetalKernel {
    /// Compile a Metal kernel from inline source.
    ///
    /// `header` is prepended verbatim (helper functions, `#include`s).
    /// `ensure_row_contiguous` makes MLX copy non-contiguous inputs to row-major
    /// before launch; `atomic_outputs` declares the outputs as `atomic<T>` for
    /// kernels that accumulate across threads.
    #[allow(clippy::too_many_arguments)]
    pub fn new(
        name: &str,
        input_names: &[&str],
        output_names: &[&str],
        source: &str,
        header: &str,
        ensure_row_contiguous: bool,
        atomic_outputs: bool,
    ) -> Result<Self> {
        let name = cstr(name)?;
        let source = cstr(source)?;
        let header = cstr(header)?;
        let inputs = VectorString::try_from_strs(input_names)?;
        let outputs = VectorString::try_from_strs(output_names)?;

        // All arguments outlive the call; the C side copies what it keeps.
        let c_kernel = unsafe {
            mlx_sys::mlx_fast_metal_kernel_new(
                name.as_ptr(),
                inputs.as_ptr(),
                outputs.as_ptr(),
                source.as_ptr(),
                header.as_ptr(),
                ensure_row_contiguous,
                atomic_outputs,
            )
        };
        if c_kernel.ctx.is_null() {
            return Err(last_error_or("failed to create metal kernel"));
        }
        Ok(Self { c_kernel })
    }

    /// Launch the kernel on the default stream / device.
    pub fn apply<A: AsRef<Array>>(
        &self,
        inputs: &[A],
        config: &MetalKernelConfig,
    ) -> Result<Vec<Array>> {
        self.apply_device(inputs, config, crate::StreamOrDevice::default())
    }

    /// Launch the kernel on a specific stream / device.
    pub fn apply_device<A: AsRef<Array>>(
        &self,
        inputs: &[A],
        config: &MetalKernelConfig,
        stream: impl AsRef<Stream>,
    ) -> Result<Vec<Array>> {
        debug_assert_eq!(
            config.output_shapes.len(),
            config.output_dtypes.len(),
            "output_shapes and output_dtypes must be parallel"
        );

        // Inputs and config are built fresh per launch; the kernel handle is not.
        let inputs = VectorArray::try_from_iter(inputs.iter())?;
        let cfg = MetalKernelConfigGuard::new()?;

        for (shape, dtype) in config.output_shapes.iter().zip(config.output_dtypes) {
            check(unsafe {
                mlx_sys::mlx_fast_metal_kernel_config_add_output_arg(
                    cfg.0,
                    shape.as_ptr(),
                    shape.len(),
                    u32::from(*dtype) as mlx_sys::mlx_dtype,
                )
            })?;
        }

        let (gx, gy, gz) = config.grid;
        check(unsafe { mlx_sys::mlx_fast_metal_kernel_config_set_grid(cfg.0, gx, gy, gz) })?;
        let (tx, ty, tz) = config.thread_group;
        check(unsafe {
            mlx_sys::mlx_fast_metal_kernel_config_set_thread_group(cfg.0, tx, ty, tz)
        })?;

        if let Some(value) = config.init_value {
            check(unsafe { mlx_sys::mlx_fast_metal_kernel_config_set_init_value(cfg.0, value) })?;
        }
        if config.verbose {
            check(unsafe { mlx_sys::mlx_fast_metal_kernel_config_set_verbose(cfg.0, true) })?;
        }

        for (tname, arg) in config.template_args {
            let tname = cstr(tname)?;
            check(unsafe {
                match arg {
                    MetalKernelTemplateArg::Dtype(d) => {
                        mlx_sys::mlx_fast_metal_kernel_config_add_template_arg_dtype(
                            cfg.0,
                            tname.as_ptr(),
                            u32::from(*d) as mlx_sys::mlx_dtype,
                        )
                    }
                    MetalKernelTemplateArg::Int(v) => {
                        mlx_sys::mlx_fast_metal_kernel_config_add_template_arg_int(
                            cfg.0,
                            tname.as_ptr(),
                            *v,
                        )
                    }
                    MetalKernelTemplateArg::Bool(v) => {
                        mlx_sys::mlx_fast_metal_kernel_config_add_template_arg_bool(
                            cfg.0,
                            tname.as_ptr(),
                            *v,
                        )
                    }
                }
            })?;
        }

        // `_apply` writes the result vector into `res`; on a non-zero status
        // `try_from_op` extracts the MLX error message for us.
        Vec::<Array>::try_from_op(|res| unsafe {
            mlx_sys::mlx_fast_metal_kernel_apply(
                res,
                self.c_kernel,
                inputs.as_ptr(),
                cfg.0,
                stream.as_ref().as_ptr(),
            )
        })
    }
}

impl Drop for MetalKernel {
    fn drop(&mut self) {
        unsafe { mlx_sys::mlx_fast_metal_kernel_free(self.c_kernel) };
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        ops::indexing::{ArrayIndexOp, IndexOp},
        random::normal,
    };
    use float_eq::assert_float_eq;
    use pretty_assertions::assert_eq;

    #[test]
    fn test_rope() {
        crate::random::seed(71).unwrap();
        let a = crate::random::uniform::<_, f32>(0.0, 1.0, &[2, 8, 16], None).unwrap();
        assert_eq!(a.shape(), [2, 8, 16]);
        assert_eq!(a.dtype(), crate::Dtype::Float32);

        let result = rope(a, 8, false, 10000., 1.0, 0, None).unwrap();
        assert_eq!(result.shape(), [2, 8, 16]);
        assert_eq!(result.dtype(), crate::Dtype::Float32);
        assert_float_eq!(
            result.mean(None).unwrap().item::<f32>(),
            0.456_253_77,
            abs <= 0.009_125_075
        );
        assert_float_eq!(
            result.sum(None).unwrap().item::<f32>(),
            116.800_964,
            abs <= 2.336_019_3
        );
    }

    // Test adapted from Python test_fast.py/test_rope - the Python test accepts both
    // int offset and array offset, which in C/Rust are separate functions
    #[test]
    fn test_rope_dynamic() {
        crate::random::seed(71).unwrap();
        let a = crate::random::uniform::<_, f32>(0.0, 1.0, &[2, 8, 16], None).unwrap();
        assert_eq!(a.shape(), [2, 8, 16]);
        assert_eq!(a.dtype(), crate::Dtype::Float32);

        // Test with array offset - should produce similar results to int offset of 3
        let offset = crate::Array::from_int(3);
        let result = rope_dynamic(&a, 8, false, 10000., 1.0, &offset, None).unwrap();
        assert_eq!(result.shape(), [2, 8, 16]);
        assert_eq!(result.dtype(), crate::Dtype::Float32);

        // Compare with regular rope using int offset=3
        let result_int_offset = rope(&a, 8, false, 10000., 1.0, 3, None).unwrap();
        assert_eq!(result_int_offset.shape(), [2, 8, 16]);

        // The results should be close
        let diff = &result - &result_int_offset;
        let max_diff = diff.abs().unwrap().max(None).unwrap().item::<f32>();
        assert!(max_diff < 1e-5, "Max difference was {}", max_diff);
    }

    #[test]
    fn test_rms_norm() {
        crate::random::seed(103).unwrap();
        let a = crate::random::uniform::<_, f32>(0.0, 1.0, &[2, 8, 16], None).unwrap();
        assert_eq!(a.shape(), [2, 8, 16]);
        assert_eq!(a.dtype(), crate::Dtype::Float32);

        let weight = Array::ones::<f32>(&[16]).unwrap();
        let result = rms_norm(a, weight, 1e-5).unwrap();
        assert_eq!(result.shape(), [2, 8, 16]);
        assert_eq!(result.dtype(), crate::Dtype::Float32);
        assert_float_eq!(
            result.mean(None).unwrap().item::<f32>(),
            0.872_938_75,
            abs <= 0.017_458_774
        );
        assert_float_eq!(
            result.sum(None).unwrap().item::<f32>(),
            223.472_32,
            abs <= 4.469_446
        );
    }

    #[test]
    pub fn test_layer_norm_affine() {
        crate::random::seed(635).unwrap();
        let a = crate::random::uniform::<_, f32>(0.0, 1.0, &[2, 8, 16], None).unwrap();
        assert_eq!(a.shape(), [2, 8, 16]);
        assert_eq!(a.dtype(), crate::Dtype::Float32);

        let weight = Array::ones::<f32>(&[16]).unwrap();
        let bias = Array::zeros::<f32>(&[16]).unwrap();
        let result = layer_norm(a, &weight, &bias, 1e-5).unwrap();
        let result = result.index((ArrayIndexOp::Ellipsis, 0));
        assert_eq!(result.shape(), [2, 8]);
        assert_eq!(result.dtype(), crate::Dtype::Float32);
        assert_float_eq!(
            result.mean(None).unwrap().item::<f32>(),
            0.290_990_38,
            abs <= 0.005_819_807_8
        );
        assert_float_eq!(
            result.sum(None).unwrap().item::<f32>(),
            4.655_846,
            abs <= 0.093_116_924
        );
    }

    #[test]
    #[allow(non_snake_case)]
    fn test_fast_sdpa() {
        // This test just makes sure that `scaled_dot_product_attention` is callable
        // in the various cases, based on the Python test `test_fast_sdpa`.

        let Dk = 64;
        let scale = 1.0 / (Dk as f32).sqrt();
        for seq_len in [63, 129, 400] {
            for dtype in [crate::Dtype::Float32, crate::Dtype::Float16] {
                let B = 2;
                let H = 24;
                let q = normal::<f32>(&[B, H, seq_len, Dk], None, None, None)
                    .unwrap()
                    .as_dtype(dtype)
                    .unwrap();
                let k = normal::<f32>(&[B, H, seq_len, Dk], None, None, None)
                    .unwrap()
                    .as_dtype(dtype)
                    .unwrap();
                let v = normal::<f32>(&[B, H, seq_len, Dk], None, None, None)
                    .unwrap()
                    .as_dtype(dtype)
                    .unwrap();

                let result = scaled_dot_product_attention(q, k, v, scale, None, None).unwrap();
                assert_eq!(result.shape(), [B, H, seq_len, Dk]);
                assert_eq!(result.dtype(), dtype);
            }
        }
    }

    // Test adapted from Python test `test_fast_sdpa.py/test_sdpa_attention_sinks`
    #[test]
    fn test_fast_sdpa_with_sinks() {
        let b = 2;
        let n_q = 8;
        let t_q = 128;
        let t_kv = 128;
        let d = 64;

        let q = normal::<f32>(&[b, n_q, t_q, d], None, None, None).unwrap();
        let k = normal::<f32>(&[b, n_q, t_kv, d], None, None, None).unwrap();
        let v = normal::<f32>(&[b, n_q, t_kv, d], None, None, None).unwrap();
        let scale = (d as f32).powf(-0.5);

        // Test with sinks parameter
        let sinks = normal::<f32>(&[n_q], None, None, None).unwrap() * 10.0;

        let result = scaled_dot_product_attention(&q, &k, &v, scale, None, &sinks).unwrap();
        assert_eq!(result.shape(), &[b, n_q, t_q, d]);
    }

    // Exercises the whole metal-kernel FFI path (vector_string names, config
    // builder, apply, Vec<Array> extraction) against a real GPU launch.
    // Ignored by default like the other MLX-running smokes in this workspace;
    // run with `cargo test -p mlx-rs metal_kernel_exp -- --ignored`.
    #[test]
    #[ignore = "runs a real Metal kernel on the GPU"]
    fn test_metal_kernel_exp() {
        crate::random::seed(42).unwrap();
        let input = normal::<f32>(&[4, 16], None, None, None).unwrap();

        // Same trivial elementwise-exp kernel as the mlx-c reference example.
        let kernel = MetalKernel::new(
            "myexp",
            &["inp"],
            &["out"],
            "uint elem = thread_position_in_grid.x;\n\
             T tmp = inp[elem];\n\
             out[elem] = metal::exp(tmp);",
            "",
            true,
            false,
        )
        .unwrap();

        let shape: &[i32] = &[4, 16];
        let config = MetalKernelConfig {
            output_shapes: &[shape],
            output_dtypes: &[crate::Dtype::Float32],
            grid: (64, 1, 1),
            thread_group: (256, 1, 1),
            template_args: &[("T", MetalKernelTemplateArg::Dtype(crate::Dtype::Float32))],
            init_value: None,
            verbose: false,
        };

        let outputs = kernel.apply(&[&input], &config).unwrap();
        assert_eq!(outputs.len(), 1);
        assert_eq!(outputs[0].shape(), [4, 16]);

        let expected = input.exp().unwrap();
        let diff = &outputs[0] - &expected;
        let max_diff = diff.abs().unwrap().max(None).unwrap().item::<f32>();
        assert!(max_diff < 1e-5, "max diff was {}", max_diff);
    }
}
