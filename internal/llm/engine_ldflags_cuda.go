//go:build llama && cuda

package llm

/*
// CUDA build (GGML_CUDA=ON). Links all ggml static libs plus ggml-cuda
// and the CUDA toolkit runtime libraries.
//
// Linux statically links cudart/cublas/cublasLt (available in the toolkit).
// Static cuBLAS pulls in culibos (culibosLoadLibrary / culibosGetProcAddress).
//
// Windows CUDA CI uses MSVC + Ninja (MinGW cannot link the CUDA toolkit).
// Runtime needs cudart/cublas/cublasLt DLLs shipped next to the binary.
//
// The CUDA toolkit lib dir is injected via CGO_LDFLAGS from the CI job
// (Linux: lib64 + lib64/stubs; Windows: lib/x64).

#cgo linux LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cuda/libggml-cuda.a -lm -lstdc++ -lpthread -ldl -lgomp -lcudart_static -lcublas_static -lcublasLt_static -lculibos -lcuda -lrt -ldl -lpthread
#cgo windows LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/llama.lib ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml.lib ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-base.lib ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cpu.lib ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cuda/ggml-cuda.lib cudart.lib cublas.lib cublasLt.lib cuda.lib advapi32.lib
#cgo darwin LDFLAGS: ${SRCDIR}/../../third_party/llama.cpp/build/src/libllama.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-base.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/libggml-cpu.a ${SRCDIR}/../../third_party/llama.cpp/build/ggml/src/ggml-cuda/libggml-cuda.a -lm -lc++ -framework Accelerate
*/
import "C"
